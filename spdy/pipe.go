package spdy

import (
	"io"
	"net"

	"github.com/docker/libchan"
)

type pipeSender struct {
	session libchan.Transport
	sender  *sender
}

type pipeReceiver struct {
	session  libchan.Transport
	receiver *receiver
}

// Pipe creates a top-level channel pipe using an in memory transport.
func Pipe() (libchan.Receiver, libchan.Sender, error) {
	c1, c2 := net.Pipe()

	s1, err := NewSpdyStreamProvider(c1, false)
	if err != nil {
		return nil, nil, err
	}
	t1 := NewTransport(s1, &MsgpackCodec{})

	s2, err := NewSpdyStreamProvider(c2, true)
	if err != nil {
		return nil, nil, err
	}
	t2 := NewTransport(s2, &MsgpackCodec{})

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
	return &pipeReceiver{t2, recv.(*receiver)}, &pipeSender{t1, send.(*sender)}, nil
}

func (p *pipeSender) Send(message interface{}) error {
	return p.sender.Send(message)
}

func (p *pipeSender) Close() error {
	err := p.sender.Close()
	if err != nil {
		return err
	}
	if closer, ok := p.session.(io.Closer); ok {
		return closer.Close()
	}
	return nil
}

func (p *pipeReceiver) Receive(message interface{}) error {
	return p.receiver.Receive(message)
}

func (p *pipeReceiver) SendTo(dst libchan.Sender) (int, error) {
	return p.receiver.SendTo(dst)
}
