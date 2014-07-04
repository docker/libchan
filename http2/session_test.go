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
	Recv    *Channel
	Send    *Channel
}

type SimpleMessage struct {
	Message string
}

func TestChannelEncoding(t *testing.T) {
	client := func(t *testing.T, sender *Channel) {
		recv, err1 := sender.NewSubChannel(In)
		if err1 != nil {
			t.Fatalf("Error creating receive channel: %s", err1)
		}

		send, err2 := sender.NewSubChannel(Out)
		if err2 != nil {
			t.Fatalf("Error creating send channel: %s", err2)
		}

		m1 := &InOutMessage{
			Message: "WithInOut",
			Recv:    send,
			Send:    recv,
		}
		sendErr := sender.Communicate(m1)
		if sendErr != nil {
			t.Fatalf("Error sending InOutMessage: %s", sendErr)
		}

		m2 := &SimpleMessage{"This is a simple message"}
		sendErr = send.Communicate(m2)
		if sendErr != nil {
			t.Fatalf("Error sending simple message: %s", sendErr)
		}

		m3 := &SimpleMessage{}
		recvErr := recv.Communicate(m3)
		if recvErr != nil {
			t.Fatalf("Error receiving simple message: %s", recvErr)
		}
		if expected := "This is a responding simple message"; m3.Message != expected {
			t.Fatalf("Unexpected message value\n\tExpected: %s\n\tActual: %s", expected, m3.Message)
		}

		closeErr := send.Close()
		if closeErr != nil {
			t.Fatalf("Error closing s1: %s", closeErr)
		}

		closeErr = recv.Close()
		if closeErr != nil {
			t.Fatalf("Error closing s1: %s", closeErr)
		}
	}
	server := func(t *testing.T, receiver *Channel) {
		m1 := &InOutMessage{}
		receiveErr := receiver.Communicate(m1)
		if receiveErr != nil {
			t.Fatalf("Error receiving InOutMessage: %s", receiveErr)
		}

		if expected := "WithInOut"; m1.Message != expected {
			t.Fatalf("Unexpected message\n\tExpected: %s\n\tActual: %s", expected, m1.Message)
		}

		if m1.Recv == nil {
			t.Fatalf("Recv is nil")
		}

		if m1.Send == nil {
			t.Fatalf("Send is nil")
		}

		m2 := &SimpleMessage{}
		receiveErr = m1.Recv.Communicate(m2)
		if receiveErr != nil {
			t.Fatalf("Error receiving SimpleMessage: %s", receiveErr)
		}
		if expected := "This is a simple message"; m2.Message != expected {
			t.Fatalf("Unexpected message value:\n\tExpected: %s\n\tActual: %s", expected, m2.Message)
		}

		m3 := &SimpleMessage{"This is a responding simple message"}
		sendErr := m1.Send.Communicate(m3)
		if sendErr != nil {
			t.Fatalf("Error sending SimpleMessage: %s", sendErr)
		}

		closeErr := m1.Recv.Close()
		if closeErr != nil {
			t.Fatalf("Error closing recv connection: %s", closeErr)
		}

		closeErr = m1.Send.Close()
		if closeErr != nil {
			t.Fatalf("Error closing send connection: %s", closeErr)
		}
	}
	SpawnClientServerTest(t, "localhost:12843", ClientSendWrapper(client), ServerReceiveWrapper(server))
}

type ChannelAbstraction interface {
	Communicate(interface{}) error
	Close() error
}

type AbstractionMessage struct {
	Message string
	Channel ChannelAbstraction
}

func TestChannelAbstraction(t *testing.T) {
	client := func(t *testing.T, sender *Channel) {
		channel, channelErr := sender.NewSubChannel(Out)
		if channelErr != nil {
			t.Fatalf("Error creating sub channel: %s", channelErr)
		}

		m1 := &AbstractionMessage{
			Message: "irrelevant content",
			Channel: channel,
		}

		sendErr := sender.Communicate(m1)
		if sendErr != nil {
			t.Fatalf("Error sending channel: %s", sendErr)
		}

		closeErr := channel.Close()
		if closeErr != nil {
			t.Fatalf("Error closing sender: %s", closeErr)
		}
	}
	server := func(t *testing.T, receiver *Channel) {
		m1 := &AbstractionMessage{Channel: &Channel{}}
		recvErr := receiver.Communicate(m1)
		if recvErr != nil {
			t.Fatalf("Error receiving message: %s", recvErr)
		}

		if expected := "irrelevant content"; m1.Message != expected {
			t.Fatalf("Unexpected message value:\n\tExpected: %s\n\tActual: %s", expected, m1.Message)
		}

		closeErr := m1.Channel.Close()
		if closeErr != nil {
			t.Fatalf("Error closing channel: %s", closeErr)
		}
	}
	SpawnClientServerTest(t, "localhost:12943", ClientSendWrapper(client), ServerReceiveWrapper(server))
}

type MessageWithInput struct {
	Message string
	Input   *Channel
}

func TestBadDirection(t *testing.T) {
	client := func(t *testing.T, sender *Channel) {
		channel, channelErr := sender.NewSubChannel(In)
		if channelErr != nil {
			t.Fatalf("Error creating sub channel: %s", channelErr)
		}

		m1 := &MessageWithInput{
			Message: "contentless",
			Input:   channel,
		}

		sendErr := sender.Communicate(m1)
		if sendErr != nil {
			t.Fatalf("Error sending channel: %s", sendErr)
		}

		m2 := &SimpleMessage{"Supposedly input"}
		commErr := channel.Communicate(m2)
		if commErr != nil {
			t.Fatalf("Error receiving message")
		}

		if len(m2.Message) != 0 {
			t.Fatalf("Expected m2's value to be cleared after communicate")
		}

		closeErr := channel.Close()
		if closeErr != nil {
			t.Fatalf("Error closing sender: %s", closeErr)
		}
	}
	server := func(t *testing.T, receiver *Channel) {
		m1 := &MessageWithInput{}
		recvErr := receiver.Communicate(m1)
		if recvErr != nil {
			t.Fatalf("Error receiving message: %s", recvErr)
		}

		m2 := &SimpleMessage{}
		commErr := m1.Input.Communicate(m2)
		if commErr != nil {
			t.Fatalf("Error receiving message")
		}
		if len(m2.Message) != 0 {
			t.Fatalf("Expected m2's value to remain empty after communicate")
		}

		closeErr := m1.Input.Close()
		if closeErr != nil {
			t.Fatalf("Error closing channel: %s", closeErr)
		}
	}
	SpawnClientServerTest(t, "localhost:12943", ClientSendWrapper(client), ServerReceiveWrapper(server))
}

type MessageWithByteStream struct {
	Message string
	Stream  io.ReadWriteCloser
}

func TestByteStream(t *testing.T) {
	client := func(t *testing.T, sender *Channel) {
		bs, bsErr := sender.CreateByteStream()
		if bsErr != nil {
			t.Fatalf("Error creating byte stream: %s", bsErr)
		}

		m1 := &MessageWithByteStream{
			Message: "with a byte stream",
			Stream:  bs,
		}

		_, writeErr := bs.Write([]byte("Hello there server!"))
		if writeErr != nil {
			t.Fatalf("Error writing to byte stream: %s", writeErr)
		}

		sendErr := sender.Communicate(m1)
		if sendErr != nil {
			t.Fatalf("Error sending channel: %s", sendErr)
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
	server := func(t *testing.T, receiver *Channel) {
		m1 := &MessageWithByteStream{Stream: new(ByteStream)}
		recvErr := receiver.Communicate(m1)
		if recvErr != nil {
			t.Fatalf("Error receiving message: %s", recvErr)
		}
		if m1.Stream == nil {
			t.Fatalf("Missing byte stream")
		}
		bs, bsOk := m1.Stream.(*ByteStream)
		if !bsOk {
			t.Fatalf("Wrong byte stream type: %T", m1.Stream)
		}
		if bs.stream == nil {
			t.Fatalf("Bytestream missing underlying stream")
		}

		readBytes := make([]byte, 30)
		n, readErr := m1.Stream.Read(readBytes)
		if readErr != nil {
			t.Fatalf("Error reading from byte stream: %s", readErr)
		}
		if expected := "Hello there server!"; string(readBytes[:n]) != expected {
			t.Fatalf("Unexpected read value:\n\tExpected: %q\n\tActual: %q", expected, string(readBytes[:n]))
		}

		_, writeErr := bs.Write([]byte("G'day client ☺"))
		if writeErr != nil {
			t.Fatalf("Error writing to byte stream: %s", writeErr)
		}

		closeErr := m1.Stream.Close()
		if closeErr != nil {
			t.Fatalf("Error closing byte stream: %s", closeErr)
		}
	}
	SpawnClientServerTest(t, "localhost:12943", ClientSendWrapper(client), ServerReceiveWrapper(server))
}

func ClientSendWrapper(f func(t *testing.T, c *Channel)) ClientRoutine {
	return func(t *testing.T, server string) {
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

		f(t, sender)

		closeErr := sender.Close()
		if closeErr != nil {
			t.Fatalf("Error closing sender: %s", closeErr)
		}

		closeErr = session.Close()
		if closeErr != nil {
			t.Fatalf("Error closing connection: %s", closeErr)
		}
	}
}

func ServerReceiveWrapper(f func(t *testing.T, c *Channel)) ServerRoutine {
	return func(t *testing.T, listener net.Listener) {
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

		f(t, receiver)

		closeErr := receiver.Close()
		if closeErr != nil {
			t.Fatalf("Error closing receiver: %s", closeErr)
		}

		closeErr = session.Close()
		if closeErr != nil {
			t.Fatalf("Error closing connection: %s", closeErr)
		}
	}
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
