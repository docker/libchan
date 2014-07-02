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
		message := &Message{Ret: RetPipe}
		encodeErr := message.Encode([]byte("hello"))
		if encodeErr != nil {
			t.Fatal(encodeErr)
		}
		ret, err := w.Send(message)
		if err != nil {
			t.Fatal(err)
		}
		msg, err := ret.Receive(0)
		if err != nil {
			t.Fatal(err)
		}
		var data []byte
		err = msg.Decode(&data)
		if err != nil {
			t.Fatal(err)
		}
		if string(data) != "this better not crash" {
			t.Fatalf("%#v", msg)
		}
	}()
	msg, err := r.Receive(Ret)
	if err != nil {
		t.Fatal(err)
	}
	message := &Message{}
	encodeErr := message.Encode([]byte("this better not crash"))
	if encodeErr != nil {
		t.Fatal(encodeErr)
	}
	if _, err := msg.Ret.Send(message); err != nil {
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
			var data []byte
			err = msg.Decode(&data)
			if err != nil {
				t.Fatal(err)
			}
			if string(data) != "hello world" {
				t.Fatalf("%#v", *msg)
			}
		}()
		message := &Message{}
		encodeErr := message.Encode([]byte("hello world"))
		if encodeErr != nil {
			t.Fatal(encodeErr)
		}
		if _, err := w.Send(message); err != nil {
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
			message := &Message{Ret: RetPipe}
			encodeErr := message.Encode([]byte("this is the request"))
			if encodeErr != nil {
				t.Fatal(encodeErr)
			}
			ret, err := w.Send(message)
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
			var data []byte
			decodeErr := msg.Decode(&data)
			if decodeErr != nil {
				t.Fatal(decodeErr)
			}
			if string(data) != "this is the reply" {
				t.Fatalf("%#v", msg)
			}
		}()
		// Receive a message with mode=Ret
		msg, err := r.Receive(Ret)
		if err != nil {
			t.Fatal(err)
		}
		var data []byte
		decodeErr := msg.Decode(&data)
		if decodeErr != nil {
			t.Fatal(decodeErr)
		}
		if string(data) != "this is the request" {
			t.Fatalf("%#v", msg)
		}
		if msg.Ret == nil {
			t.Fatalf("%#v", msg)
		}
		// Send a reply
		message := &Message{}
		encodeErr := message.Encode([]byte("this is the reply"))
		if encodeErr != nil {
			t.Fatal(encodeErr)
		}
		_, err = msg.Ret.Send(message)
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
			message := &Message{Stream: tmp}
			encodeErr := message.Encode([]byte("path=" + tmp.Name()))
			if encodeErr != nil {
				t.Fatal(encodeErr)
			}
			_, err := w.Send(message)
			if err != nil {
				t.Fatal(err)
			}
		}()
		msg, err := r.Receive(0)
		if err != nil {
			t.Fatal(err)
		}
		var data []byte
		decodeErr := msg.Decode(&data)
		if decodeErr != nil {
			t.Fatal(decodeErr)
		}
		if string(data) != "path="+tmp.Name() {
			t.Fatalf("%#v", msg)
		}
		txt, err := ioutil.ReadAll(msg.Stream)
		if err != nil {
			t.Fatal(err)
		}
		if string(txt) != "hello world\n" {
			t.Fatalf("%s\n", txt)
		}
	})
}
