package spdy

import (
	"bufio"
	"errors"
	"io"
	"net/http"
	"strconv"
	"sync"

	"github.com/dmcgowan/msgpack"
	"github.com/docker/libchan"
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
	provider StreamProvider

	// TODO unify channel and streams maps with libchanStream type
	receiverChan chan *channel
	channelC     *sync.Cond
	channels     map[uint64]*channel

	referenceLock    sync.Mutex
	referenceCounter uint64
	byteStreamC      *sync.Cond
	byteStreams      map[uint64]*byteStream
}

type channel struct {
	referenceID uint64
	parentID    uint64
	stream      Stream
	session     *Transport
	direction   direction
	encodeLock  sync.Mutex
	encoder     *msgpack.Encoder
	decodeLock  sync.Mutex
	decoder     *msgpack.Decoder
	buffer      *bufio.Writer
}

// NewTransport returns an object implementing the
// libchan Transport interface using a stream provider.
func NewTransport(provider StreamProvider) libchan.Transport {
	session := &Transport{
		provider:         provider,
		receiverChan:     make(chan *channel),
		channelC:         sync.NewCond(new(sync.Mutex)),
		channels:         make(map[uint64]*channel),
		referenceCounter: 1,
		byteStreamC:      sync.NewCond(new(sync.Mutex)),
		byteStreams:      make(map[uint64]*byteStream),
	}

	go session.handleStreams()

	return session
}

func (s *Transport) handleStreams() {
	listener := s.provider.Listen()
	for {
		stream, err := listener.Accept()
		if err != nil {
			if err != io.EOF {
				// TODO: stream handle error
			}
			break
		}
		headers := stream.Headers()
		referenceIDString := headers.Get("libchan-ref")
		parentIDString := headers.Get("libchan-parent-ref")

		referenceID, parseErr := strconv.ParseUint(referenceIDString, 10, 64)
		if parseErr != nil {
			// TODO: bad stream error
			stream.Reset()
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
			} else {
				parentID, parseErr := strconv.ParseUint(parentIDString, 10, 64)
				if parseErr != nil {
					// TODO: bad stream error
					stream.Reset()
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

				}
			}
		}
	}
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

func (s *Transport) dial(referenceID, parentID uint64) (*byteStream, error) {
	headers := http.Header{}
	headers.Set("libchan-ref", strconv.FormatUint(referenceID, 10))
	// TODO: Add this to implement protocol
	//headers.Set("libchan-parent-ref", strconv.FormatUint(parentID, 10))
	stream, streamErr := s.provider.NewStream(headers)
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
	s.referenceCounter = referenceID + 2
	s.referenceLock.Unlock()
	return referenceID
}

func (c *channel) createByteStream() (io.ReadWriteCloser, error) {
	referenceID := c.session.nextReferenceID()

	byteStream, bsErr := c.session.dial(referenceID, c.referenceID)
	if bsErr != nil {
		return nil, bsErr
	}
	byteStream.referenceID = referenceID

	c.session.byteStreamC.L.Lock()
	c.session.byteStreams[referenceID] = byteStream
	c.session.byteStreamC.L.Unlock()

	return byteStream, nil
}

func addrKey(local, remote string) string {
	b := make([]byte, len(local)+len(remote)+2)
	copy(b, local)
	copy(b[len(local):], "<>")
	copy(b[len(local)+2:], remote)
	return string(b)
}

// Close closes the underlying stream provider
func (s *Transport) Close() error {
	return s.provider.Close()
}

// NewSendChannel creates and returns a new send channel.  The receive
// end will get picked up on the remote end through the remote calling
// WaitReceiveChannel.
func (s *Transport) NewSendChannel() (libchan.Sender, error) {
	referenceID := s.nextReferenceID()
	headers := http.Header{}
	headers.Set("libchan-ref", strconv.FormatUint(referenceID, 10))
	headers.Set("libchan-parent-ref", "0")

	stream, streamErr := s.provider.NewStream(headers)
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
	r, ok := <-s.receiverChan
	if !ok {
		return nil, io.EOF
	}

	return r, nil
}

func (c *channel) createSubChannel(direction direction) (libchan.Sender, libchan.Receiver, error) {
	if c.direction == inbound {
		return nil, nil, errors.New("cannot create sub channel of an inbound channel")
	}
	referenceID := c.session.nextReferenceID()
	headers := http.Header{}
	headers.Set("libchan-ref", strconv.FormatUint(referenceID, 10))
	headers.Set("libchan-parent-ref", strconv.FormatUint(c.referenceID, 10))

	stream, streamErr := c.session.provider.NewStream(headers)
	if streamErr != nil {
		return nil, nil, streamErr
	}
	subChannel := &channel{
		referenceID: referenceID,
		parentID:    c.referenceID,
		stream:      stream,
		session:     c.session,
		direction:   direction,
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

	c.encodeLock.Lock()
	defer c.encodeLock.Unlock()
	if c.encoder == nil {
		c.buffer = bufio.NewWriter(c.stream)
		c.encoder = msgpack.NewEncoder(c.buffer)
		c.encoder.AddExtensions(c.initializeExtensions())
	}

	if err := c.encoder.Encode(message); err != nil {
		return err
	}

	return c.buffer.Flush()
}

// Receive receives a message sent across the channel from
// a sender on the other side of the transport.
func (c *channel) Receive(message interface{}) error {
	if c.direction == outbound {
		return ErrWrongDirection
	}

	c.decodeLock.Lock()
	defer c.decodeLock.Unlock()
	if c.decoder == nil {
		c.decoder = msgpack.NewDecoder(c.stream)
		c.decoder.AddExtensions(c.initializeExtensions())
	}

	decodeErr := c.decoder.Decode(message)
	if decodeErr == io.EOF {
		c.stream.Close()
		c.decoder = nil
	}
	return decodeErr
}

func (c *channel) SendTo(dst libchan.Sender) (int, error) {
	if c.direction == outbound {
		return 0, ErrWrongDirection
	}
	var n int
	for {
		var rm msgpack.RawMessage
		if err := c.Receive(&rm); err == io.EOF {
			break
		} else if err != nil {
			return n, err
		}

		if err := dst.Send(&rm); err != nil {
			return n, err
		}
		n++
	}
	return n, nil
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
