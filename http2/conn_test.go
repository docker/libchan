package http2

import (
	"bytes"
	"io"
	"net"
	"testing"

	"github.com/docker/libchan"
)

type MultiMessage struct {
	Message string
	Both    io.ReadWriteCloser
	Stdin   io.WriteCloser
	Stdout  io.ReadCloser
}

type RecvMultiMessage struct {
	Message string
	Both    io.ReadWriteCloser
	Stdin   io.ReadCloser
	Stdout  io.WriteCloser
}

func TestMultiTcpByteStream(t *testing.T) {
	wait := make(chan bool)
	client := func(t *testing.T, sender libchan.ChannelSender, s *Session) {
		<-wait
		both, connErr := net.Dial("tcp", "localhost:9272")
		if connErr != nil {
			t.Fatalf("Error creating connection: %s", connErr)
		}

		in, connErr := net.Dial("tcp", "localhost:9272")
		if connErr != nil {
			t.Fatalf("Error creating connection: %s", connErr)
		}

		out, connErr := net.Dial("tcp", "localhost:9272")
		if connErr != nil {
			t.Fatalf("Error creating connection: %s", connErr)
		}

		m1 := &MultiMessage{
			Message: "Tcp Stream!",
			Both:    both,
			Stdin:   out,
			Stdout:  in,
		}

		sendErr := sender.Send(m1)
		if sendErr != nil {
			t.Fatalf("Error sending channel: %s", sendErr)
		}

		buf := make([]byte, 5)
		n, readErr := in.Read(buf)
		if readErr != nil {
			t.Fatalf("Error reading from in stream: %s", readErr)
		}
		if expected := 5; n != expected {
			t.Fatalf("Unexpected number of bytes read\n\tExpected: %d\n\tActual: %d", expected, n)
		}
		if expected := []byte{0x03, 0x05, 0x07, 0x0b, 0x0d}; bytes.Compare(buf, expected) != 0 {
			t.Fatalf("Unexpected byte values\n\tExpected: %x\n\tActual: %x", expected, buf)
		}

		n, readErr = both.Read(buf)
		if readErr != nil {
			t.Fatalf("Error reading from in/out stream: %s", readErr)
		}
		if expected := 5; n != expected {
			t.Fatalf("Unexpected number of bytes read\n\tExpected: %d\n\tActual: %d", expected, n)
		}
		if expected := []byte{0x13, 0x15, 0x17, 0x1b, 0x1d}; bytes.Compare(buf, expected) != 0 {
			t.Fatalf("Unexpected byte values\n\tExpected: %x\n\tActual: %x", expected, buf)
		}

		n, writeErr := both.Write([]byte{0x23, 0x25, 0x27, 0x2b, 0x2d})
		if writeErr != nil {
			t.Fatalf("Error writing from in/out stream: %s", writeErr)
		}
		if expected := 5; n != expected {
			t.Fatalf("Unexpected number of bytes written\n\tExpected: %d\n\tActual: %d", expected, n)
		}

		n, writeErr = out.Write([]byte{0x33, 0x35, 0x37, 0x3b, 0x3d})
		if writeErr != nil {
			t.Fatalf("Error writing from out stream: %s", writeErr)
		}
		if expected := 5; n != expected {
			t.Fatalf("Unexpected number of bytes written\n\tExpected: %d\n\tActual: %d", expected, n)
		}

		both.Close()
		in.Close()
		out.Close()
	}
	server := func(t *testing.T, receiver libchan.ChannelReceiver, s *Session) {
		listener, listenerErr := net.Listen("tcp", "localhost:9272")
		if listenerErr != nil {
			t.Fatalf("Error creating byte stream listener: %s", listenerErr)
		}
		s.RegisterListener(listener)
		close(wait)

		m1 := &RecvMultiMessage{}
		recvErr := receiver.Receive(m1)
		if recvErr != nil {
			t.Fatalf("Error receiving message: %s", recvErr)
		}

		if expected := "Tcp Stream!"; m1.Message != expected {
			t.Fatalf("Unexpected message value:\n\tExpected: %s\n\tActual: %s", expected, m1.Message)
		}

		n, writeErr := m1.Stdout.Write([]byte{0x03, 0x05, 0x07, 0x0b, 0x0d})
		if writeErr != nil {
			t.Fatalf("Error writing from in/out stream: %s", writeErr)
		}
		if expected := 5; n != expected {
			t.Fatalf("Unexpected number of bytes written\n\tExpected: %d\n\tActual: %d", expected, n)
		}

		n, writeErr = m1.Both.Write([]byte{0x13, 0x15, 0x17, 0x1b, 0x1d})
		if writeErr != nil {
			t.Fatalf("Error writing from out stream: %s", writeErr)
		}
		if expected := 5; n != expected {
			t.Fatalf("Unexpected number of bytes written\n\tExpected: %d\n\tActual: %d", expected, n)
		}

		buf := make([]byte, 5)
		n, readErr := m1.Both.Read(buf)
		if readErr != nil {
			t.Fatalf("Error reading from in stream: %s", readErr)
		}
		if expected := 5; n != expected {
			t.Fatalf("Unexpected number of bytes read\n\tExpected: %d\n\tActual: %d", expected, n)
		}
		if expected := []byte{0x23, 0x25, 0x27, 0x2b, 0x2d}; bytes.Compare(buf, expected) != 0 {
			t.Fatalf("Unexpected byte values\n\tExpected: %x\n\tActual: %x", expected, buf)
		}

		n, readErr = m1.Stdin.Read(buf)
		if readErr != nil {
			t.Fatalf("Error reading from in/out stream: %s", readErr)
		}
		if expected := 5; n != expected {
			t.Fatalf("Unexpected number of bytes read\n\tExpected: %d\n\tActual: %d", expected, n)
		}
		if expected := []byte{0x33, 0x35, 0x37, 0x3b, 0x3d}; bytes.Compare(buf, expected) != 0 {
			t.Fatalf("Unexpected byte values\n\tExpected: %x\n\tActual: %x", expected, buf)
		}

		listener.Close()
	}
	SpawnClientServerTest(t, "localhost:12947", ClientSendWrapper(client), ServerReceiveWrapper(server))
}
