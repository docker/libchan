package libchan

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
}

// Sender is a channel which sent messages of any content
// including other channels and bytestreams.
type Sender interface {
	// Send sends a message across the channel to a receiver on the
	// other side of the underlying transport.
	Send(message interface{}) error

	// Close closes the channel.
	Close() error
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
}
