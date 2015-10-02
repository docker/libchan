package libchan

import (
	"fmt"
	"io"
	"reflect"
)

type pSender chan<- interface{}
type pReceiver <-chan interface{}

func (s pSender) Close() error {
	close(s)
	return nil
}

func (s pSender) Send(msg interface{}) error {
	s <- msg
	return nil
}

type messageDecoder interface {
	Decode(v ...interface{}) error
}

func (r pReceiver) Receive(msg interface{}) error {
	rmsg, ok := <-r
	if !ok {
		return io.EOF
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

func (r pReceiver) SendTo(dst Sender) (int, error) {
	var n int
	for {
		pmsg, ok := <-r
		if !ok {
			break
		}
		if err := dst.Send(pmsg); err != nil {
			return n, err
		}
		n++
	}
	return n, nil
}

// Pipe returns an inmemory Sender/Receiver pair.
func Pipe() (Receiver, Sender) {
	c := make(chan interface{})
	return pReceiver(c), pSender(c)
}

// BufferedPipe returns an inmemory buffered pipe.
func BufferedPipe(n int) (Receiver, Sender) {
	c := make(chan interface{}, n)
	return pReceiver(c), pSender(c)
}
