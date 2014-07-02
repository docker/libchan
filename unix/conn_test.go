package unix

import (
	"testing"

	lch "github.com/docker/libchan"
	"github.com/dotcloud/docker/pkg/testutils"
)

type Command struct {
	Name    string
	Content []byte
}

func TestPair(t *testing.T) {
	r, w, err := Pair()
	if err != nil {
		t.Fatal("Unexpected error")
	}
	defer r.Close()
	defer w.Close()
	testutils.Timeout(t, func() {
		endChan := make(chan bool)
		go func() {
			msg, err := r.Receive(0)
			if err != nil {
				t.Fatal(err)
			}
			var command Command
			msg.Decode(&command)
			if string(command.Content) != "hello world" {
				t.Fatalf("%#v", *msg)
			}
			if command.Name != "Hello" {
				t.Fatalf("%#v", *msg)
			}
			close(endChan)
		}()
		message := &lch.Message{}
		message.Encode(&Command{Name: "Hello", Content: []byte("hello world")})
		_, err := w.Send(message)
		if err != nil {
			t.Fatal(err)
		}
		<-endChan
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
		endChan := make(chan bool)
		go func() {
			// Send a message with mode=R
			message := &lch.Message{Ret: lch.RetPipe}
			encodeErr := message.Encode([]byte("this is the request"))
			if encodeErr != nil {
				t.Fatal(encodeErr)
			}
			ret, err := w.Send(message)
			if err != nil {
				t.Fatal(err)
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
			close(endChan)
		}()
		// Receive a message with mode=W
		msg, err := r.Receive(lch.Ret)
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
		// Send a reply
		message := &lch.Message{}
		encodeErr := message.Encode([]byte("this is the reply"))
		if encodeErr != nil {
			t.Fatal(encodeErr)
		}
		_, err = msg.Ret.Send(message)
		if err != nil {
			t.Fatal(err)
		}
		<-endChan
	})
}
