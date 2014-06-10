package http2

import (
	"encoding/base64"
	"fmt"
	"github.com/docker/libchan"
	"github.com/docker/spdystream"
	"net"
	"net/http"
	"sync"
)

// Only allows sending, no parent stream
type StreamSession struct {
	conn *spdystream.Connection

	streamLock     sync.Mutex
	streamChan     chan *spdystream.Stream
	subStreamChans map[string]chan *spdystream.Stream
}

func (s *StreamSession) addStreamChan(stream *spdystream.Stream, streamChan chan *spdystream.Stream) {
	s.subStreamChans[stream.String()] = streamChan
}

func (s *StreamSession) getStreamChan(stream *spdystream.Stream) chan *spdystream.Stream {
	if stream == nil {
		return s.streamChan
	}
	streamChan, ok := s.subStreamChans[stream.String()]
	if ok {
		return streamChan
	}
	return s.streamChan
}

func (s *StreamSession) newStreamHandler(stream *spdystream.Stream) {
	streamChan := s.getStreamChan(stream.Parent())
	streamChan <- stream
}

func NewStreamSession(conn net.Conn) (*StreamSession, error) {
	session := &StreamSession{
		streamChan:     make(chan *spdystream.Stream),
		subStreamChans: make(map[string]chan *spdystream.Stream),
	}

	spdyConn, spdyErr := spdystream.NewConnection(conn, false)
	if spdyErr != nil {
		return nil, spdyErr
	}
	go spdyConn.Serve(session.newStreamHandler, spdystream.NoAuthHandler)

	session.conn = spdyConn

	return session, nil
}

func (s *StreamSession) Send(msg *libchan.Message) (ret libchan.Receiver, err error) {
	if msg.Fd != nil {
		return nil, fmt.Errorf("file attachment not yet implemented for spdy transport")
	}

	var fin bool
	if libchan.RetPipe.Equals(msg.Ret) {
		fin = false
	} else {
		fin = true
	}
	headers := http.Header{
		"Data": []string{base64.URLEncoding.EncodeToString(msg.Data)},
	}
	stream, streamErr := s.conn.CreateStream(headers, nil, fin)
	if streamErr != nil {
		return nil, streamErr
	}

	streamChan := make(chan *spdystream.Stream)
	s.subStreamChans[stream.String()] = streamChan

	if libchan.RetPipe.Equals(msg.Ret) {
		ret = &StreamReceiver{stream: stream, streamChans: s}
	} else {
		ret = &libchan.NopReceiver{}
	}
	return
}

func (s *StreamSession) Close() error {
	return s.conn.Close()
}

type StreamReceiver struct {
	stream      *spdystream.Stream
	streamChans streamChanProvider
	ret         libchan.Sender
}

func (s *StreamReceiver) Receive(mode int) (*libchan.Message, error) {
	waitErr := s.stream.Wait()
	if waitErr != nil {
		return nil, waitErr
	}
	streamChan := s.streamChans.getStreamChan(s.stream)
	stream := <-streamChan
	return createStreamMessage(stream, mode, s.streamChans, s.ret)
}

type StreamSender struct {
	stream      *spdystream.Stream
	streamChans streamChanProvider
}

func (s *StreamSender) Send(msg *libchan.Message) (ret libchan.Receiver, err error) {
	if msg.Fd != nil {
		return nil, fmt.Errorf("file attachment not yet implemented for spdy transport")
	}

	var fin bool
	if libchan.RetPipe.Equals(msg.Ret) {
		fin = false
	} else {
		fin = true
	}
	headers := http.Header{
		"Data": []string{base64.URLEncoding.EncodeToString(msg.Data)},
	}

	stream, streamErr := s.stream.CreateSubStream(headers, fin)
	if streamErr != nil {
		return nil, streamErr
	}

	streamChan := make(chan *spdystream.Stream)
	s.streamChans.addStreamChan(stream, streamChan)

	if libchan.RetPipe.Equals(msg.Ret) {
		ret = &StreamReceiver{stream: stream, streamChans: s.streamChans}
	} else {
		ret = libchan.NopReceiver{}
	}

	return
}

func (s *StreamSender) Close() error {
	// TODO Remove stream from stream chans
	return s.stream.Close()
}
