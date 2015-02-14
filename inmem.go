package libchan

import (
	"fmt"
	"io"
	"reflect"
	"sync"
)

// Pipe returns an inmemory Sender/Receiver pair.
func Pipe() (Receiver, Sender) {
	p := new(pipe)
	p.rwait.L = &p.l
	p.wwait.L = &p.l
	r := &pipeReceiver{p}
	w := &pipeSender{p}
	return r, w
}

type pipe struct {
	rwait sync.Cond
	wwait sync.Cond
	l     sync.Mutex
	rl    sync.Mutex
	wl    sync.Mutex
	rerr  error // if reader closed, error to give writes
	werr  error // if writer closed, error to give reads
	msg   interface{}
}

func (p *pipe) send(msg interface{}) error {
	var err error
	// One writer at a time.
	p.wl.Lock()
	defer p.wl.Unlock()

	p.l.Lock()
	defer p.l.Unlock()
	p.msg = msg
	p.rwait.Signal()
	for {
		if p.msg == nil {
			break
		}
		if p.rerr != nil {
			err = p.rerr
			break
		}
		if p.werr != nil {
			err = io.ErrClosedPipe
		}
		p.wwait.Wait()
	}
	p.msg = nil // in case of rerr or werr
	return err
}

func (p *pipe) receive() (interface{}, error) {
	p.rl.Lock()
	defer p.rl.Unlock()

	p.l.Lock()
	defer p.l.Unlock()
	for {
		if p.rerr != nil {
			return nil, io.ErrClosedPipe
		}
		if p.msg != nil {
			break
		}
		if p.werr != nil {
			return nil, p.werr
		}
		p.rwait.Wait()
	}
	msg := p.msg
	p.msg = nil
	p.wwait.Signal()
	return msg, nil
}

func (p *pipe) rclose(err error) {
	if err == nil {
		err = io.ErrClosedPipe
	}
	p.l.Lock()
	defer p.l.Unlock()
	p.rerr = err
	p.rwait.Signal()
	p.wwait.Signal()
}

func (p *pipe) wclose(err error) {
	if err == nil {
		err = io.EOF
	}
	p.l.Lock()
	defer p.l.Unlock()
	p.werr = err
	p.rwait.Signal()
	p.wwait.Signal()
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
	return r.CloseWithError(nil)
}

func (r *pipeReceiver) CloseWithError(err error) error {
	r.p.rclose(err)
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
	return w.CloseWithError(nil)
}

func (w *pipeSender) CloseWithError(err error) error {
	w.p.wclose(err)
	return nil
}
