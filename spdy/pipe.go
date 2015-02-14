package spdy

import (
	"net"

	"github.com/docker/libchan"
)

type pipeSender struct {
	session *Transport
	sender  *channel
}

type pipeReceiver struct {
	session  *Transport
	receiver *channel
}

// Pipe creates a top-level channel pipe using an in memory transport.
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
	return &pipeSender{s1, sender.(*channel)}, &pipeReceiver{s2, receiver.(*channel)}, nil
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

func (p *pipeReceiver) Receive(message interface{}) error {
	return p.receiver.Receive(message)
}

func (p *pipeReceiver) SendTo(dst libchan.Sender) (int, error) {
	return p.receiver.SendTo(dst)
}
