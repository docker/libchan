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

// StreamSession is a session manager on top of a network
// connection using spdy.
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
	stream.SendReply(http.Header{}, false)
	streamChan := s.getStreamChan(stream.Parent())
	streamChan <- stream
}

// NewStreamSession creates a new stream session from the
// provided network connection.  The network connection is
// expected to already provide a tls session.
func NewStreamSession(conn net.Conn) (*StreamSession, error) {
	session := &StreamSession{
		streamChan:     make(chan *spdystream.Stream),
		subStreamChans: make(map[string]chan *spdystream.Stream),
	}

	spdyConn, spdyErr := spdystream.NewConnection(conn, false)
	if spdyErr != nil {
		return nil, spdyErr
	}
	go spdyConn.Serve(session.newStreamHandler)

	session.conn = spdyConn

	return session, nil
}

// NewSender returns a new Sender from the session.  Each
// call to NewSender will result in a sender with a new
// underlying spdy stream.
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

// Close closes the underlying network connection, causing
// each stream created by this session to closed.
func (s *StreamSession) Close() error {
	return s.conn.Close()
}

// StreamReceiver is a receiver object with an underlying spdystream.
type StreamReceiver struct {
	stream      *spdystream.Stream
	streamChans streamChanProvider
	ret         libchan.Sender
}

// Receive returns a new message from the underlying stream. When
// the mode is set to return a pipe, the receiver will wait for a
// substream to be created.  If the mode is 0, then the receiver
// will wait for a message on the underlying stream.
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

// StreamSender is a sender object with an underlying spdy stream.
type StreamSender struct {
	stream      *spdystream.Stream
	streamChans streamChanProvider
}

// Send sends a messages either on the underlying stream or
// creating substream.  A substream is created when a file
// is attached or return pipe is requested, otherwise the
// message will just be sent on the underlying spdy stream.
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
		ret = &StreamReceiver{stream: s.stream, streamChans: s.streamChans, ret: &StreamSender{stream: s.stream, streamChans: s.streamChans}}
	}

	return
}

// Close closes the underlying spdy stream
func (s *StreamSender) Close() error {
	// TODO Remove stream from stream chans
	return s.stream.Close()
}
