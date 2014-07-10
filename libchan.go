package libchan

import (
	"errors"
	"io"
)

type Direction uint8
type ReferenceId uint64

const (
	Out = Direction(0x01)
	In  = Direction(0x02)
)

type TransportSession interface {
	RegisterListener(listener ByteStreamListener)
	NewSendChannel() Channel
	WaitReceiveChannel() Channel
}

type Channel interface {
	CreateByteStream(provider ByteStreamDialer) (*ByteStream, error)
	CreateSubChannel(Direction) (Channel, error)
	Communicate(message interface{}) error
	Close() error
	Direction() Direction
}

type ByteStream struct {
	Stream io.ReadWriteCloser
	ReferenceId
}

func (b *ByteStream) Read(p []byte) (n int, err error) {
	if b == nil || b.Stream == nil {
		return 0, errors.New("Byte stream is nil")
	}
	return b.Stream.Read(p)
}

func (b *ByteStream) Write(data []byte) (n int, err error) {
	if b == nil || b.Stream == nil {
		return 0, errors.New("Byte stream is nil")
	}
	return b.Stream.Write(data)
}

func (b *ByteStream) Close() error {
	if b == nil || b.Stream == nil {
		return errors.New("Byte stream is nil")
	}
	return b.Stream.Close()
}

type ByteStreamListener interface {
	Accept() (*ByteStream, error)
	Close() error
}

type ByteStreamDialer interface {
	Dial(referenceId ReferenceId) (*ByteStream, error)
}

type Sender interface {
	Send(msg *Message) (Receiver, error)
	Close() error
}

type Receiver interface {
	Receive(mode int) (*Message, error)
}

type Message struct {
	Data   []byte
	Stream io.ReadWriteCloser
	Ret    Sender
}

const (
	Ret int = 1 << iota
	// FIXME: use an `Att` flag to auto-close attachments by default
)

type ReceiverFrom interface {
	ReceiveFrom(Receiver) (int, error)
}

type SenderTo interface {
	SendTo(Sender) (int, error)
}

var (
	ErrIncompatibleSender   = errors.New("incompatible sender")
	ErrIncompatibleReceiver = errors.New("incompatible receiver")
)

// RetPipe is a special value for `Message.Ret`.
// When a Message is sent with `Ret=SendPipe`, the transport must
// substitute it with the writing end of a new pipe, and return the
// other end as a return value.
type retPipe struct {
	NopSender
}

var RetPipe = retPipe{}

func (r retPipe) Equals(val Sender) bool {
	if rval, ok := val.(retPipe); ok {
		return rval == r
	}
	return false
}

func Repeater(payload *Message) Sender {
	return Handler(func(msg *Message) {
		msg.Ret.Send(payload)
	})
}
