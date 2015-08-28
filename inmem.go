package libchan

import (
	"fmt"
	"io"
	"reflect"
)

// Pipe returns an inmemory Sender/Receiver pair.
func Pipe() (Receiver, Sender) {
	p := new(pipe)
	p.ch = make(chan interface{})
	r := &pipeReceiver{p}
	w := &pipeSender{p}
	return r, w
}

type pipe struct {
	ch chan interface{}
}

func (p *pipe) send(msg interface{}) (retErr error) {
	retErr = nil
	defer func() {
		if err := recover(); err == "send on closed channel" {
			retErr = io.ErrClosedPipe
		} else if err != nil {
			panic(err)
		}
	}()
	p.ch <- msg
	return
}

func (p *pipe) receive() (interface{}, error) {
	msg, ok := <-p.ch
	if !ok {
		return nil, io.EOF
	}
	return msg, nil
}

func (p *pipe) close() {
	close(p.ch)
}

type messageDecoder interface {
	Decode(v ...interface{}) error
}

type pipeReceiver struct {
	p *pipe
}

func (r *pipeReceiver) Receive(msg interface{}) error {
	rmsg, err := r.p.receive()
	if err != nil {
		return err
	}
	// check type
	v := reflect.ValueOf(msg)
	rv := reflect.ValueOf(rmsg)
	if v.Type() == rv.Type() {
		if v.Kind() == reflect.Ptr {
			v.Elem().Set(rv.Elem())
		} else {
			v.Set(rv)
		}
	} else {
		switch msg.(type) {
		case *interface{}:
			v.Elem().Set(rv)
		default:
			switch rval := rmsg.(type) {
			case messageDecoder:
				return rval.Decode(msg)
			default:
				return fmt.Errorf("Cannot receive %T into %T", rmsg, msg)
			}
		}
	}

	return nil
}

func (r *pipeReceiver) SendTo(dst Sender) (int, error) {
	var n int
	// If the destination is a pipeSender, we can cheat
	pdst, ok := dst.(*pipeSender)
	if !ok {
		return 0, ErrIncompatibleSender
	}
	for {
		pmsg, err := r.p.receive()
		if err == io.EOF {
			break
		}
		if err != nil {
			return n, err
		}
		if err := pdst.p.send(pmsg); err != nil {
			return n, err
		}
		n++
	}
	return n, nil
}

func (r *pipeReceiver) Close() error {
	r.p.close()
	return nil
}

// pipeSender

type pipeSender struct {
	p *pipe
}

func (w *pipeSender) Send(msg interface{}) error {
	return w.p.send(msg)
}

func (w *pipeSender) ReceiveFrom(src Receiver) (int, error) {
	var n int
	// If the destination is a pipeReceiver, we can cheat
	psrc, ok := src.(*pipeReceiver)
	if !ok {
		return 0, ErrIncompatibleReceiver
	}
	for {
		pmsg, err := psrc.p.receive()
		if err == io.EOF {
			break
		}
		if err != nil {
			return n, err
		}
		if err := w.p.send(pmsg); err != nil {
			return n, err
		}
		n++
	}
	return n, nil
}

func (w *pipeSender) Close() error {
	w.p.close()
	return nil
}
