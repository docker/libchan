package http2

import (
	"io"
	"net"
	"net/http"

	"github.com/docker/spdystream"
	"github.com/ugorji/go/codec"
)

type Direction uint8

const (
	Out = Direction(0x01)
	In  = Direction(0x02)
)

type Session struct {
	conn *spdystream.Connection

	streamChan chan *spdystream.Stream
}

// ReceiveChannel receives messages accross a session.  If this
// channel was intended to be used on remote end, any calls to
// receive will fail.  A remote ReceiveChannel should be sent
// on another channel to the remote endpoint to use.  When the
// channel is closed an EOF is returned.
type ReceiveChannel struct {
	stream  *spdystream.Stream
	session *Session

	remote bool
}

type SendChannel struct {
	stream  *spdystream.Stream
	session *Session

	remote bool
}

func newSession(conn net.Conn, server bool) (*Session, error) {
	session := &Session{
		streamChan: make(chan *spdystream.Stream),
	}

	spdyConn, spdyErr := spdystream.NewConnection(conn, server)
	if spdyErr != nil {
		return nil, spdyErr
	}
	go spdyConn.Serve(session.newStreamHandler)

	session.conn = spdyConn

	return session, nil
}

func (s *Session) newStreamHandler(stream *spdystream.Stream) {
	stream.SendReply(http.Header{}, false)
	if stream.Parent() == nil {
		s.streamChan <- stream
	}
}

func (s *Session) Close() error {
	return s.conn.Close()
}

func (s *Session) NewSendChannel() (*SendChannel, error) {
	stream, streamErr := s.conn.CreateStream(http.Header{}, nil, false)
	if streamErr != nil {
		return nil, streamErr
	}
	return &SendChannel{stream: stream, session: s}, nil
}

func (s *Session) WaitReceiveChannel() (*ReceiveChannel, error) {
	stream, ok := <-s.streamChan
	if !ok {
		return nil, io.EOF
	}

	return &ReceiveChannel{
		stream:  stream,
		session: s,
		remote:  false,
	}, nil
}

func (c *SendChannel) NewSubChannel(direction Direction) (*SendChannel, *ReceiveChannel, error) {
	stream, streamErr := c.stream.CreateSubStream(http.Header{}, false)
	if streamErr != nil {
		return nil, nil, streamErr
	}
	return &SendChannel{
			stream:  stream,
			session: c.session,
			remote:  direction == Out,
		}, &ReceiveChannel{
			stream:  stream,
			session: c.session,
			remote:  direction == In,
		}, nil
}

func (c *SendChannel) Send(message interface{}) error {
	handler := getMsgPackHandler(c.session)
	var buf []byte
	encoder := codec.NewEncoderBytes(&buf, handler)
	encodeErr := encoder.Encode(message)
	if encodeErr != nil {
		return encodeErr
	}
	// TODO check length of buf
	_, writeErr := c.stream.Write(buf)
	if writeErr != nil {
		return writeErr
	}
	return nil
}

func (c *SendChannel) Close() error {
	return c.stream.Close()
}

func (c *ReceiveChannel) Receive(message interface{}) error {
	buf, readErr := c.stream.ReadData()
	if readErr != nil {
		if readErr == io.EOF {
			c.stream.Close()
		}
		return readErr
	}
	handler := getMsgPackHandler(c.session)
	decoder := codec.NewDecoderBytes(buf, handler)
	decodeErr := decoder.Decode(message)
	if decodeErr != nil {
		return decodeErr
	}
	return nil
}
