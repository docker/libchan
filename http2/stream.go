package http2

import (
	"encoding/base64"
	"github.com/docker/libchan"
	"github.com/docker/spdystream"
	"io"
	"net"
	"net/http"
	"os"
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

func (s *StreamSession) NewSender() (libchan.Sender, error) {
	stream, streamErr := s.conn.CreateStream(http.Header{}, nil, false)
	if streamErr != nil {
		return nil, streamErr
	}

	// Wait for stream reply
	waitErr := stream.Wait()
	if waitErr != nil {
		return nil, waitErr
	}
	return &StreamSender{stream: stream, streamChans: s}, nil
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
	if mode&libchan.Ret == 0 {
		header, receiveErr := s.stream.ReceiveHeader()
		if receiveErr != nil {
			return nil, receiveErr
		}
		data, dataErr := extractDataHeader(header)
		if dataErr != nil {
			return nil, dataErr
		}

		return &libchan.Message{
			Data: data,
			Fd:   nil,
			Ret:  s.ret,
		}, nil
	} else {
		streamChan := s.streamChans.getStreamChan(s.stream)
		stream := <-streamChan

		data, dataErr := extractDataHeader(stream.Headers())
		if dataErr != nil {
			return nil, dataErr
		}

		var attach *os.File
		var ret libchan.Sender

		var attachErr error
		attach, attachErr = createAttachment(stream)
		if attachErr != nil {
			return nil, attachErr
		}
		ret = &StreamSender{stream: stream, streamChans: s.streamChans}

		return &libchan.Message{
			Data: data,
			Fd:   attach,
			Ret:  ret,
		}, nil
	}
}

type StreamSender struct {
	stream      *spdystream.Stream
	streamChans streamChanProvider
}

func (s *StreamSender) Send(msg *libchan.Message) (ret libchan.Receiver, err error) {
	headers := http.Header{
		"Data": []string{base64.URLEncoding.EncodeToString(msg.Data)},
	}

	if msg.Fd != nil {
		stream, streamErr := s.stream.CreateSubStream(headers, false)
		if streamErr != nil {
			return nil, streamErr
		}

		// Wait for stream reply
		waitErr := stream.Wait()
		if waitErr != nil {
			return nil, waitErr
		}

		fConn, connErr := net.FileConn(msg.Fd)
		if connErr != nil {
			return nil, connErr
		}
		closeErr := msg.Fd.Close()
		if closeErr != nil {
			return nil, closeErr
		}
		go func() {
			io.Copy(fConn, stream)
		}()
		go func() {
			io.Copy(stream, fConn)
		}()
		// TODO keep track of fConn, close all when sender closed

		streamChan := make(chan *spdystream.Stream)
		s.streamChans.addStreamChan(stream, streamChan)
		ret = &StreamReceiver{stream: stream, streamChans: s.streamChans, ret: &StreamSender{stream: stream, streamChans: s.streamChans}}
	} else if libchan.RetPipe.Equals(msg.Ret) {
		stream, streamErr := s.stream.CreateSubStream(headers, false)
		if streamErr != nil {
			return nil, streamErr
		}

		// Wait for stream reply
		waitErr := stream.Wait()
		if waitErr != nil {
			return nil, waitErr
		}

		streamChan := make(chan *spdystream.Stream)
		s.streamChans.addStreamChan(stream, streamChan)
		ret = &StreamReceiver{stream: stream, streamChans: s.streamChans, ret: &StreamSender{stream: stream, streamChans: s.streamChans}}
	} else {
		sendErr := s.stream.SendHeader(headers, false)
		if sendErr != nil {
			return nil, sendErr
		}
		//ret = &libchan.NopReceiver{}
		ret = &StreamReceiver{stream: s.stream, streamChans: s.streamChans, ret: &StreamSender{stream: s.stream, streamChans: s.streamChans}}
	}

	return
}

func (s *StreamSender) Close() error {
	// TODO Remove stream from stream chans
	return s.stream.Close()
}
