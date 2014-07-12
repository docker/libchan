package libchan

import (
	"errors"
	"io"
	"net"
)

type Direction uint8

const (
	Out = Direction(0x01)
	In  = Direction(0x02)
)

type TransportSession interface {
	// NewSendChannel creates a new Out channel and returns.
	NewSendChannel() Channel

	// WaitReceiveChannel waits for a new Out channel to be initiated
	// by the other end of the transport and returns the corresponding
	// In channel.
	WaitReceiveChannel() Channel

	// RegisterConn registers a network connection to be used
	// when by inbound messages referring to the connection
	// with the registered connection's local and remote address.
	// Note: a connection does not need to be registered before
	// being sent in a message, but does need to be registered
	// to be received by a message.  If registration should be
	// automatic, register a listener instead.
	RegisterConn(net.Conn)

	// RegisterListener accepts all connections from the listener
	// and immediately registers them.
	RegisterListener(net.Listener)

	// Unregister removes the connection from the list of known
	// connections.  This should be called when a connection is
	// closed and no longer expected in inbound messages.
	// Failure to unregister connections will increase memory
	// usage since the session is not notified of closed
	// connections to automatically unregister.
	Unregister(net.Conn)
}

type Channel interface {
	// CreateByteStream creates a new byte stream using the
	// default byte stream type of the transport.
	CreateByteStream() (io.ReadWriteCloser, error)

	// CreateSubChannel creates a new channel which is a child
	// of the current channel.  The channel can then be sent
	// on the current channel to establish channel nesting.
	// Note: It is an error to create a sub channel on an
	// In channel since there is no way to send it on the
	// parent channel.
	CreateSubChannel(Direction) (Channel, error)

	// Communicate sends or receives a message depending on
	// the direction of the channel.
	Communicate(message interface{}) error

	// Close closes the channel.  Closing on the In side does
	// keep the other side from sending messages.
	Close() error

	// Direction returns the direction of the channel. For
	// each channel, there should be a channel of the opposite
	// direction on the other side of the transport.
	Direction() Direction
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
