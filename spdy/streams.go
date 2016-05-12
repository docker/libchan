package spdy

import (
	"io"
	"net"
	"net/http"

	"github.com/docker/spdystream"
)

// Stream is an interface to represent a single
// byte stream on a multi-plexed connection with
// plus headers and a method to force full closure.
type Stream interface {
	io.ReadWriteCloser
	Headers() http.Header
	Reset() error
}

// Listener is an interface for returning remotely
// created streams.
type Listener interface {
	Accept() (Stream, error)
}

// StreamProvider is the minimal interface for
// creating new streams and receiving remotely
// created streams.
type StreamProvider interface {
	NewStream(http.Header) (Stream, error)
	Close() error
	Listen() Listener
}

// spdystream implementation of stream provider interface

type spdyStream struct {
	stream *spdystream.Stream
}

type spdyStreamListener struct {
	listenChan <-chan *spdyStream
	closeChan  <-chan bool
}

type spdyStreamProvider struct {
	conn       *spdystream.Connection
	closeChan  chan struct{}
	listenChan chan *spdyStream
}

// NewSpdyStreamProvider creates a stream provider by starting a spdy
// session on the given connection. The server argument is used to
// determine whether the spdy connection is the client or server side.
func NewSpdyStreamProvider(conn net.Conn, server bool) (StreamProvider, error) {
	spdyConn, spdyErr := spdystream.NewConnection(conn, server)
	if spdyErr != nil {
		return nil, spdyErr
	}
	provider := &spdyStreamProvider{
		conn:       spdyConn,
		closeChan:  make(chan struct{}),
		listenChan: make(chan *spdyStream),
	}
	go spdyConn.Serve(provider.newStreamHandler)

	return provider, nil
}

func (p *spdyStreamProvider) newStreamHandler(stream *spdystream.Stream) {
	s := &spdyStream{
		stream: stream,
	}
	returnHeaders := http.Header{}
	var finish bool
	select {
	case <-p.closeChan:
		returnHeaders.Set(":status", "502")
		finish = true
	case p.listenChan <- s:
		returnHeaders.Set(":status", "200")
	}
	stream.SendReply(returnHeaders, finish)
}

func (p *spdyStreamProvider) NewStream(headers http.Header) (Stream, error) {
	stream, streamErr := p.conn.CreateStream(headers, nil, false)
	if streamErr != nil {
		return nil, streamErr
	}
	return &spdyStream{stream: stream}, nil
}

func (p *spdyStreamProvider) Close() error {
	close(p.closeChan)
	return p.conn.Close()
}

func (p *spdyStreamProvider) Listen() Listener {
	return &spdyStreamListener{
		listenChan: p.listenChan,
		closeChan:  p.conn.CloseChan(),
	}
}

func (l *spdyStreamListener) Accept() (Stream, error) {
	var stream *spdyStream
	select {
	case stream = <-l.listenChan:
	case <-l.closeChan:
		// Handle spdyConn shutdown
	}
	if stream == nil {
		return nil, io.EOF
	}
	return stream, nil
}

func (s *spdyStream) Read(b []byte) (int, error) {
	return s.stream.Read(b)
}

func (s *spdyStream) Write(b []byte) (int, error) {
	return s.stream.Write(b)
}

func (s *spdyStream) Close() error {
	return s.stream.Close()
}

func (s *spdyStream) Reset() error {
	return s.stream.Reset()
}

func (s *spdyStream) Headers() http.Header {
	return s.stream.Headers()
}
