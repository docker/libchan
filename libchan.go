package libchan

import (
	"errors"
	"io"
	"net"
)

var (
	ErrWrongDirection = errors.New("wrong channel direction")
)

type Transport interface {
	NewSendChannel() (ChannelSender, error)
	WaitReceiveChannel() (ChannelReceiver, error)

	RegisterConn(net.Conn) error
	RegisterListener(net.Listener)
	Unregister(net.Conn)
}

type ChannelSender interface {
	Send(message interface{}) error
	Close() error

	CreateByteStream() (io.ReadWriteCloser, error)

	// CreatePipeReceiver creates a receive-only pipe.  The sender
	// should be sent across the channel, any call to send directly
	// will throw an error on any channel on a transport.
	CreateNestedReceiver() (ChannelReceiver, ChannelSender, error)

	// CreateNestedSender creates a send-only pipe.  The receiver
	// should be sent across the channel, any call to receive directly
	// will throw an error on any channel on a transport.
	CreateNestedSender() (ChannelSender, ChannelReceiver, error)
}

type ChannelReceiver interface {
	Receive(message interface{}) error
	Close() error
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
