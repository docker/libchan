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

type direction uint8

const (
	outbound = direction(0x01)
	inbound  = direction(0x02)
)

// session is a transport session on top of a network
// connection using spdy.
type session struct {
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

type channel struct {
	stream    *spdystream.Stream
	session   *session
	direction direction
}

// NewClientSession creates a new stream session from the
// provided network connection. The network connection is
// expected to already provide a tls session.
func NewClientSession(conn net.Conn) (libchan.Transport, error) {
	return newSession(conn, false)
}

// NewServerSession creates a new stream session from the
// provided network connection. The network connection is
// expected to already have completed the tls handshake.
func NewServerSession(conn net.Conn) (libchan.Transport, error) {
	return newSession(conn, true)
}

func newSession(conn net.Conn, server bool) (*session, error) {
	var referenceCounter uint64
	if server {
		referenceCounter = 2
	} else {
		referenceCounter = 1
	}
	session := &session{
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

func (s *session) newStreamHandler(stream *spdystream.Stream) {
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

func (s *session) getByteStream(referenceId uint64) *byteStream {
	s.byteStreamC.L.Lock()
	bs, ok := s.byteStreams[referenceId]
	if !ok {
		s.byteStreamC.Wait()
		bs, ok = s.byteStreams[referenceId]
	}
	s.byteStreamC.L.Unlock()
	return bs
}

func (s *session) dial(referenceId uint64) (*byteStream, error) {
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

func (s *session) createByteStream() (io.ReadWriteCloser, error) {
	s.referenceLock.Lock()
	referenceId := s.referenceCounter
	s.referenceCounter = referenceId + 2
	s.referenceLock.Unlock()

	byteStream, bsErr := s.dial(referenceId)
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

func (s *session) RegisterConn(conn net.Conn) error {
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

func (s *session) RegisterListener(listener net.Listener) {
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

func (s *session) Unregister(net.Conn) {

}

func (s *session) Close() error {
	return s.conn.Close()
}

// NewSendChannel creates and returns a new send channel.  The receive
// end will get picked up on the remote end through the remote calling
// WaitReceiveChannel.
func (s *session) NewSendChannel() (libchan.ChannelSender, error) {
	stream, streamErr := s.conn.CreateStream(http.Header{}, nil, false)
	if streamErr != nil {
		return nil, streamErr
	}
	return &channel{stream: stream, session: s, direction: outbound}, nil
}

// WaitReceiveChannel waits for a new channel be created by a remote
// call to NewSendChannel.
func (s *session) WaitReceiveChannel() (libchan.ChannelReceiver, error) {
	stream, ok := <-s.streamChan
	if !ok {
		return nil, io.EOF
	}

	return &channel{
		stream:    stream,
		session:   s,
		direction: inbound,
	}, nil
}

func (c *channel) createSubChannel(direction direction) (libchan.ChannelSender, libchan.ChannelReceiver, error) {
	if c.direction == inbound {
		return nil, nil, errors.New("Cannot create sub channel of an input channel")
	}
	stream, streamErr := c.stream.CreateSubStream(http.Header{}, false)
	if streamErr != nil {
		return nil, nil, streamErr
	}
	channel := &channel{
		stream:    stream,
		session:   c.session,
		direction: direction,
	}
	return channel, channel, nil
}

// CreateByteStream creates a new byte stream using an underlying
// spdy stream.
func (c *channel) CreateByteStream() (io.ReadWriteCloser, error) {
	return c.session.createByteStream()
}

// CreateNestedReceiver creates a new channel returning the local
// receiver and the remote sender.  The remote sender needs to be
// sent across the channel before being utilized.
func (c *channel) CreateNestedReceiver() (libchan.ChannelReceiver, libchan.ChannelSender, error) {
	send, recv, err := c.createSubChannel(inbound)
	return recv, send, err
}

// CreateNestedReceiver creates a new channel returning the local
// sender and the remote receiver.  The remote receiver needs to be
// sent across the channel before being utilized.
func (c *channel) CreateNestedSender() (libchan.ChannelSender, libchan.ChannelReceiver, error) {
	return c.createSubChannel(outbound)
}

// Send sends a message across the channel to a receiver on the
// other side of the transport.
func (c *channel) Send(message interface{}) error {
	if c.direction == inbound {
		return libchan.ErrWrongDirection
	}
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
	return nil
}

// Receive receives a message sent across the channel from
// a sender on the other side of the transport.
func (c *channel) Receive(message interface{}) error {
	if c.direction == outbound {
		return libchan.ErrWrongDirection
	}
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
	return nil
}

// Close closes the underlying stream, causing any subsequent
// sends to throw an error and receives to return EOF.
func (c *channel) Close() error {
	return c.stream.Close()
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
