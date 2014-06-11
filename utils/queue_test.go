package utils

import (
	"testing"

	"github.com/docker/libchan"
)

func TestSendRet(t *testing.T) {
	r, w := libchan.Pipe()
	defer r.Close()
	defer w.Close()
	q := NewQueue(w, 1)
	defer q.Close()
	ret, err := q.Send(&libchan.Message{Data: []byte("Log"), Ret: libchan.RetPipe})
	if err != nil {
		t.Fatal(err)
	}
	go func() {
		ping, err := r.Receive(libchan.Ret)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := ping.Ret.Send(&libchan.Message{Data: []byte("Log")}); err != nil {
			t.Fatal(err)
		}
	}()
	pong, err := ret.Receive(0)
	if err != nil {
		t.Fatal(err)
	}
	if string(pong.Data) != "Log" {
		t.Fatal(err)
	}
}

func TestSendClose(t *testing.T) {
	q := NewQueue(libchan.NopSender{}, 1)
	q.Send(&libchan.Message{Data: []byte("Error")})
	q.Close()
	if _, err := q.Send(&libchan.Message{Data: []byte("Ack")}); err == nil {
		t.Fatal("send on closed queue should return an error")
	}
}
