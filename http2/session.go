package http2

import (
	"errors"
	"io"
	"net"
	"net/http"
	"strconv"
	"sync"

	"github.com/docker/libchan"
	"github.com/docker/spdystream"
	"github.com/ugorji/go/codec"
)

type Session struct {
	conn *spdystream.Connection

	streamChan chan *spdystream.Stream

	referenceCounter int
	byteStreamC      *sync.Cond
	byteStreams      map[string]*ByteStream
}

type Channel struct {
	stream    *spdystream.Stream
	session   *Session
	direction libchan.Direction
}

type ByteStream struct {
	referenceId string

	stream io.ReadWriteCloser
}

func NewClientSession(conn net.Conn) (*Session, error) {
	return newSession(conn, false)
}

func newSession(conn net.Conn, server bool) (*Session, error) {
	var referenceCounter int
	if server {
		referenceCounter = 2
	} else {
		referenceCounter = 1
	}
	session := &Session{
		streamChan:       make(chan *spdystream.Stream),
		referenceCounter: referenceCounter,
		byteStreamC:      sync.NewCond(new(sync.Mutex)),
		byteStreams:      make(map[string]*ByteStream),
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
	referenceId := stream.Headers().Get("libchan-ref")
	if referenceId != "" {
		byteStream := &ByteStream{
			referenceId: referenceId,
			stream:      stream,
		}
		s.byteStreamC.L.Lock()
		s.byteStreams[referenceId] = byteStream
		s.byteStreamC.Broadcast()
		s.byteStreamC.L.Unlock()
	} else {
		if stream.Parent() == nil {
			s.streamChan <- stream
		}
	}

	stream.SendReply(http.Header{}, false)
}

func (s *Session) GetByteStream(referenceId string) *ByteStream {
	s.byteStreamC.L.Lock()
	bs, ok := s.byteStreams[referenceId]
	if !ok {
		s.byteStreamC.Wait()
		bs, ok = s.byteStreams[referenceId]
	}
	s.byteStreamC.L.Unlock()
	return bs
}

func (s *Session) createByteStream() (io.ReadWriteCloser, error) {
	referenceId := strconv.Itoa(s.referenceCounter) // Add machine id?
	s.referenceCounter = s.referenceCounter + 2
	headers := http.Header{}
	headers.Set("libchan-ref", referenceId)
	stream, streamErr := s.conn.CreateStream(headers, nil, false)
	if streamErr != nil {
		return nil, streamErr
	}
	bs := &ByteStream{
		referenceId: referenceId,
		stream:      stream,
	}
	s.byteStreamC.L.Lock()
	s.byteStreams[referenceId] = bs
	s.byteStreamC.L.Unlock()
	return bs, nil
}

func (s *Session) Close() error {
	return s.conn.Close()
}

func (s *Session) NewSendChannel() (*Channel, error) {
	stream, streamErr := s.conn.CreateStream(http.Header{}, nil, false)
	if streamErr != nil {
		return nil, streamErr
	}
	return &Channel{stream: stream, session: s, direction: libchan.Out}, nil
}

func (s *Session) WaitReceiveChannel() (*Channel, error) {
	stream, ok := <-s.streamChan
	if !ok {
		return nil, io.EOF
	}

	return &Channel{
		stream:    stream,
		session:   s,
		direction: libchan.In,
	}, nil
}

func (c *Channel) NewSubChannel(direction libchan.Direction) (*Channel, error) {
	if c.direction == libchan.In {
		return nil, errors.New("Cannot create sub channel of an input channel")
	}
	stream, streamErr := c.stream.CreateSubStream(http.Header{}, false)
	if streamErr != nil {
		return nil, streamErr
	}
	return &Channel{
		stream:    stream,
		session:   c.session,
		direction: direction,
	}, nil
}

func (c *Channel) CreateByteStream() (io.ReadWriteCloser, error) {
	return c.session.createByteStream()
}

func (c *Channel) Communicate(message interface{}) error {
	if c.direction == libchan.Out {
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
	} else if c.direction == libchan.In {
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
	} else {
		return errors.New("Invalid direction")
	}
	return nil

}

func (c *Channel) Close() error {
	return c.stream.Close()
}

func (c *Channel) Direction() libchan.Direction {
	return c.direction
}

func (b *ByteStream) Read(p []byte) (n int, err error) {
	if b == nil || b.stream == nil {
		return 0, errors.New("Byte stream is nil")
	}
	return b.stream.Read(p)
}

func (b *ByteStream) Write(data []byte) (n int, err error) {
	if b == nil || b.stream == nil {
		return 0, errors.New("Byte stream is nil")
	}
	return b.stream.Write(data)
}

func (b *ByteStream) Close() error {
	if b == nil || b.stream == nil {
		return errors.New("Byte stream is nil")
	}
	return b.stream.Close()
}
