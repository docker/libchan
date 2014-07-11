package libchan

import (
	"fmt"
	"io"
	"io/ioutil"
	"os"
	"runtime/pprof"
	"testing"
	"time"

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

type ComplexMessage struct {
	Message  string
	Sender   Channel
	Receiver Channel
	Stream   io.ReadWriteCloser
}

type SimpleMessage struct {
	Message string
}

func TestComplexMessage(t *testing.T) {
	sender := func(t *testing.T, c Channel) {
		s, sErr := c.CreateSubChannel(Out)
		if sErr != nil {
			t.Fatalf("Error creating sender: %s", sErr)
		}
		r, rErr := c.CreateSubChannel(In)
		if rErr != nil {
			t.Fatalf("Error creating receiver: %s", rErr)
		}
		bs, bsErr := c.CreateByteStream()
		if bsErr != nil {
			t.Fatalf("Error creating bytestream: %s", bsErr)
		}

		m1 := &ComplexMessage{
			Message:  "This is a complex message",
			Sender:   r,
			Receiver: s,
			Stream:   bs,
		}

		sendErr := c.Communicate(m1)
		if sendErr != nil {
			t.Fatalf("Error sending: %s", sendErr)
		}

		m2 := &SimpleMessage{}
		receiveErr := m1.Sender.Communicate(m2)
		if receiveErr != nil {
			t.Fatalf("Error receiving from message: %s", receiveErr)
		}

		if expected := "Return to sender"; expected != m2.Message {
			t.Fatalf("Unexpected message\n\tExpected: %s\n\tActual: %s", expected, m2.Message)
		}

		m3 := &SimpleMessage{"Receive returned"}
		sendErr = m1.Receiver.Communicate(m3)
		if sendErr != nil {
			t.Fatalf("Error sending return: %s", sendErr)
		}

		_, writeErr := m1.Stream.Write([]byte("Hello there server!"))
		if writeErr != nil {
			t.Fatalf("Error writing to byte stream: %s", writeErr)
		}

		readBytes := make([]byte, 30)
		n, readErr := m1.Stream.Read(readBytes)
		if readErr != nil {
			t.Fatalf("Error reading from byte stream: %s", readErr)
		}
		if expected := "G'day client ☺"; string(readBytes[:n]) != expected {
			t.Fatalf("Unexpected read value:\n\tExpected: %q\n\tActual: %q", expected, string(readBytes[:n]))
		}

		closeErr := m1.Stream.Close()
		if closeErr != nil {
			t.Fatalf("Error closing byte stream: %s", closeErr)
		}
	}
	receiver := func(t *testing.T, c Channel) {
		m1 := &ComplexMessage{}
		receiveErr := c.Communicate(m1)
		if receiveErr != nil {
			t.Fatalf("Error receiving: %s", receiveErr)
		}

		if expected := "This is a complex message"; m1.Message != expected {
			t.Fatalf("Unexpected message\n\tExpected: %s\n\tActual: %s", expected, m1.Message)
		}

		if m1.Sender.Direction() != Out {
			t.Fatalf("Wrong direction for sender")
		}

		m2 := &SimpleMessage{"Return to sender"}
		sendErr := m1.Sender.Communicate(m2)
		if sendErr != nil {
			t.Fatalf("Error sending return: %s", sendErr)
		}

		m3 := &SimpleMessage{}
		receiveErr = m1.Receiver.Communicate(m3)
		if receiveErr != nil {
			t.Fatalf("Error receiving from message: %s", receiveErr)
		}

		if expected := "Receive returned"; expected != m3.Message {
			t.Fatalf("Unexpected message\n\tExpected: %s\n\tActual: %s", expected, m3.Message)
		}

		readBytes := make([]byte, 30)
		n, readErr := m1.Stream.Read(readBytes)
		if readErr != nil {
			t.Fatalf("Error reading from byte stream: %s", readErr)
		}
		if expected := "Hello there server!"; string(readBytes[:n]) != expected {
			t.Fatalf("Unexpected read value:\n\tExpected: %q\n\tActual: %q", expected, string(readBytes[:n]))
		}

		_, writeErr := m1.Stream.Write([]byte("G'day client ☺"))
		if writeErr != nil {
			t.Fatalf("Error writing to byte stream: %s", writeErr)
		}

		closeErr := m1.Stream.Close()
		if closeErr != nil {
			t.Fatalf("Error closing byte stream: %s", closeErr)
		}

	}
	SpawnChannelTestRoutines(t, sender, receiver)
}

type TestRoutine func(t *testing.T, c Channel)

var RoutineTimeout = 300 * time.Millisecond
var DumpStackOnTimeout = true

func SpawnChannelTestRoutines(t *testing.T, r1, r2 TestRoutine) {
	end1 := make(chan bool)
	end2 := make(chan bool)
	session := NewInMemSession()

	go func() {
		defer close(end1)
		s := session.NewSendChannel()
		defer s.Close()
		r1(t, s)
	}()

	go func() {
		defer close(end2)
		r := session.WaitReceiveChannel()
		defer r.Close()
		r2(t, r)
	}()

	timeout := time.After(RoutineTimeout)

	for end1 != nil || end2 != nil {
		select {
		case <-end1:
			if t.Failed() {
				t.Fatal("Test routine 1 failed")
			}
			end1 = nil
		case <-end2:
			if t.Failed() {
				t.Fatal("Test routine 2 failed")
			}
			end2 = nil
		case <-timeout:
			if DumpStackOnTimeout {
				pprof.Lookup("goroutine").WriteTo(os.Stdout, 1)
			}
			t.Fatal("Timeout")
		}
	}

}
