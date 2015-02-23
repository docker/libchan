package spdy

import (
	"bufio"
	"errors"
	"io"
	"net/http"
	"reflect"
	"strconv"
	"sync"
	"sync/atomic"

	"github.com/dmcgowan/streams"
	"github.com/docker/libchan"
	"github.com/docker/libchan/encoding"
)

var (
	// ErrOperationNotAllowed occurs when an action is attempted
	// on a nop object.
	ErrOperationNotAllowed = errors.New("operation not allowed")
)

// Transport is a transport session on top of a network
// connection using spdy.
type Transport struct {
	provider         streams.StreamProvider
	referenceCounter uint64
	receiverChan     chan *receiver
	streamC          *sync.Cond
	streams          map[uint64]*stream
	codec            encoding.ChannelCodec
}

type stream struct {
	referenceID uint64
	parentID    uint64
	stream      streams.Stream
	session     *Transport
}

type sender struct {
	stream     *stream
	encodeLock sync.Mutex
	encoder    encoding.Encoder
	buffer     *bufio.Writer
}

type receiver struct {
	stream     *stream
	decodeLock sync.Mutex
	decoder    encoding.Decoder
}

// NewTransport returns an object implementing the
// libchan Transport interface using a stream provider.
func NewTransport(provider streams.StreamProvider, codec encoding.ChannelCodec) libchan.Transport {
	session := &Transport{
		provider:         provider,
		referenceCounter: 1,
		receiverChan:     make(chan *receiver),
		streamC:          sync.NewCond(new(sync.Mutex)),
		streams:          make(map[uint64]*stream),
		codec:            codec,
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

	// TODO: Do not store reference
	s.streamC.L.Lock()
	s.streams[referenceID] = newStream
	s.streamC.L.Unlock()

	return newStream, nil

}
func (s *stream) CreateStream() (io.ReadWriteCloser, uint64, error) {
	strm, err := s.session.createSubStream(s.referenceID)
	if err != nil {
		return nil, 0, err
	}
	return strm, strm.referenceID, nil
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

// CreateReceiver creates a new channel returning the local
// receiver and the remote sender identifier.
func (s *stream) CreateReceiver() (libchan.Receiver, uint64, error) {
	stream, err := s.session.createSubStream(s.referenceID)
	if err != nil {
		return nil, 0, err
	}

	return &receiver{stream: stream}, uint64(stream.referenceID), err
}

// CreateSender creates a new channel returning the local
// sender and the remote receiver identifier.
func (s *stream) CreateSender() (libchan.Sender, uint64, error) {
	stream, err := s.session.createSubStream(s.referenceID)
	if err != nil {
		return nil, 0, err
	}

	return &sender{stream: stream}, uint64(stream.referenceID), err
}

func (s *stream) GetSender(sid uint64) (libchan.Sender, error) {
	strm := s.session.getStream(sid)
	if strm == nil {
		return nil, errors.New("sender does not exist")
	}
	return &sender{stream: strm}, nil
}

func (s *stream) GetReceiver(sid uint64) (libchan.Receiver, error) {
	strm := s.session.getStream(sid)
	if strm == nil {
		return nil, errors.New("sender does not exist")
	}
	return &receiver{stream: strm}, nil
}

func (s *stream) GetStream(sid uint64) (io.ReadWriteCloser, error) {
	strm := s.session.getStream(sid)
	if strm == nil {
		return nil, errors.New("sender does not exist")
	}
	return strm, nil
}

// Send sends a message across the channel to a receiver on the
// other side of the transport.
func (s *sender) Send(message interface{}) error {
	s.encodeLock.Lock()
	defer s.encodeLock.Unlock()
	if s.encoder == nil {
		s.buffer = bufio.NewWriter(s.stream)
		s.encoder = s.stream.session.codec.NewEncoder(s.buffer, s.stream)
	}

	if err := s.encoder.Encode(message); err != nil {
		return err
	}

	return s.buffer.Flush()
}

// Close closes the underlying stream, causing any subsequent
// sends to throw an error and receives to return EOF.
func (s *sender) Close() error {
	return s.stream.stream.Close()
}

var (
	streamT = reflect.TypeOf(&stream{})
	recvT   = reflect.TypeOf(&receiver{})
	sendT   = reflect.TypeOf(&sender{})
)

// Receive receives a message sent across the channel from
// a sender on the other side of the transport.
func (r *receiver) Receive(message interface{}) error {
	r.decodeLock.Lock()
	defer r.decodeLock.Unlock()
	if r.decoder == nil {
		r.decoder = r.stream.session.codec.NewDecoder(r.stream, r.stream, streamT, recvT, sendT)
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
		rm := r.stream.session.codec.NewRawMessage()
		if err := r.Receive(rm); err == io.EOF {
			break
		} else if err != nil {
			return n, err
		}

		if err := dst.Send(rm); err != nil {
			return n, err
		}
		n++
	}
	return n, nil
}

func (s *stream) Read(b []byte) (int, error) {
	return s.stream.Read(b)
}

func (s *stream) Write(b []byte) (int, error) {
	return s.stream.Write(b)
}

func (s *stream) Close() error {
	return s.stream.Close()
}
