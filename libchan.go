package libchan

import (
	"io"
	"net"
)

// Transport represents a connection which can multiplex channels and
// bytestreams.
type Transport interface {
	// NewSendChannel creates and returns a new send channel.  The receive
	// end will get picked up on the remote end of the transport through
	// the remote calling WaitReceiveChannel.
	NewSendChannel() (Sender, error)

	// WaitReceiveChannel waits for a new channel be created by the
	// remote end of the transport calling NewSendChannel.
	WaitReceiveChannel() (Receiver, error)

	// RegisterConn registers a network connection to be used
	// by inbound messages referring to the connection
	// with the registered connection's local and remote address.
	// Note: a connection does not need to be registered before
	// being sent in a message, but does need to be registered
	// to by the receiver of a message. If registration should be
	// automatic, register a listener instead.
	RegisterConn(net.Conn) error

	// RegisterListener accepts all connections from the listener
	// and immediately registers them.
	RegisterListener(net.Listener)

	// Unregister removes the connection from the list of known
	// connections. This should be called when a connection is
	// closed and no longer expected in inbound messages.
	// Failure to unregister connections will increase memory
	// usage since the transport is not notified of closed
	// connections to automatically unregister.
	Unregister(net.Conn)
}

// Sender is a channel which sent messages of any content
// including other channels and bytestreams.
type Sender interface {
	// Send sends a message across the channel to a receiver on the
	// other side of the underlying transport.
	Send(message interface{}) error

	// Close closes the channel.
	Close() error

	// CreateByteStream creates a new byte stream using the
	// default byte stream type of the underlying transport.
	CreateByteStream() (io.ReadWriteCloser, error)

	// CreatePipeReceiver creates a receive-only pipe.  The sender
	// should be sent across the channel, any call to send directly
	// will throw an error on any channel on a transport.
	CreateNestedReceiver() (Receiver, Sender, error)

	// CreateNestedSender creates a send-only pipe.  The receiver
	// should be sent across the channel, any call to receive directly
	// will throw an error on any channel on a transport.
	CreateNestedSender() (Sender, Receiver, error)
}

// Receiver is a channel which can receive messages of any
// content including other channels and bytestreams.
type Receiver interface {
	// Receive receives a message sent across the channel from
	// a sender on the other side of the underlying transport.
	// Receive is expected to receive the same object that was
	// sent by the Sender, any differences between the
	// receive and send type should be handled carefully.  It is
	// up to the application to determine type compatibility, if
	// the receive object is incompatible, Receiver will
	// throw an error.
	Receive(message interface{}) error

	// Close closes the channel.  Closing does not keep the remote
	// sender from sending messages.  Normally a receiver can be
	// automatically closed through receiving an EOF.
	Close() error
}
