package spdy

import (
	"net"

	"github.com/docker/libchan"
	"github.com/docker/libchan/encoding/msgpack"
)

// Pipe creates a top-level channel pipe using an in memory transport.
func Pipe() (libchan.Receiver, libchan.Sender, error) {
	c1, c2 := net.Pipe()

	s1, err := NewSpdyStreamProvider(c1, false)
	if err != nil {
		return nil, nil, err
	}
	t1 := NewTransport(s1, &msgpack.Codec{})

	s2, err := NewSpdyStreamProvider(c2, true)
	if err != nil {
		return nil, nil, err
	}
	t2 := NewTransport(s2, &msgpack.Codec{})

	var recv libchan.Receiver
	waitError := make(chan error)

	go func() {
		var err error
		recv, err = t2.WaitReceiveChannel()
		waitError <- err
	}()

	send, senderErr := t1.NewSendChannel()
	if senderErr != nil {
		c1.Close()
		c2.Close()
		return nil, nil, senderErr
	}

	receiveErr := <-waitError
	if receiveErr != nil {
		c1.Close()
		c2.Close()
		return nil, nil, receiveErr
	}
	return recv, send, nil
}
