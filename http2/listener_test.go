package http2

import (
	"bytes"
	"net"
	"testing"

	"github.com/docker/libchan"
)

func TestListenSession(t *testing.T) {
	listen := "localhost:7743"
	listener, listenErr := net.Listen("tcp", listen)
	if listenErr != nil {
		t.Fatalf("Error creating listener: %s", listenErr)
	}

	listenSession, listenSessionErr := NewListenSession(listener, NoAuthenticator)
	if listenSessionErr != nil {
		t.Fatalf("Error creating session: %s", listenSessionErr)
	}

	end := make(chan bool)
	go exerciseServer(t, listen, end)

	session, sessionErr := listenSession.AcceptSession()
	if sessionErr != nil {
		t.Fatalf("Error accepting session: %s", sessionErr)
	}
	receiver, receiverErr := session.ReceiverWait()
	if receiverErr != nil {
		t.Fatalf("Error accepting receiver: %s", receiverErr)
	}

	msg, msgErr := receiver.Receive(libchan.Ret)
	if msgErr != nil {
		t.Fatalf("Error receiving message: %s", msgErr)
	}
	if msg.Fd == nil {
		t.Fatalf("Error message missing attachment")
	}
	if bytes.Compare(msg.Data, []byte("Attach")) != 0 {
		t.Fatalf("Wrong verb\nActual: %s\nExpecting: %s", msg.Data, "Attach")
	}

	receiver, sendErr := msg.Ret.Send(&libchan.Message{Data: []byte("Ack")})
	if sendErr != nil {
		t.Fatalf("Error sending return message: %s", sendErr)
	}

	<-end
	shutdownErr := session.Close()
	if shutdownErr != nil {
		t.Fatalf("Error shutting down: %s", shutdownErr)
	}
}

func exerciseServer(t *testing.T, server string, endChan chan bool) {
	defer close(endChan)

	conn, connErr := net.Dial("tcp", server)
	if connErr != nil {
		t.Fatalf("Error dialing server: %s", connErr)
	}

	session, sessionErr := NewStreamSession(conn)
	if sessionErr != nil {
		t.Fatalf("Error creating session: %s", sessionErr)
	}
	sender, senderErr := session.NewSender()
	if senderErr != nil {
		t.Fatalf("Error creating sender: %s", senderErr)
	}

	receiver, sendErr := sender.Send(&libchan.Message{Data: []byte("Attach"), Ret: libchan.RetPipe})
	if sendErr != nil {
		t.Fatalf("Error sending message: %s", sendErr)
	}

	msg, receiveErr := receiver.Receive(0)
	if receiveErr != nil {
		t.Fatalf("Error receiving message")
	}

	if bytes.Compare(msg.Data, []byte("Ack")) != 0 {
		t.Fatalf("Wrong verb\nActual: %s\nExpecting: %s", msg.Data, "Ack")
	}
}
