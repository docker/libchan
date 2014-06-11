package utils

import (
	"github.com/docker/libchan"
	"testing"
)

func TestSendRet(t *testing.T) {
	r, w := libchan.Pipe()
	defer r.Close()
	defer w.Close()
	q := NewQueue(w, 1)
	defer q.Close()
	ret, err := q.Send(&libchan.Message{Data: []byte("ping"), Ret: libchan.RetPipe})
	if err != nil {
		t.Fatal(err)
	}
	go func() {
		ping, err := r.Receive(libchan.Ret)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := ping.Ret.Send(&libchan.Message{Data: []byte("pong")}); err != nil {
			t.Fatal(err)
		}
	}()
	_, err = ret.Receive(0)
	if err != nil {
		t.Fatal(err)
	}
}

func TestSendClose(t *testing.T) {
	q := NewQueue(libchan.NopSender{}, 1)
	q.Send(&libchan.Message{Data: []byte("hello")})
	q.Close()
	if _, err := q.Send(&libchan.Message{Data: []byte("again")}); err == nil {
		t.Fatal("send on closed queue should return an error")
	}
}
