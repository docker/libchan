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

	referenceLock    sync.Mutex
	referenceCounter libchan.ReferenceId
	byteStreamC      *sync.Cond
	byteStreams      map[libchan.ReferenceId]*libchan.ByteStream
}

type Channel struct {
	stream    *spdystream.Stream
	session   *Session
	direction libchan.Direction
}

func NewClientSession(conn net.Conn) (*Session, error) {
	return newSession(conn, false)
}

func newSession(conn net.Conn, server bool) (*Session, error) {
	var referenceCounter libchan.ReferenceId
	if server {
		referenceCounter = 2
	} else {
		referenceCounter = 1
	}
	session := &Session{
		streamChan:       make(chan *spdystream.Stream),
		referenceCounter: referenceCounter,
		byteStreamC:      sync.NewCond(new(sync.Mutex)),
		byteStreams:      make(map[libchan.ReferenceId]*libchan.ByteStream),
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
	referenceIdString := stream.Headers().Get("libchan-ref")

	returnHeaders := http.Header{}
	finish := false
	if referenceIdString != "" {
		referenceId, parseErr := strconv.ParseUint(referenceIdString, 10, 64)
		if parseErr != nil {
			returnHeaders.Set("status", "400")
			finish = true
		} else {
			byteStream := &libchan.ByteStream{
				ReferenceId: libchan.ReferenceId(referenceId),
				Stream:      stream,
			}
			s.byteStreamC.L.Lock()
			s.byteStreams[libchan.ReferenceId(referenceId)] = byteStream
			s.byteStreamC.Broadcast()
			s.byteStreamC.L.Unlock()
		}
	} else {
		if stream.Parent() == nil {
			s.streamChan <- stream
		}
	}

	stream.SendReply(returnHeaders, finish)
}

func (s *Session) GetByteStream(referenceId libchan.ReferenceId) *libchan.ByteStream {
	s.byteStreamC.L.Lock()
	bs, ok := s.byteStreams[referenceId]
	if !ok {
		s.byteStreamC.Wait()
		bs, ok = s.byteStreams[referenceId]
	}
	s.byteStreamC.L.Unlock()
	return bs
}

func (s *Session) Dial(referenceId libchan.ReferenceId) (*libchan.ByteStream, error) {
	headers := http.Header{}
	headers.Set("libchan-ref", strconv.FormatUint(uint64(referenceId), 10))
	stream, streamErr := s.conn.CreateStream(headers, nil, false)
	if streamErr != nil {
		return nil, streamErr
	}
	bs := &libchan.ByteStream{
		ReferenceId: referenceId,
		Stream:      stream,
	}
	return bs, nil
}

func (s *Session) CreateByteStream(provider libchan.ByteStreamDialer) (*libchan.ByteStream, error) {
	s.referenceLock.Lock()
	referenceId := s.referenceCounter
	s.referenceCounter = referenceId + 2
	s.referenceLock.Unlock()

	if provider == nil {
		provider = s
	}

	byteStream, bsErr := provider.Dial(referenceId)
	if bsErr != nil {
		return nil, bsErr
	}
	byteStream.ReferenceId = referenceId

	s.byteStreamC.L.Lock()
	s.byteStreams[referenceId] = byteStream
	s.byteStreamC.L.Unlock()

	return byteStream, nil
}

func (s *Session) RegisterListener(listener libchan.ByteStreamListener) {
	go func() {
		for {
			byteStream, err := listener.Accept()
			if err != nil {
				// Log
				continue
			}
			referenceId := byteStream.ReferenceId

			s.byteStreamC.L.Lock()
			s.byteStreams[referenceId] = byteStream
			s.byteStreamC.Broadcast()
			s.byteStreamC.L.Unlock()
		}
	}()
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

func (c *Channel) CreateSubChannel(direction libchan.Direction) (libchan.Channel, error) {
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

func (c *Channel) CreateByteStream(provider libchan.ByteStreamDialer) (*libchan.ByteStream, error) {
	return c.session.CreateByteStream(provider)
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
