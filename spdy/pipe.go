package spdy

import (
	"io"
	"net"

	"github.com/docker/libchan"
)

type pipeSender struct {
	session *Transport
	sender  libchan.Sender
}

type pipeReceiver struct {
	session  *Transport
	receiver libchan.Receiver
}

// Pipe creates a channel pipe using an in memory transport.
func Pipe() (libchan.Sender, libchan.Receiver, error) {
	c1, c2 := net.Pipe()

	s1, err := newSession(c1, false)
	if err != nil {
		return nil, nil, err
	}

	s2, err := newSession(c2, true)
	if err != nil {
		return nil, nil, err
	}

	var receiver libchan.Receiver
	waitError := make(chan error)

	go func() {
		var err error
		receiver, err = s2.WaitReceiveChannel()
		waitError <- err
	}()

	sender, senderErr := s1.NewSendChannel()
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
	return &pipeSender{s1, sender}, &pipeReceiver{s2, receiver}, nil
}

func (p *pipeSender) Send(message interface{}) error {
	return p.sender.Send(message)
}

func (p *pipeSender) Close() error {
	err := p.sender.Close()
	if err != nil {
		return err
	}
	return p.session.Close()
}

func (p *pipeSender) CreateByteStream() (io.ReadWriteCloser, error) {
	return p.sender.CreateByteStream()
}

func (p *pipeSender) CreateNestedReceiver() (libchan.Receiver, libchan.Sender, error) {
	return p.sender.CreateNestedReceiver()
}

func (p *pipeSender) CreateNestedSender() (libchan.Sender, libchan.Receiver, error) {
	return p.sender.CreateNestedSender()
}

func (p *pipeReceiver) Receive(message interface{}) error {
	return p.receiver.Receive(message)
}

func (p *pipeReceiver) Close() error {
	err := p.receiver.Close()
	if err != nil {
		return err
	}
	return p.session.Close()
}
