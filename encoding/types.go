package encoding

import (
	"io"
	"reflect"

	"github.com/docker/libchan"
)

// ChanProducer represents an object which is able to create new
// channels and streams. This interface is used by an encoder
// create a channel or stream, copy the encoded type, and
// encode the identifier.
type ChanProducer interface {
	// CreateSender creates a new send channel and returns
	// the identifier associated with the sender. This
	// identifier can be used to get the Receiver on
	// the receiving side by calling GetReceiver.
	CreateSender() (libchan.Sender, uint64, error)

	// CreateReceiver creates a new receive channel and
	// returns the identifier associated with the receiver.
	// This identifier can be used to get the Sender on
	// the receiving side by calling GetSender.
	CreateReceiver() (libchan.Receiver, uint64, error)

	// CreateStream createsa  new byte stream and returns
	// the identifier associate with the stream.  This
	// identifier can be used to get the byte stream
	// by calling GetStream on the receiving side.
	CreateStream() (io.ReadWriteCloser, uint64, error)
}

// ChanReceiver represents an object which is able to receive
// new channels and streams and retrieve by an integer identifer.
type ChanReceiver interface {
	// GetSender gets a remotely created sender referenced
	// by the given identifier.
	GetSender(uint64) (libchan.Sender, error)

	// GetReceiver gets a remotely created receiver referenced
	// by the given identifier.
	GetReceiver(uint64) (libchan.Receiver, error)

	// GetStream gets a remotely created byte stream
	// referenced by the given identifier.
	GetStream(uint64) (io.ReadWriteCloser, error)
}

// Encoder represents an object which can encode an interface
// into data stream to be decoded. This Encoder must be able
// to encode interfaces by converting to libchan channels and
// streams and encoding the identifier.
type Encoder interface {
	Encode(v ...interface{}) error
}

// Decoder represents an object which can decode from a data
// stream into an interface. The decoder must have support
// for decoding stream and channel identifiers into a libchan
// Sender or Receiver as well as io Readers and Writers.
type Decoder interface {
	Decode(v ...interface{}) error
}

// ChannelCodec represents a libchan codec capable of encoding
// Go interfaces into data streams supporting libchan types as
// well as decode into libchan supported interfaces. In addition
// to encoding and decoding, the codec must provide a transit
// type which is capable of copying a data stream in order to
// delay decoding into an object until finally received.
// The RawMessage must return an object similar to json.RawMessage
// with the capability of decoding itself into an object.
type ChannelCodec interface {
	NewEncoder(io.Writer, ChanProducer) Encoder
	NewDecoder(io.Reader, ChanReceiver, reflect.Type, reflect.Type, reflect.Type) Decoder
	NewRawMessage() Decoder
}
