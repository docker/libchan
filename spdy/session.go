package spdy

import (
	"errors"
	"io"
	"net"
	"net/http"
	"strconv"
	"sync"
	"time"
	"fmt"

	"github.com/dmcgowan/go/codec"
	"github.com/docker/libchan"
	"github.com/docker/spdystream"
)

type direction uint8

const (
	outbound = direction(0x01)
	inbound  = direction(0x02)
	// Defaults for heartbeat.
	// Currently ping every 30 seconds and fail if no pings respond
	//
	// The frequency of pinging the client to
	// detect a dead session. This is the default and
	// can be overridden via property on the *Transport
	defaultHeartbeatInterval = time.Second * 30
	// The amount of times the heartbeat fails
	// before the session is considered dead. This is
	// the default and can be overridden via property
	// on the *Transport.
	defaultHeartbeatLimit = 3
)

var (
	// ErrWrongDirection occurs when an illegal action is
	// attempted on a channel because of its direction.
	ErrWrongDirection = errors.New("wrong channel direction")
)

// Transport is a transport session on top of a network
// connection using spdy.
type Transport struct {
	HeartbeatInterval time.Duration
	HeartbeatLimit    int

	conn    *spdystream.Connection
	handler codec.Handle

	deadSessionChan chan struct{}
	deadSessionFlag bool

	receiverChan chan *channel
	channelC     *sync.Cond
	channels     map[uint64]*channel

	referenceLock    sync.Mutex
	referenceCounter uint64
	byteStreamC      *sync.Cond
	byteStreams      map[uint64]*byteStream

	netConnC *sync.Cond
	netConns map[byte]map[string]net.Conn
	networks map[string]byte
}

type channel struct {
	referenceID uint64
	parentID    uint64
	stream      *spdystream.Stream
	session     *Transport
	direction   direction
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
		deadSessionChan:   make(chan struct{}),
		receiverChan:      make(chan *channel),
		channelC:          sync.NewCond(new(sync.Mutex)),
		channels:          make(map[uint64]*channel),
		referenceCounter:  referenceCounter,
		byteStreamC:       sync.NewCond(new(sync.Mutex)),
		byteStreams:       make(map[uint64]*byteStream),
		netConnC:          sync.NewCond(new(sync.Mutex)),
		netConns:          make(map[byte]map[string]net.Conn),
		networks:          make(map[string]byte),
		HeartbeatInterval: defaultHeartbeatInterval,
		HeartbeatLimit:    defaultHeartbeatLimit,
	}

	spdyConn, spdyErr := spdystream.NewConnection(conn, server)
	if spdyErr != nil {
		return nil, spdyErr
	}
	go spdyConn.Serve(session.newStreamHandler)

	session.conn = spdyConn
	session.handler = session.initializeHandler()

	// Looping heartbeat monitor. Pings the client to
	// determine if it has lost connection without sending
	// a close.
	go session.monitorHeartbeat()

	return session, nil
}

// errDeadSession occurs when heartbeat is enabled and
// a ping returns an error trying to contact the client.
// This is useful for managing long runnning connections
// that may die form network failure. This is a method
// rather than a var to allow insertion of time elapsed
// dynamically.
func (s *Transport) errDeadSession() error {
	return errors.New(fmt.Sprintf("session appears dead no response after %v", s.HeartbeatInterval * time.Duration(s.HeartbeatLimit)))
}

func (s *Transport) monitorHeartbeat() {
	var hbFailures int = 0
	for {
		// Only loop after waiting for the heartbeatInterval
		time.Sleep(s.HeartbeatInterval)
		_, err := s.conn.Ping()
		if err != nil {
			// Increase heartbeat failure count
			hbFailures++
			// If we have hit out limit on failures we trigger marking
			// the session as dead.
			if hbFailures >= s.HeartbeatLimit {
				// Set the deadSessionFlag to true. This is used to
				// check for a dead session before starting a blocking
				// op using a channel.
				s.deadSessionFlag = true
				// Uses the closing of a channel trick to
				// broadcast to all waiting threads that
				// the session is dead.
				// Any thread that needs to wait on a blocking
				// op that is dependant on the session being live
				// should implement a select that includes this
				// channel closing as a signal.
				close(s.deadSessionChan)
				return
			}
		} else {
			// Reset heartbeat failure count
			hbFailures = 0
		}
	}
}

func (s *Transport) newStreamHandler(stream *spdystream.Stream) {
	referenceIDString := stream.Headers().Get("libchan-ref")
	parentIDString := stream.Headers().Get("libchan-parent-ref")

	returnHeaders := http.Header{}
	finish := false
	referenceID, parseErr := strconv.ParseUint(referenceIDString, 10, 64)
	if parseErr != nil {
		returnHeaders.Set("status", "400")
		finish = true
	} else {
		if parentIDString == "" {
			byteStream := &byteStream{
				referenceID: referenceID,
				stream:      stream,
				session:     s,
			}
			s.byteStreamC.L.Lock()
			s.byteStreams[referenceID] = byteStream
			s.byteStreamC.Broadcast()
			s.byteStreamC.L.Unlock()

			returnHeaders.Set("status", "200")
		} else {
			parentID, parseErr := strconv.ParseUint(parentIDString, 10, 64)
			if parseErr != nil {
				returnHeaders.Set("status", "400")
				finish = true
			} else {
				c := &channel{
					referenceID: referenceID,
					parentID:    parentID,
					stream:      stream,
					session:     s,
				}
				s.channelC.L.Lock()
				s.channels[referenceID] = c
				s.channelC.Broadcast()
				s.channelC.L.Unlock()

				if parentID == 0 {
					c.direction = inbound
					s.receiverChan <- c
				}

				returnHeaders.Set("status", "200")
			}
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

func (s *Transport) getChannel(referenceID uint64) *channel {
	s.channelC.L.Lock()
	c, ok := s.channels[referenceID]
	if !ok {
		s.channelC.Wait()
		c, ok = s.channels[referenceID]
	}
	s.channelC.L.Unlock()
	return c
}

func (s *Transport) dial(referenceID uint64) (*byteStream, error) {
	headers := http.Header{}
	headers.Set("libchan-ref", strconv.FormatUint(referenceID, 10))
	stream, streamErr := s.conn.CreateStream(headers, nil, false)
	if streamErr != nil {
		return nil, streamErr
	}
	bs := &byteStream{
		referenceID: referenceID,
		stream:      stream,
		session:     s,
	}
	return bs, nil
}

func (s *Transport) nextReferenceID() uint64 {
	s.referenceLock.Lock()
	referenceID := s.referenceCounter
	s.referenceCounter = referenceID+2
	s.referenceLock.Unlock()
	return referenceID
}

func (s *Transport) createByteStream() (io.ReadWriteCloser, error) {
	referenceID := s.nextReferenceID()

	byteStream, bsErr := s.dial(referenceID)
	if bsErr != nil {
		return nil, bsErr
	}
	byteStream.referenceID = referenceID

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
	referenceID := s.nextReferenceID()
	headers := http.Header{}
	headers.Set("libchan-ref", strconv.FormatUint(referenceID, 10))
	headers.Set("libchan-parent-ref", "0")

	stream, streamErr := s.conn.CreateStream(headers, nil, false)
	if streamErr != nil {
		return nil, streamErr
	}
	c := &channel{
		referenceID: referenceID,
		stream:      stream,
		session:     s,
		direction:   outbound,
	}

	s.channelC.L.Lock()
	s.channels[referenceID] = c
	s.channelC.L.Unlock()

	return c, nil
}

// WaitReceiveChannel waits for a new channel be created by a remote
// call to NewSendChannel.
func (s *Transport) WaitReceiveChannel() (libchan.Receiver, error) {
	for {
		// Safety check to see if session is dead before starting select
		if s.deadSessionFlag {
			return nil, s.errDeadSession()
		}
		// We use a select to wait for either the receiver channel
		// or a dead session channel.
		select {
		case <-s.deadSessionChan:
			// Return nil and ErrDeadSession
			return nil, s.errDeadSession()
		case r, ok := <-s.receiverChan:
			if !ok {
				return nil, io.EOF
			}
			return r, nil
		}

	}
}

func (c *channel) createSubChannel(direction direction) (libchan.Sender, libchan.Receiver, error) {
	if c.direction == inbound {
		return nil, nil, errors.New("cannot create sub channel of an inbound channel")
	}
	referenceID := c.session.nextReferenceID()
	headers := http.Header{}
	headers.Set("libchan-ref", strconv.FormatUint(referenceID, 10))
	headers.Set("libchan-parent-ref", strconv.FormatUint(c.referenceID, 10))

	stream, streamErr := c.stream.CreateSubStream(headers, false)
	if streamErr != nil {
		return nil, nil, streamErr
	}
	subChannel := &channel{
		stream:    stream,
		session:   c.session,
		direction: direction,
	}

	c.session.channelC.L.Lock()
	c.session.channels[referenceID] = subChannel
	c.session.channelC.L.Unlock()

	return subChannel, subChannel, nil
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
	mCopy, mErr := c.copyMessage(message)
	if mErr != nil {
		return mErr
	}

	var buf []byte
	encoder := codec.NewEncoderBytes(&buf, c.session.handler)
	encodeErr := encoder.Encode(mCopy)
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
	// Use a goroutine and channel to ReadData from channel
	buffChan := make(chan interface{})
	go c.handleReadData(buffChan)
	// Wait for channel response or signal that session is dead
	select {
	case <-c.session.deadSessionChan:
		// Dead session
		return c.session.errDeadSession()
	case b := <-buffChan:
		switch b.(type) {
		case error:
			if b.(error) == io.EOF {
				c.stream.Close()
			}
			return b.(error)
		case []byte:
			decoder := codec.NewDecoderBytes(b.([]byte), c.session.handler)
			decodeErr := decoder.Decode(message)
			if decodeErr != nil {
				return decodeErr
			}
			return nil
		default:
			panic("unknown type")
		}
	}
}

func (c *channel) handleReadData(buffChan chan interface{}) {
	buf, err := c.stream.ReadData()
	if err != nil {
		buffChan<-err
	} else {
		buffChan<-buf
	}
}

// Close closes the underlying stream, causing any subsequent
// sends to throw an error and receives to return EOF.
func (c *channel) Close() error {
	return c.stream.Close()
}

type byteStream struct {
	stream      io.ReadWriteCloser
	referenceID uint64
	session     *Transport
}

func (b *byteStream) Read(p []byte) (n int, err error) {
	if b == nil || b.stream == nil {
		return 0, errors.New("byte stream is nil")
	}
	return b.stream.Read(p)
}

func (b *byteStream) Write(data []byte) (n int, err error) {
	if b == nil || b.stream == nil {
		return 0, errors.New("byte stream is nil")
	}
	return b.stream.Write(data)
}

func (b *byteStream) Close() error {
	if b == nil || b.stream == nil {
		return errors.New("byte stream is nil")
	}
	return b.stream.Close()
}
