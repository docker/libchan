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
	conn    *spdystream.Connection
	handler codec.Handle

	streamChan chan *spdystream.Stream

	referenceLock    sync.Mutex
	referenceCounter uint64
	byteStreamC      *sync.Cond
	byteStreams      map[uint64]*byteStream

	netConnC *sync.Cond
	netConns map[byte]map[string]net.Conn
	networks map[string]byte
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
	var referenceCounter uint64
	if server {
		referenceCounter = 2
	} else {
		referenceCounter = 1
	}
	session := &Session{
		streamChan:       make(chan *spdystream.Stream),
		referenceCounter: referenceCounter,
		byteStreamC:      sync.NewCond(new(sync.Mutex)),
		byteStreams:      make(map[uint64]*byteStream),
		netConnC:         sync.NewCond(new(sync.Mutex)),
		netConns:         make(map[byte]map[string]net.Conn),
		networks:         make(map[string]byte),
	}

	spdyConn, spdyErr := spdystream.NewConnection(conn, server)
	if spdyErr != nil {
		return nil, spdyErr
	}
	go spdyConn.Serve(session.newStreamHandler)

	session.conn = spdyConn
	session.handler = session.initializeHandler()

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
			byteStream := &byteStream{
				ReferenceId: referenceId,
				Stream:      stream,
			}
			s.byteStreamC.L.Lock()
			s.byteStreams[referenceId] = byteStream
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

func (s *Session) GetByteStream(referenceId uint64) *byteStream {
	s.byteStreamC.L.Lock()
	bs, ok := s.byteStreams[referenceId]
	if !ok {
		s.byteStreamC.Wait()
		bs, ok = s.byteStreams[referenceId]
	}
	s.byteStreamC.L.Unlock()
	return bs
}

func (s *Session) Dial(referenceId uint64) (*byteStream, error) {
	headers := http.Header{}
	headers.Set("libchan-ref", strconv.FormatUint(referenceId, 10))
	stream, streamErr := s.conn.CreateStream(headers, nil, false)
	if streamErr != nil {
		return nil, streamErr
	}
	bs := &byteStream{
		ReferenceId: referenceId,
		Stream:      stream,
	}
	return bs, nil
}

func (s *Session) CreateByteStream() (io.ReadWriteCloser, error) {
	s.referenceLock.Lock()
	referenceId := s.referenceCounter
	s.referenceCounter = referenceId + 2
	s.referenceLock.Unlock()

	byteStream, bsErr := s.Dial(referenceId)
	if bsErr != nil {
		return nil, bsErr
	}
	byteStream.ReferenceId = referenceId

	s.byteStreamC.L.Lock()
	s.byteStreams[referenceId] = byteStream
	s.byteStreamC.L.Unlock()

	return byteStream, nil
}

func addrKey(local, remote string) string {
	b := make([]byte, len(local)+len(remote)+2)
	copy(b, local)
	copy(b[len(local):], "<>")
	copy(b[len(local)+2:], remote)
	return string(b)
}

func (s *Session) RegisterConn(conn net.Conn) error {
	s.netConnC.L.Lock()
	defer s.netConnC.L.Unlock()
	networkType, ok := s.networks[conn.LocalAddr().Network()]
	if !ok {
		return errors.New("unknown network")
	}
	networks := s.netConns[networkType]
	networks[addrKey(conn.LocalAddr().String(), conn.RemoteAddr().String())] = conn
	s.netConnC.Broadcast()

	return nil
}

func (s *Session) RegisterListener(listener net.Listener) {
	go func() {
		for {
			conn, err := listener.Accept()
			if err != nil {
				// Log
				continue
			}
			err = s.RegisterConn(conn)
			if err != nil {
				// Log use of unsupported network
				break
			}
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

func (c *Channel) CreateByteStream() (io.ReadWriteCloser, error) {
	return c.session.CreateByteStream()
}

func (c *Channel) Communicate(message interface{}) error {
	if c.direction == libchan.Out {
		var buf []byte
		encoder := codec.NewEncoderBytes(&buf, c.session.handler)
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
		decoder := codec.NewDecoderBytes(buf, c.session.handler)
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

type byteStream struct {
	Stream      io.ReadWriteCloser
	ReferenceId uint64
}

func (b *byteStream) Read(p []byte) (n int, err error) {
	if b == nil || b.Stream == nil {
		return 0, errors.New("Byte stream is nil")
	}
	return b.Stream.Read(p)
}

func (b *byteStream) Write(data []byte) (n int, err error) {
	if b == nil || b.Stream == nil {
		return 0, errors.New("Byte stream is nil")
	}
	return b.Stream.Write(data)
}

func (b *byteStream) Close() error {
	if b == nil || b.Stream == nil {
		return errors.New("Byte stream is nil")
	}
	return b.Stream.Close()
}
