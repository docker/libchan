package unix

import (
	"testing"

	lch "github.com/docker/libchan"
	"github.com/dotcloud/docker/pkg/testutils"
)

func TestPair(t *testing.T) {
	r, w, err := Pair()
	if err != nil {
		t.Fatal("Unexpected error")
	}
	defer r.Close()
	defer w.Close()
	testutils.Timeout(t, func() {
		go func() {
			msg, err := r.Receive(0)
			if err != nil {
				t.Fatal(err)
			}
			if string(msg.Data) != "hello world" {
				t.Fatalf("%#v", *msg)
			}
		}()
		_, err := w.Send(&lch.Message{Data: []byte("hello world")})
		if err != nil {
			t.Fatal(err)
		}
	})
}

func TestSendReply(t *testing.T) {
	r, w, err := Pair()
	if err != nil {
		t.Fatal(err)
	}
	defer r.Close()
	defer w.Close()
	testutils.Timeout(t, func() {
		// Send
		go func() {
			// Send a message with mode=R
			ret, err := w.Send(&lch.Message{Data: []byte("this is the request"), Ret: lch.RetPipe})
			if err != nil {
				t.Fatal(err)
			}
			// Read for a reply
			msg, err := ret.Receive(0)
			if err != nil {
				t.Fatal(err)
			}
			if string(msg.Data) != "this is the reply" {
				t.Fatalf("%#v", msg)
			}
		}()
		// Receive a message with mode=W
		msg, err := r.Receive(lch.Ret)
		if err != nil {
			t.Fatal(err)
		}
		if string(msg.Data) != "this is the request" {
			t.Fatalf("%#v", msg)
		}
		// Send a reply
		_, err = msg.Ret.Send(&lch.Message{Data: []byte("this is the reply")})
		if err != nil {
			t.Fatal(err)
		}
	})
}
