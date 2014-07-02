package http2

import (
	"github.com/docker/libchan"
	"net"
	"testing"
	"time"
)

type Command struct {
	Verb string
}

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
	session.conn.SetCloseTimeout(30 * time.Millisecond)
	receiver, receiverErr := session.ReceiverWait()
	if receiverErr != nil {
		t.Fatalf("Error accepting receiver: %s", receiverErr)
	}

	msg, msgErr := receiver.Receive(libchan.Ret)
	if msgErr != nil {
		t.Fatalf("Error receiving message: %s", msgErr)
	}
	if msg.Stream == nil {
		t.Fatalf("Error message missing attachment")
	}
	var command Command
	decodeErr := msg.Decode(&command)
	if decodeErr != nil {
		t.Fatalf("Error decoding attach: %s", decodeErr)
	}
	if command.Verb != "Attach" {
		t.Fatalf("Wrong verb\nActual: %s\nExpecting: %s", command.Verb, "Attach")
	}

	message := &libchan.Message{}
	encodeErr := message.Encode(&Command{"Ack"})
	if encodeErr != nil {
		t.Fatalf("Error encoding ack: %s", encodeErr)
	}
	receiver, sendErr := msg.Ret.Send(message)
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
	session.conn.SetCloseTimeout(30 * time.Millisecond)
	sender, senderErr := session.NewSender()
	if senderErr != nil {
		t.Fatalf("Error creating sender: %s", senderErr)
	}

	message := &libchan.Message{Ret: libchan.RetPipe}
	encodeErr := message.Encode(&Command{"Attach"})
	if encodeErr != nil {
		t.Fatalf("Error encoding attach: %s", encodeErr)
	}
	receiver, sendErr := sender.Send(message)
	if sendErr != nil {
		t.Fatalf("Error sending message: %s", sendErr)
	}

	msg, receiveErr := receiver.Receive(0)
	if receiveErr != nil {
		t.Fatalf("Error receiving message")
	}

	var command Command
	decodeErr := msg.Decode(&command)
	if decodeErr != nil {
		t.Fatalf("Error decoding ack: %s", decodeErr)
	}
	if command.Verb != "Ack" {
		t.Fatalf("Wrong verb\nActual: %s\nExpecting: %s", command.Verb, "Ack")
	}
}
