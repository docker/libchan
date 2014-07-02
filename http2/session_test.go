package http2

import (
	"io"
	"net"
	"os"
	"runtime/pprof"
	"testing"
	"time"
)

type InOutMessage struct {
	Message string
	Recv    *ReceiveChannel
	Send    *SendChannel
}

type SimpleMessage struct {
	Message string
}

func TestChannelEncoding(t *testing.T) {
	client := func(t *testing.T, server string) {
		conn, connErr := net.Dial("tcp", server)
		if connErr != nil {
			t.Fatalf("Error dialing server: %s", connErr)
		}

		session, sessionErr := newSession(conn, false)
		if sessionErr != nil {
			t.Fatalf("Error creating session: %s", sessionErr)
		}

		sender, senderErr := session.NewSendChannel()
		if senderErr != nil {
			t.Fatalf("Error creating sender: %s", senderErr)
		}

		s1, r1, err1 := sender.NewSubChannel(In)
		if err1 != nil {
			t.Fatalf("Error creating receive channel: %s", err1)
		}

		s2, r2, err2 := sender.NewSubChannel(Out)
		if err2 != nil {
			t.Fatalf("Error creating send channel: %s", err2)
		}

		m1 := &InOutMessage{
			Message: "WithInOut",
			Recv:    r1,
			Send:    s2,
		}
		sendErr := sender.Send(m1)
		if sendErr != nil {
			t.Fatalf("Error sending InOutMessage: %s", sendErr)
		}

		m2 := &SimpleMessage{"This is a simple message"}
		sendErr = s1.Send(m2)
		if sendErr != nil {
			t.Fatalf("Error sending simple message: %s", sendErr)
		}

		m3 := &SimpleMessage{}
		recvErr := r2.Receive(m3)
		if recvErr != nil {
			t.Fatalf("Error receiving simple message: %s", recvErr)
		}

		closeErr := sender.Close()
		if closeErr != nil {
			t.Fatalf("Error closing sender: %s", closeErr)
		}

		closeErr = s1.Close()
		if closeErr != nil {
			t.Fatalf("Error closing s1: %s", closeErr)
		}

		recvErr = r2.Receive(nil)
		if recvErr != io.EOF {
			t.Fatalf("Expected receive EOF, received %s", recvErr)
		}

		closeErr = session.Close()
		if closeErr != nil {
			t.Fatalf("Error closing connection: %s", closeErr)
		}
	}
	server := func(t *testing.T, listener net.Listener) {
		conn, connErr := listener.Accept()
		if connErr != nil {
			t.Fatalf("Error accepting connection: %s", connErr)
		}

		session, sessionErr := newSession(conn, true)
		if sessionErr != nil {
			t.Fatalf("Error creating session: %s", sessionErr)
		}

		receiver, receiverErr := session.WaitReceiveChannel()
		if receiverErr != nil {
			t.Fatalf("Error waiting for receiver: %s", receiverErr)
		}

		m1 := &InOutMessage{}
		receiveErr := receiver.Receive(m1)
		if receiveErr != nil {
			t.Fatalf("Error receiving InOutMessage: %s", receiveErr)
		}

		expectedMessage := "WithInOut"
		if m1.Message != expectedMessage {
			t.Fatalf("Unexpected message\nExpected: %s\nActual: %s", expectedMessage, m1.Message)
		}

		if m1.Recv == nil {
			t.Fatalf("Recv is nil")
		}

		if m1.Send == nil {
			t.Fatalf("Send is nil")
		}

		m2 := &SimpleMessage{}
		receiveErr = m1.Recv.Receive(m2)
		if receiveErr != nil {
			t.Fatalf("Error receiving SimpleMessage: %s", receiveErr)
		}

		m3 := &SimpleMessage{"This is a responding simple message"}
		sendErr := m1.Send.Send(m3)
		if sendErr != nil {
			t.Fatalf("Error sending SimpleMessage: %s", sendErr)
		}

		receiveErr = receiver.Receive(nil)
		if receiveErr != io.EOF {
			t.Fatalf("Expected receive EOF, received %s", receiveErr)
		}

		receiveErr = m1.Recv.Receive(nil)
		if receiveErr != io.EOF {
			t.Fatalf("Expected receive EOF, received %s", receiveErr)
		}

		closeErr := m1.Send.Close()
		if closeErr != nil {
			t.Fatalf("Error closing send connection: %s", closeErr)
		}

		closeErr = session.Close()
		if closeErr != nil {
			t.Fatalf("Error closing connection: %s", closeErr)
		}
	}
	SpawnClientServerTest(t, "localhost:12843", client, server)
}

type ClientRoutine func(t *testing.T, server string)
type ServerRoutine func(t *testing.T, listener net.Listener)

var ClientServerTimeout = 300 * time.Millisecond
var DumpStackOnTimeout = true

// SpawnClientServer ensures two routines are run in parallel and a
// failure in one will cause the test to fail
func SpawnClientServerTest(t *testing.T, host string, client ClientRoutine, server ServerRoutine) {
	endClient := make(chan bool)
	endServer := make(chan bool)
	listenWait := make(chan bool)

	go func() {
		defer close(endClient)
		<-listenWait
		client(t, host)
	}()

	go func() {
		defer close(endServer)
		listener, listenErr := net.Listen("tcp", host)
		if listenErr != nil {
			t.Fatalf("Error creating listener: %s", listenErr)
		}
		defer func() {
			closeErr := listener.Close()
			if closeErr != nil {
				t.Fatalf("Error closing listener")
			}
		}()
		close(listenWait)
		server(t, listener)
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
