package spdy

import (
	"errors"
	"io"
	"net"
	"net/http"
	"strconv"
	"sync"

	"github.com/dmcgowan/go/codec"
	"github.com/docker/libchan"
	"github.com/docker/spdystream"
)

type direction uint8

const (
	outbound = direction(0x01)
	inbound  = direction(0x02)
)

var (
	// ErrWrongDirection occurs when an illegal action is
	// attempted on a channel because of its direction.
	ErrWrongDirection = errors.New("wrong channel direction")
)

// Transport is a transport session on top of a network
// connection using spdy.
type Transport struct {
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
	session   *Transport
	direction direction
}

// NewClientTransport creates a new stream transport from the
// provided network connection. The network connection is
// expected to already provide a tls session.
func NewClientTransport(conn net.Conn) (*Transport, error) {
	return newSession(conn, false)
}

// NewServerTransport creates a new stream transport from the
// provided network connection. The network connection is
// expected to already have completed the tls handshake.
func NewServerTransport(conn net.Conn) (*Transport, error) {
	return newSession(conn, true)
}

func newSession(conn net.Conn, server bool) (*Transport, error) {
	var referenceCounter uint64
	if server {
		referenceCounter = 2
	} else {
		referenceCounter = 1
	}
	session := &Transport{
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

func (s *Transport) newStreamHandler(stream *spdystream.Stream) {
	referenceIDString := stream.Headers().Get("libchan-ref")

	returnHeaders := http.Header{}
	finish := false
	if referenceIDString != "" {
		referenceID, parseErr := strconv.ParseUint(referenceIDString, 10, 64)
		if parseErr != nil {
			returnHeaders.Set("status", "400")
			finish = true
		} else {
			byteStream := &byteStream{
				ReferenceID: referenceID,
				Stream:      stream,
			}
			s.byteStreamC.L.Lock()
			s.byteStreams[referenceID] = byteStream
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

func (s *Transport) getByteStream(referenceID uint64) *byteStream {
	s.byteStreamC.L.Lock()
	bs, ok := s.byteStreams[referenceID]
	if !ok {
		s.byteStreamC.Wait()
		bs, ok = s.byteStreams[referenceID]
	}
	s.byteStreamC.L.Unlock()
	return bs
}

func (s *Transport) dial(referenceID uint64) (*byteStream, error) {
	headers := http.Header{}
	headers.Set("libchan-ref", strconv.FormatUint(referenceID, 10))
	stream, streamErr := s.conn.CreateStream(headers, nil, false)
	if streamErr != nil {
		return nil, streamErr
	}
	bs := &byteStream{
		ReferenceID: referenceID,
		Stream:      stream,
	}
	return bs, nil
}

func (s *Transport) createByteStream() (io.ReadWriteCloser, error) {
	s.referenceLock.Lock()
	referenceID := s.referenceCounter
	s.referenceCounter = referenceID + 2
	s.referenceLock.Unlock()

	byteStream, bsErr := s.dial(referenceID)
	if bsErr != nil {
		return nil, bsErr
	}
	byteStream.ReferenceID = referenceID

	s.byteStreamC.L.Lock()
	s.byteStreams[referenceID] = byteStream
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

// RegisterConn registers a network connection to be used
// by inbound messages referring to the connection
// with the registered connection's local and remote address.
// Note: a connection does not need to be registered before
// being sent in a message, but does need to be registered
// to by the receiver of a message. If registration should be
// automatic, register a listener instead.
func (s *Transport) RegisterConn(conn net.Conn) error {
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

// RegisterListener accepts all connections from the listener
// and immediately registers them.
func (s *Transport) RegisterListener(listener net.Listener) {
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

// Unregister removes the connection from the list of known
// connections. This should be called when a connection is
// closed and no longer expected in inbound messages.
// Failure to unregister connections will increase memory
// usage since the transport is not notified of closed
// connections to automatically unregister.
func (s *Transport) Unregister(net.Conn) {

}

// Close closes the underlying spdy connection
func (s *Transport) Close() error {
	return s.conn.Close()
}

// NewSendChannel creates and returns a new send channel.  The receive
// end will get picked up on the remote end through the remote calling
// WaitReceiveChannel.
func (s *Transport) NewSendChannel() (libchan.Sender, error) {
	stream, streamErr := s.conn.CreateStream(http.Header{}, nil, false)
	if streamErr != nil {
		return nil, streamErr
	}
	return &channel{stream: stream, session: s, direction: outbound}, nil
}

// WaitReceiveChannel waits for a new channel be created by a remote
// call to NewSendChannel.
func (s *Transport) WaitReceiveChannel() (libchan.Receiver, error) {
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

func (c *channel) createSubChannel(direction direction) (libchan.Sender, libchan.Receiver, error) {
	if c.direction == inbound {
		return nil, nil, errors.New("cannot create sub channel of an inbound channel")
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
func (c *channel) CreateNestedReceiver() (libchan.Receiver, libchan.Sender, error) {
	send, recv, err := c.createSubChannel(inbound)
	return recv, send, err
}

// CreateNestedReceiver creates a new channel returning the local
// sender and the remote receiver.  The remote receiver needs to be
// sent across the channel before being utilized.
func (c *channel) CreateNestedSender() (libchan.Sender, libchan.Receiver, error) {
	return c.createSubChannel(outbound)
}

// Send sends a message across the channel to a receiver on the
// other side of the transport.
func (c *channel) Send(message interface{}) error {
	if c.direction == inbound {
		return ErrWrongDirection
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
		return ErrWrongDirection
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
	ReferenceID uint64
}

func (b *byteStream) Read(p []byte) (n int, err error) {
	if b == nil || b.Stream == nil {
		return 0, errors.New("byte stream is nil")
	}
	return b.Stream.Read(p)
}

func (b *byteStream) Write(data []byte) (n int, err error) {
	if b == nil || b.Stream == nil {
		return 0, errors.New("byte stream is nil")
	}
	return b.Stream.Write(data)
}

func (b *byteStream) Close() error {
	if b == nil || b.Stream == nil {
		return errors.New("byte stream is nil")
	}
	return b.Stream.Close()
}
