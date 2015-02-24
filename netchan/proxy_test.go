package netchan

import (
	"io"
	"net"
	"os"
	"runtime/pprof"
	"testing"
	"time"

	"github.com/docker/libchan"
)

type ProxiedMessage struct {
	Message string
	Ret     libchan.Sender
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
	client := func(t *testing.T, sender libchan.Sender) {
		for i, m := range messages {
			nestedReceiver, remoteSender := libchan.Pipe()

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

			if ack.N != (i + 1) {
				t.Fatalf("Unexpected ack value\n\tExpected: %d\n\tActual: %d", (i + 1), ack.N)
			}

			if ack.MessageLen != len(m) {
				t.Fatalf("Unexpected ack value\n\tExpected: %d\n\tActual: %d", len(m), ack.MessageLen)
			}
		}

	}
	server := func(t *testing.T, receiver libchan.Receiver) {
		for i, m := range messages {
			message := &ProxiedMessage{}
			err := receiver.Receive(message)
			if err != nil {
				t.Fatalf("Error receiving message: %s", err)
			}

			if message.Message != m {
				t.Fatalf("Unexpected message:\n\tExpected: %s\n\tActual: %s", m, message.Message)
			}

			ack := &ProxyAckMessage{N: i + 1, MessageLen: len(message.Message)}
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
	client := func(t *testing.T, sender libchan.Sender) {
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
	server := func(t *testing.T, receiver libchan.Receiver) {
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

func SpawnProxyTest(t *testing.T, client PipeSenderRoutine, server PipeReceiverRoutine, proxyCount int) {
	endClient := make(chan bool)
	endServer := make(chan bool)
	endProxy := make(chan bool)

	receiver1, sender1, err := testPipe()
	receiver2, sender2, err := testPipe()

	if err != nil {
		t.Fatalf("Error creating pipe: %s", err)
	}

	go func() {
		defer close(endProxy)
		n, err := libchan.Copy(sender2, receiver1)
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

	timeout := time.After(ClientServerTimeout)

	for endClient != nil || endServer != nil || endProxy != nil {
		select {
		case <-endProxy:
			if t.Failed() {
				t.Fatal("Proxy failed")
			}
			endProxy = nil
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
