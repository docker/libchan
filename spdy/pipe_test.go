package spdy

import (
	"io"
	"net"
	"os"
	"runtime/pprof"
	"testing"
	"time"

	"github.com/docker/libchan"
	"github.com/docker/libchan/encoding/msgpack"
)

// testPipe creates a top-level channel pipe using an in memory
// transport using spdy and msgpack
func testPipe() (libchan.Receiver, libchan.Sender, error) {
	c1, c2 := net.Pipe()

	s1, err := NewSpdyStreamProvider(c1, false)
	if err != nil {
		return nil, nil, err
	}
	t1 := NewTransport(s1, &msgpack.Codec{})

	s2, err := NewSpdyStreamProvider(c2, true)
	if err != nil {
		return nil, nil, err
	}
	t2 := NewTransport(s2, &msgpack.Codec{})

	var recv libchan.Receiver
	waitError := make(chan error)

	go func() {
		var err error
		recv, err = t2.WaitReceiveChannel()
		waitError <- err
	}()

	send, senderErr := t1.NewSendChannel()
	if senderErr != nil {
		c1.Close()
		c2.Close()
		return nil, nil, senderErr
	}

	receiveErr := <-waitError
	if receiveErr != nil {
		c1.Close()
		c2.Close()
		return nil, nil, receiveErr
	}
	return recv, send, nil
}

type PipeMessage struct {
	Message string
	Stream  io.ReadWriteCloser
	Send    libchan.Sender
}

func TestSendFirstPipe(t *testing.T) {
	message1 := "Pipe messages"
	message2 := "Must more simple message"
	message3 := "This was sent over a byte stream"
	message4 := "This was ALSO sent over a byte stream"
	client := func(t *testing.T, sender libchan.Sender) {
		bs, bsRemote := net.Pipe()

		nestedReceiver, remoteSender := libchan.Pipe()

		m1 := &PipeMessage{
			Message: message1,
			Stream:  bsRemote,
			Send:    remoteSender,
		}
		sendErr := sender.Send(m1)
		if sendErr != nil {
			t.Fatalf("Error sending message: %s", sendErr)
		}

		m2 := &SimpleMessage{}
		recvErr := nestedReceiver.Receive(m2)
		if recvErr != nil {
			t.Fatalf("Error receiving from nested receiver: %s", recvErr)
		}

		if m2.Message != message2 {
			t.Fatalf("Unexpected message\n\tExpected: %s\n\tActual: %s", message2, m2.Message)
		}

		_, writeErr := bs.Write([]byte(message3))
		if writeErr != nil {
			t.Fatalf("Error writing on byte stream: %s", writeErr)
		}

		readData := make([]byte, len([]byte(message4)))
		_, readErr := bs.Read(readData)
		if readErr != nil {
			t.Fatalf("Error reading on byte stream: %s", readErr)
		}
		if string(readData) != message4 {
			t.Fatalf("Unexpected message\n\tExpected: %s\n\tActual: %s", message4, string(readData))
		}
	}
	server := func(t *testing.T, receiver libchan.Receiver) {
		m1 := &PipeMessage{}
		recvErr := receiver.Receive(m1)
		if recvErr != nil {
			t.Fatalf("Error receiving message: %s", recvErr)
		}

		if m1.Message != message1 {
			t.Fatalf("Unexpected message\n\tExpected: %s\n\tActual: %s", message1, m1.Message)
		}

		sendErr := m1.Send.Send(&SimpleMessage{message2})
		if sendErr != nil {
			t.Fatalf("Error creating sender: %s", sendErr)
		}

		readData := make([]byte, len([]byte(message3)))
		_, readErr := m1.Stream.Read(readData)
		if readErr != nil {
			t.Fatalf("Error reading on byte stream: %s", readErr)
		}
		if string(readData) != message3 {
			t.Fatalf("Unexpected message\n\tExpected: %s\n\tActual: %s", message3, string(readData))
		}

		_, writeErr := m1.Stream.Write([]byte(message4))
		if writeErr != nil {
			t.Fatalf("Error writing on byte stream: %s", writeErr)
		}

		closeErr := m1.Send.Close()
		if closeErr != nil {
			t.Fatalf("Error closing received sender: %s", closeErr)
		}

	}
	SpawnPipeTest(t, client, server)
}

type PipeSenderRoutine func(*testing.T, libchan.Sender)
type PipeReceiverRoutine func(*testing.T, libchan.Receiver)

func SpawnPipeTest(t *testing.T, client PipeSenderRoutine, server PipeReceiverRoutine) {
	endClient := make(chan bool)
	endServer := make(chan bool)

	receiver, sender, err := testPipe()
	if err != nil {
		t.Fatalf("Error creating pipe: %s", err)
	}

	go func() {
		defer close(endClient)
		client(t, sender)
		err := sender.Close()
		if err != nil {
			t.Fatalf("Error closing sender: %s", err)
		}
	}()

	go func() {
		defer close(endServer)
		server(t, receiver)
	}()

	timeout := time.After(ClientServerTimeout)

	for endClient != nil || endServer != nil {
		select {
		case <-endClient:
			if t.Failed() {
				t.Fatal("Client failed")
			}
			endClient = nil
		case <-endServer:
			if t.Failed() {
				t.Fatal("Server failed")
			}
			endServer = nil
		case <-timeout:
			if DumpStackOnTimeout {
				pprof.Lookup("goroutine").WriteTo(os.Stdout, 1)
			}
			t.Fatal("Timeout")
		}
	}
}
