package libchan

import (
	"io"
	"net"
	"os"
	"reflect"
	"runtime/pprof"
	"testing"
	"time"
)

type ProxiedMessage struct {
	Message string
	Ret     Sender
}

type ProxyAckMessage struct {
	N          int
	MessageLen int
}

func TestChannelProxy(t *testing.T) {
	messages := []string{
		"Proxied messages",
		"Another proxied message",
		"Far less interesting message",
		"This was ALSO sent over the proxy",
	}
	client := func(t *testing.T, sender Sender) {
		for i, m := range messages {
			nestedReceiver, remoteSender := Pipe()

			message := &ProxiedMessage{
				Message: m,
				Ret:     remoteSender,
			}

			err := sender.Send(message)
			if err != nil {
				t.Fatalf("Error sending message: %s", err)
			}

			ack := &ProxyAckMessage{}
			err = nestedReceiver.Receive(ack)
			if err != nil {
				t.Fatalf("Error receiving ack: %s", err)
			}

			if ack.N != i {
				t.Fatalf("Unexpected ack value\n\tExpected: %d\n\tActual: %d", i, ack.N)
			}

			if ack.MessageLen != len(m) {
				t.Fatalf("Unexpected ack value\n\tExpected: %d\n\tActual: %d", len(m), ack.MessageLen)
			}
		}

	}
	server := func(t *testing.T, receiver Receiver) {
		for i, m := range messages {
			message := &ProxiedMessage{}
			err := receiver.Receive(message)
			if err != nil {
				t.Fatalf("Error receiving message: %s", err)
			}

			if message.Message != m {
				t.Fatalf("Unexpected message:\n\tExpected: %s\n\tActual: %s", m, message.Message)
			}

			ack := &ProxyAckMessage{N: i, MessageLen: len(message.Message)}
			err = message.Ret.Send(ack)
			if err != nil {
				t.Fatalf("Error sending ack: %s", err)
			}
		}
	}
	SpawnProxyTest(t, client, server, 4)
}

type ProxiedStreamMessage struct {
	Stream io.ReadWriteCloser
}

func TestByteStreamProxy(t *testing.T) {
	sendString := "Sending a string"
	retString := "Returned string"
	client := func(t *testing.T, sender Sender) {
		bs, bsRemote := net.Pipe()

		message := &ProxiedStreamMessage{
			Stream: bsRemote,
		}

		err := sender.Send(message)
		if err != nil {
			t.Fatalf("Error sending message: %s", err)
		}

		_, err = bs.Write([]byte(sendString))
		if err != nil {
			t.Fatalf("Error writing bytes: %s", err)
		}

		buf := make([]byte, 30)
		n, err := bs.Read(buf)
		if string(buf[:n]) != retString {
			t.Fatalf("Unexpected string value:\n\tExpected: %s\n\tActual: %s", retString, string(buf[:n]))
		}
	}
	server := func(t *testing.T, receiver Receiver) {
		message := &ProxiedStreamMessage{}
		err := receiver.Receive(message)
		if err != nil {
			t.Fatalf("Error receiving message: %s", err)
		}

		buf := make([]byte, 30)
		n, err := message.Stream.Read(buf)
		if string(buf[:n]) != sendString {
			t.Fatalf("Unexpected string value:\n\tExpected: %s\n\tActual: %s", sendString, string(buf[:n]))
		}

		_, err = message.Stream.Write([]byte(retString))
		if err != nil {
			t.Fatalf("Error writing bytes: %s", err)
		}
	}
	SpawnProxyTest(t, client, server, 1)
}

func TestTypeTransmission(t *testing.T) {
	// TODO(stevvooe): Ensure that libchan transports can all have this same
	// test run against it.

	// Add problem types to this type definition. For now, we just care about
	// time.Time.
	type A struct {
		T time.Time
	}

	expected := A{T: time.Now()}

	receiver, sender := Pipe()

	go func() {
		if err := sender.Send(expected); err != nil {
			t.Fatalf("unexpected error sending: %v", err)
		}
	}()

	var received A
	if err := receiver.Receive(&received); err != nil {
		t.Fatalf("unexpected error receiving: %v", err)
	}

	if !reflect.DeepEqual(received, expected) {
		t.Fatalf("expected structs to be equal: %#v != %#v", received, expected)
	}
}

func SpawnProxyTest(t *testing.T, client SendTestRoutine, server ReceiveTestRoutine, proxyCount int) {
	endClient := make(chan bool)
	endServer := make(chan bool)
	endProxy := make(chan bool)

	receiver1, sender1 := Pipe()
	receiver2, sender2 := Pipe()

	go func() {
		defer close(endProxy)
		n, err := Copy(sender2, receiver1)
		if err != nil {
			t.Errorf("Error proxying: %s", err)
		}
		err = sender2.Close()
		if err != nil {
			t.Errorf("Error closing sender: %s", err)
		}
		if n != proxyCount {
			t.Errorf("Wrong proxy count\n\tExpected: %d\n\tActual: %d", proxyCount, n)
		}
	}()

	go func() {
		defer close(endClient)
		client(t, sender1)
		err := sender1.Close()
		if err != nil {
			t.Errorf("Error closing sender: %s", err)
		}
	}()

	go func() {
		defer close(endServer)
		server(t, receiver2)
	}()

	timeout := time.After(RoutineTimeout)

	for endClient != nil || endServer != nil {
		select {
		case <-endProxy:
			if t.Failed() {
				t.Fatal("Proxy failed")
			}
			endClient = nil
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
