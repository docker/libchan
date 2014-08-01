package libchan

import (
	"fmt"
	"io"
	"io/ioutil"
	"net"
	"os"
	"runtime/pprof"
	"testing"
	"time"
)

type InMemMessage struct {
	Data   string
	Stream io.ReadWriteCloser
	Ret    Sender
}

func TestInmemRetPipe(t *testing.T) {
	client := func(t *testing.T, w Sender) {
		ret, retPipe := Pipe()
		message := &InMemMessage{Data: "hello", Ret: retPipe}

		err := w.Send(message)
		if err != nil {
			t.Fatal(err)
		}
		msg := &InMemMessage{}
		err = ret.Receive(msg)
		if err != nil {
			t.Fatal(err)
		}

		if msg.Data != "this better not crash" {
			t.Fatalf("%#v", msg)
		}

	}
	server := func(t *testing.T, r Receiver) {
		msg := &InMemMessage{}
		err := r.Receive(msg)
		if err != nil {
			t.Fatal(err)
		}

		if msg.Data != "hello" {
			t.Fatalf("Wrong message:\n\tExpected: %s\n\tActual: %s", "hello", msg.Data)
		}
		if msg.Ret == nil {
			t.Fatal("Message Ret is nil")
		}

		message := &InMemMessage{Data: "this better not crash"}
		if err := msg.Ret.Send(message); err != nil {
			t.Fatal(err)
		}
	}
	SpawnPipeTestRoutines(t, client, server)

}

func TestSimpleSend(t *testing.T) {
	client := func(t *testing.T, w Sender) {
		message := &InMemMessage{Data: "hello world"}
		if err := w.Send(message); err != nil {
			t.Fatal(err)
		}
	}
	server := func(t *testing.T, r Receiver) {
		msg := &InMemMessage{Data: "hello world"}
		err := r.Receive(msg)
		if err != nil {
			t.Fatal(err)
		}
		if msg.Data != "hello world" {
			t.Fatalf("%#v", *msg)
		}
	}
	SpawnPipeTestRoutines(t, client, server)
}

func TestSendReply(t *testing.T) {
	client := func(t *testing.T, w Sender) {
		ret, retPipe := Pipe()
		message := &InMemMessage{Data: "this is the request", Ret: retPipe}
		err := w.Send(message)
		if err != nil {
			t.Fatal(err)
		}

		// Read for a reply
		msg := &InMemMessage{}
		err = ret.Receive(msg)
		if err != nil {
			t.Fatal(err)
		}
		if msg.Data != "this is the reply" {
			t.Fatalf("%#v", msg)
		}
	}
	server := func(t *testing.T, r Receiver) {
		// Receive a message with mode=Ret
		msg := &InMemMessage{}
		err := r.Receive(msg)
		if err != nil {
			t.Fatal(err)
		}

		if msg.Data != "this is the request" {
			t.Fatalf("%#v", msg)
		}
		if msg.Ret == nil {
			t.Fatalf("%#v", msg)
		}
		// Send a reply
		message := &InMemMessage{Data: "this is the reply"}
		err = msg.Ret.Send(message)
		if err != nil {
			t.Fatal(err)
		}

	}
	SpawnPipeTestRoutines(t, client, server)
}

func TestSendFile(t *testing.T) {
	tmp, err := ioutil.TempFile("", "libchan-test-")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(tmp.Name())
	fmt.Fprintf(tmp, "hello world\n")
	tmp.Sync()
	tmp.Seek(0, 0)

	client := func(t *testing.T, w Sender) {
		message := &InMemMessage{Data: "path=" + tmp.Name(), Stream: tmp}
		err = w.Send(message)
		if err != nil {
			t.Fatal(err)
		}
	}
	server := func(t *testing.T, r Receiver) {
		msg := &InMemMessage{}
		err := r.Receive(msg)
		if err != nil {
			t.Fatal(err)
		}
		if msg.Data != "path="+tmp.Name() {
			t.Fatalf("%#v", msg)
		}
		txt, err := ioutil.ReadAll(msg.Stream)
		if err != nil {
			t.Fatal(err)
		}
		if string(txt) != "hello world\n" {
			t.Fatalf("%s\n", txt)
		}

	}
	SpawnPipeTestRoutines(t, client, server)
}

type ComplexMessage struct {
	Message  string
	Sender   Sender
	Receiver Receiver
	Stream   io.ReadWriteCloser
}

type SimpleMessage struct {
	Message string
}

func TestComplexMessage(t *testing.T) {
	client := func(t *testing.T, w Sender) {
		remoteRecv, send := Pipe()
		recv, remoteSend := Pipe()
		bs, bsRemote := net.Pipe()

		m1 := &ComplexMessage{
			Message:  "This is a complex message",
			Sender:   remoteSend,
			Receiver: remoteRecv,
			Stream:   bsRemote,
		}

		sendErr := w.Send(m1)
		if sendErr != nil {
			t.Fatalf("Error sending: %s", sendErr)
		}

		m2 := &SimpleMessage{}
		receiveErr := recv.Receive(m2)
		if receiveErr != nil {
			t.Fatalf("Error receiving from message: %s", receiveErr)
		}

		if expected := "Return to sender"; expected != m2.Message {
			t.Fatalf("Unexpected message\n\tExpected: %s\n\tActual: %s", expected, m2.Message)
		}

		m3 := &SimpleMessage{"Receive returned"}
		sendErr = send.Send(m3)
		if sendErr != nil {
			t.Fatalf("Error sending return: %s", sendErr)
		}

		_, writeErr := bs.Write([]byte("Hello there server!"))
		if writeErr != nil {
			t.Fatalf("Error writing to byte stream: %s", writeErr)
		}

		readBytes := make([]byte, 30)
		n, readErr := bs.Read(readBytes)
		if readErr != nil {
			t.Fatalf("Error reading from byte stream: %s", readErr)
		}
		if expected := "G'day client ☺"; string(readBytes[:n]) != expected {
			t.Fatalf("Unexpected read value:\n\tExpected: %q\n\tActual: %q", expected, string(readBytes[:n]))
		}

		closeErr := bs.Close()
		if closeErr != nil {
			t.Fatalf("Error closing byte stream: %s", closeErr)
		}
	}
	server := func(t *testing.T, r Receiver) {
		m1 := &ComplexMessage{}
		receiveErr := r.Receive(m1)
		if receiveErr != nil {
			t.Fatalf("Error receiving: %s", receiveErr)
		}

		if expected := "This is a complex message"; m1.Message != expected {
			t.Fatalf("Unexpected message\n\tExpected: %s\n\tActual: %s", expected, m1.Message)
		}

		m2 := &SimpleMessage{"Return to sender"}
		sendErr := m1.Sender.Send(m2)
		if sendErr != nil {
			t.Fatalf("Error sending return: %s", sendErr)
		}

		m3 := &SimpleMessage{}
		receiveErr = m1.Receiver.Receive(m3)
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
	SpawnPipeTestRoutines(t, client, server)
}

func TestInmemWrappedSend(t *testing.T) {
	tmp, err := ioutil.TempFile("", "libchan-test-")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(tmp.Name())
	fmt.Fprintf(tmp, "hello through a wrapper\n")
	tmp.Sync()
	tmp.Seek(0, 0)

	client := func(t *testing.T, w Sender) {
		message := &InMemMessage{Data: "path=" + tmp.Name(), Stream: tmp}
		err = w.Send(message)
		if err != nil {
			t.Fatal(err)
		}
	}
	server := func(t *testing.T, r Receiver) {
		msg := &InMemMessage{}
		err := r.Receive(msg)
		if err != nil {
			t.Fatal(err)
		}
		if msg.Data != "path="+tmp.Name() {
			t.Fatalf("%#v", msg)
		}
		txt, err := ioutil.ReadAll(msg.Stream)
		if err != nil {
			t.Fatal(err)
		}
		if string(txt) != "hello through a wrapper\n" {
			t.Fatalf("%s\n", txt)
		}

	}
	SpawnPipeTestRoutines(t, client, server)
}

type SendTestRoutine func(*testing.T, Sender)
type ReceiveTestRoutine func(*testing.T, Receiver)

var RoutineTimeout = 300 * time.Millisecond
var DumpStackOnTimeout = true

func SpawnPipeTestRoutines(t *testing.T, s SendTestRoutine, r ReceiveTestRoutine) {
	end1 := make(chan bool)
	end2 := make(chan bool)

	receiver, sender := Pipe()

	go func() {
		defer close(end1)
		s(t, sender)
		err := sender.Close()
		if err != nil {
			t.Fatalf("Error closing sender: %s", err)
		}
	}()

	go func() {
		defer close(end2)
		r(t, receiver)
	}()

	timeout := time.After(RoutineTimeout)

	for end1 != nil || end2 != nil {
		select {
		case <-end1:
			if t.Failed() {
				t.Fatal("Send routine failed")
			}
			end1 = nil
		case <-end2:
			if t.Failed() {
				t.Fatal("Receive routine failed")
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
