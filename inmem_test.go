package libchan

import (
	"fmt"
	"io/ioutil"
	"os"
	"testing"

	"github.com/dotcloud/docker/pkg/testutils"
)

func TestInmemRetPipe(t *testing.T) {
	r, w := Pipe()
	defer r.Close()
	defer w.Close()
	wait := make(chan struct{})
	go func() {
		defer close(wait)
		ret, err := w.Send(&Message{Data: []byte("hello"), Ret: RetPipe})
		if err != nil {
			t.Fatal(err)
		}
		msg, err := ret.Receive(0)
		if err != nil {
			t.Fatal(err)
		}
		if string(msg.Data) != "this better not crash" {
			t.Fatalf("%#v", msg)
		}
	}()
	msg, err := r.Receive(Ret)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := msg.Ret.Send(&Message{Data: []byte("this better not crash")}); err != nil {
		t.Fatal(err)
	}
	<-wait
}

func TestSimpleSend(t *testing.T) {
	r, w := Pipe()
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
		if _, err := w.Send(&Message{Data: []byte("hello world")}); err != nil {
			t.Fatal(err)
		}
	})
}

func TestSendReply(t *testing.T) {
	r, w := Pipe()
	defer r.Close()
	defer w.Close()
	testutils.Timeout(t, func() {
		// Send
		go func() {
			ret, err := w.Send(&Message{Data: []byte("this is the request"), Ret: RetPipe})
			if err != nil {
				t.Fatal(err)
			}
			if ret == nil {
				t.Fatalf("ret = nil\n")
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
		// Receive a message with mode=Ret
		msg, err := r.Receive(Ret)
		if err != nil {
			t.Fatal(err)
		}
		if string(msg.Data) != "this is the request" {
			t.Fatalf("%#v", msg)
		}
		if msg.Ret == nil {
			t.Fatalf("%#v", msg)
		}
		// Send a reply
		_, err = msg.Ret.Send(&Message{Data: []byte("this is the reply")})
		if err != nil {
			t.Fatal(err)
		}
	})
}

func TestSendFile(t *testing.T) {
	r, w := Pipe()
	defer r.Close()
	defer w.Close()
	tmp, err := ioutil.TempFile("", "libchan-test-")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(tmp.Name())
	fmt.Fprintf(tmp, "hello world\n")
	tmp.Sync()
	tmp.Seek(0, 0)
	testutils.Timeout(t, func() {
		go func() {
			_, err := w.Send(&Message{Data: []byte("path=" + tmp.Name()), Fd: tmp})
			if err != nil {
				t.Fatal(err)
			}
		}()
		msg, err := r.Receive(0)
		if err != nil {
			t.Fatal(err)
		}
		if string(msg.Data) != "path="+tmp.Name() {
			t.Fatalf("%#v", msg)
		}
		txt, err := ioutil.ReadAll(msg.Fd)
		if err != nil {
			t.Fatal(err)
		}
		if string(txt) != "hello world\n" {
			t.Fatalf("%s\n", txt)
		}
	})
}
