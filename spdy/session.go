package spdy

import (
	"bufio"
	"errors"
	"io"
	"net/http"
	"strconv"
	"sync"
	"sync/atomic"

	"github.com/dmcgowan/msgpack"
	"github.com/docker/libchan"
)

var (
	// ErrOperationNotAllowed occurs when an action is attempted
	// on a nop object.
	ErrOperationNotAllowed = errors.New("operation not allowed")
)

// Transport is a transport session on top of a network
// connection using spdy.
type Transport struct {
	provider         StreamProvider
	referenceCounter uint64
	receiverChan     chan *receiver
	streamC          *sync.Cond
	streams          map[uint64]*stream
}

type stream struct {
	referenceID uint64
	parentID    uint64
	stream      Stream
	session     *Transport
}

type sender struct {
	stream     *stream
	encodeLock sync.Mutex
	encoder    *msgpack.Encoder
	buffer     *bufio.Writer
}

type receiver struct {
	stream     *stream
	decodeLock sync.Mutex
	decoder    *msgpack.Decoder
}

type nopReceiver struct {
	stream *stream
}

type nopSender struct {
	stream *stream
}

// NewTransport returns an object implementing the
// libchan Transport interface using a stream provider.
func NewTransport(provider StreamProvider) libchan.Transport {
	session := &Transport{
		provider:         provider,
		referenceCounter: 1,
		receiverChan:     make(chan *receiver),
		streamC:          sync.NewCond(new(sync.Mutex)),
		streams:          make(map[uint64]*stream),
	}

	go session.handleStreams()

	return session
}

func (s *Transport) handleStreams() {
	listener := s.provider.Listen()
	for {
		strm, err := listener.Accept()
		if err != nil {
			if err != io.EOF {
				// TODO: strm handle error
			}
			break
		}
		headers := strm.Headers()
		referenceIDString := headers.Get("libchan-ref")
		parentIDString := headers.Get("libchan-parent-ref")

		referenceID, parseErr := strconv.ParseUint(referenceIDString, 10, 64)
		if parseErr != nil {
			// TODO: bad strm error
			strm.Reset()
		} else {
			var parentID uint64
			if parentIDString != "" {
				parsedID, parseErr := strconv.ParseUint(parentIDString, 10, 64)
				if parseErr != nil {
					// TODO: bad strm error
					strm.Reset()
				}
				parentID = parsedID
			}

			newStream := &stream{
				referenceID: referenceID,
				parentID:    parentID,
				stream:      strm,
				session:     s,
			}

			s.streamC.L.Lock()
			s.streams[referenceID] = newStream
			s.streamC.Broadcast()
			s.streamC.L.Unlock()

			if parentID == 0 {
				s.receiverChan <- &receiver{stream: newStream}
			}

		}
	}
}

func (s *Transport) getStream(referenceID uint64) *stream {
	s.streamC.L.Lock()
	c, ok := s.streams[referenceID]
	for i := 0; i < 10 && !ok; i++ {
		s.streamC.Wait()
		c, ok = s.streams[referenceID]
	}
	s.streamC.L.Unlock()
	return c

}

func (s *Transport) removeStream(stream *stream) {
	s.streamC.L.Lock()
	delete(s.streams, stream.referenceID)
	s.streamC.L.Unlock()
	s.streamC.Broadcast()
}

// Close closes the underlying stream provider
func (s *Transport) Close() error {
	return s.provider.Close()
}

func (s *Transport) createSubStream(parentID uint64) (*stream, error) {
	referenceID := atomic.AddUint64(&s.referenceCounter, 1)
	headers := http.Header{}
	headers.Set("libchan-ref", strconv.FormatUint(referenceID, 10))
	if parentID > 0 {
		headers.Set("libchan-parent-ref", strconv.FormatUint(parentID, 10))
	}

	providedStream, err := s.provider.NewStream(headers)
	if err != nil {
		return nil, err
	}

	newStream := &stream{
		referenceID: referenceID,
		parentID:    parentID,
		stream:      providedStream,
		session:     s,
	}

	// TODO: hold reference to the newly created stream
	// for possible cleanup. This stream should not be put
	// in the streams maps which holds remotely created
	// streams and will can have reference id conflicts.

	return newStream, nil

}
func (s *stream) createByteStream() (*stream, error) {
	return s.session.createSubStream(s.referenceID)
}

// NewSendChannel creates and returns a new send channel.  The receive
// end will get picked up on the remote end through the remote calling
// WaitReceiveChannel.
func (s *Transport) NewSendChannel() (libchan.Sender, error) {
	stream, err := s.createSubStream(0)
	if err != nil {
		return nil, err
	}

	// TODO check synchronized
	return &sender{stream: stream}, nil
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

// CreateNestedReceiver creates a new channel returning the local
// receiver and the remote sender.  The remote sender needs to be
// sent across the channel before being utilized.
func (s *stream) CreateNestedReceiver() (libchan.Receiver, libchan.Sender, error) {
	stream, err := s.session.createSubStream(s.referenceID)
	if err != nil {
		return nil, nil, err
	}

	return &receiver{stream: stream}, &nopSender{stream: stream}, err
}

// CreateNestedReceiver creates a new channel returning the local
// sender and the remote receiver.  The remote receiver needs to be
// sent across the channel before being utilized.
func (s *stream) CreateNestedSender() (libchan.Sender, libchan.Receiver, error) {
	stream, err := s.session.createSubStream(s.referenceID)
	if err != nil {
		return nil, nil, err
	}

	return &sender{stream: stream}, &nopReceiver{stream: stream}, err
}

// Send sends a message across the channel to a receiver on the
// other side of the transport.
func (s *sender) Send(message interface{}) error {
	s.encodeLock.Lock()
	defer s.encodeLock.Unlock()
	if s.encoder == nil {
		s.buffer = bufio.NewWriter(s.stream)
		s.encoder = msgpack.NewEncoder(s.buffer)
		s.encoder.AddExtensions(s.stream.initializeExtensions())
	}

	if err := s.encoder.Encode(message); err != nil {
		return err
	}

	return s.buffer.Flush()
}

// Close closes the underlying stream, causing any subsequent
// sends to throw an error and receives to return EOF.
func (s *sender) Close() error {
	return s.stream.Close()
}

// Receive receives a message sent across the channel from
// a sender on the other side of the transport.
func (r *receiver) Receive(message interface{}) error {
	r.decodeLock.Lock()
	defer r.decodeLock.Unlock()
	if r.decoder == nil {
		r.decoder = msgpack.NewDecoder(r.stream)
		r.decoder.AddExtensions(r.stream.initializeExtensions())
	}

	decodeErr := r.decoder.Decode(message)
	if decodeErr == io.EOF {
		r.stream.Close()
		r.decoder = nil
	}
	return decodeErr
}

func (r *receiver) SendTo(dst libchan.Sender) (int, error) {
	var n int
	for {
		var rm msgpack.RawMessage
		if err := r.Receive(&rm); err == io.EOF {
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

func (*nopSender) Send(interface{}) error {
	return ErrOperationNotAllowed
}

func (*nopSender) Close() error {
	return ErrOperationNotAllowed
}

func (*nopReceiver) Receive(interface{}) error {
	return ErrOperationNotAllowed
}

func (*nopReceiver) SendTo(libchan.Sender) (int, error) {
	return 0, ErrOperationNotAllowed
}

func (s *stream) Read(b []byte) (int, error) {
	return s.stream.Read(b)
}

func (s *stream) Write(b []byte) (int, error) {
	return s.stream.Write(b)
}

func (s *stream) Close() error {
	defer s.session.removeStream(s)
	return s.stream.Close()
}
