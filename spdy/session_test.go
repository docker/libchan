package spdy

import (
	"io"
	"net"
	"os"
	"runtime/pprof"
	"testing"
	"time"

	"github.com/docker/libchan"
)

type InOutMessage struct {
	Message string
	Recv    libchan.Receiver
	Send    libchan.Sender
}

type SimpleMessage struct {
	Message string
}

func TestChannelEncoding(t *testing.T) {
	client := func(t *testing.T, sender libchan.Sender, s *SpdyTransport) {
		recv, s1, err1 := sender.CreateNestedReceiver()
		if err1 != nil {
			t.Fatalf("Error creating receive channel: %s", err1)
		}

		send, r1, err2 := sender.CreateNestedSender()
		if err2 != nil {
			t.Fatalf("Error creating send channel: %s", err2)
		}

		m1 := &InOutMessage{
			Message: "WithInOut",
			Recv:    r1,
			Send:    s1,
		}
		sendErr := sender.Send(m1)
		if sendErr != nil {
			t.Fatalf("Error sending InOutMessage: %s", sendErr)
		}

		m2 := &SimpleMessage{"This is a simple message"}
		sendErr = send.Send(m2)
		if sendErr != nil {
			t.Fatalf("Error sending simple message: %s", sendErr)
		}

		m3 := &SimpleMessage{}
		recvErr := recv.Receive(m3)
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
	server := func(t *testing.T, receiver libchan.Receiver, s *SpdyTransport) {
		m1 := &InOutMessage{}
		receiveErr := receiver.Receive(m1)
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
		receiveErr = m1.Recv.Receive(m2)
		if receiveErr != nil {
			t.Fatalf("Error receiving SimpleMessage: %s", receiveErr)
		}
		if expected := "This is a simple message"; m2.Message != expected {
			t.Fatalf("Unexpected message value:\n\tExpected: %s\n\tActual: %s", expected, m2.Message)
		}

		m3 := &SimpleMessage{"This is a responding simple message"}
		sendErr := m1.Send.Send(m3)
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

type AbstractionMessage struct {
	Message string
	Channel interface{}
}

func TestChannelAbstraction(t *testing.T) {
	client := func(t *testing.T, sender libchan.Sender, s *SpdyTransport) {
		send, recv, channelErr := sender.CreateNestedSender()
		if channelErr != nil {
			t.Fatalf("Error creating sub channel: %s", channelErr)
		}

		m1 := &AbstractionMessage{
			Message: "irrelevant content",
			Channel: recv,
		}

		sendErr := sender.Send(m1)
		if sendErr != nil {
			t.Fatalf("Error sending channel: %s", sendErr)
		}

		closeErr := send.Close()
		if closeErr != nil {
			t.Fatalf("Error closing sender: %s", closeErr)
		}
	}
	server := func(t *testing.T, receiver libchan.Receiver, s *SpdyTransport) {
		m1 := &AbstractionMessage{}
		recvErr := receiver.Receive(m1)
		if recvErr != nil {
			t.Fatalf("Error receiving message: %s", recvErr)
		}

		if expected := "irrelevant content"; m1.Message != expected {
			t.Fatalf("Unexpected message value:\n\tExpected: %s\n\tActual: %s", expected, m1.Message)
		}

		closeErr := m1.Channel.(libchan.Receiver).Close()
		if closeErr != nil {
			t.Fatalf("Error closing channel: %s", closeErr)
		}
	}
	SpawnClientServerTest(t, "localhost:12943", ClientSendWrapper(client), ServerReceiveWrapper(server))
}

type MessageWithInput struct {
	Message string
	Input   libchan.Receiver
}

func TestBadDirection(t *testing.T) {
	client := func(t *testing.T, sender libchan.Sender, s *SpdyTransport) {
		send, recv, channelErr := sender.CreateNestedSender()
		if channelErr != nil {
			t.Fatalf("Error creating sub channel: %s", channelErr)
		}

		m1 := &MessageWithInput{
			Message: "contentless",
			Input:   recv,
		}

		sendErr := sender.Send(m1)
		if sendErr != nil {
			t.Fatalf("Error sending channel: %s", sendErr)
		}

		m2 := &SimpleMessage{"Supposedly input"}
		commErr := recv.Receive(m2)
		if commErr == nil {
			t.Fatalf("No error receiving on sender")
		} else if commErr != ErrWrongDirection {
			t.Fatalf("Error receiving message: %s", commErr)
		}

		closeErr := send.Close()
		if closeErr != nil {
			t.Fatalf("Error closing sender: %s", closeErr)
		}
	}
	server := func(t *testing.T, receiver libchan.Receiver, s *SpdyTransport) {
		m1 := &MessageWithInput{}
		recvErr := receiver.Receive(m1)
		if recvErr != nil {
			t.Fatalf("Error receiving message: %s", recvErr)
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
	client := func(t *testing.T, sender libchan.Sender, s *SpdyTransport) {
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

		sendErr := sender.Send(m1)
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
	server := func(t *testing.T, receiver libchan.Receiver, s *SpdyTransport) {
		m1 := &MessageWithByteStream{}
		recvErr := receiver.Receive(m1)
		if recvErr != nil {
			t.Fatalf("Error receiving message: %s", recvErr)
		}
		if m1.Stream == nil {
			t.Fatalf("Missing byte stream")
		}
		bs, bsOk := m1.Stream.(*byteStream)
		if !bsOk {
			t.Fatalf("Wrong byte stream type: %T", m1.Stream)
		}
		if bs.Stream == nil {
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
	SpawnClientServerTest(t, "localhost:12944", ClientSendWrapper(client), ServerReceiveWrapper(server))
}

type WrappedMessage struct {
	Message string
	Wrapped io.ReadWriteCloser
}

func TestWrappedByteStreams(t *testing.T) {
	serverSend := "G'day client ☺"
	clientReply := "Hello Server, ☢ FYI your stream was transparently copied ☠"
	client := func(t *testing.T, sender libchan.Sender, s *SpdyTransport) {
		// Create pipe
		p1, p2 := net.Pipe()

		m1 := &WrappedMessage{
			Message: "wrapped",
			Wrapped: libchan.ByteStreamWrapper{p2},
		}

		sendErr := sender.Send(m1)
		if sendErr != nil {
			t.Fatalf("Error sending channel: %s", sendErr)
		}

		// read
		readBytes := make([]byte, 30)
		n, readErr := p1.Read(readBytes)
		if readErr != nil {
			t.Fatalf("Error reading from byte stream: %s", readErr)
		}
		if expected := serverSend; string(readBytes[:n]) != expected {
			t.Fatalf("Unexpected read value:\n\tExpected: %q\n\tActual: %q", expected, string(readBytes[:n]))
		}

		// write
		_, writeErr := p1.Write([]byte(clientReply))
		if writeErr != nil {
			t.Fatalf("Error writing to byte stream: %s", writeErr)
		}

	}
	server := func(t *testing.T, receiver libchan.Receiver, s *SpdyTransport) {
		m1 := &WrappedMessage{}
		recvErr := receiver.Receive(m1)
		if recvErr != nil {
			t.Fatalf("Error receiving message: %s", recvErr)
		}

		if expected := "wrapped"; m1.Message != expected {
			t.Fatalf("Unexpected message\n\tExpected: %s\n\tActual: %s", expected, m1.Message)
		}

		_, writeErr := m1.Wrapped.Write([]byte(serverSend))
		if writeErr != nil {
			t.Fatalf("Error writing to byte stream: %s", writeErr)
		}

		readBytes := make([]byte, 80)
		n, readErr := m1.Wrapped.Read(readBytes)
		if readErr != nil {
			t.Fatalf("Error reading from byte stream: %s", readErr)
		}
		if expected := clientReply; string(readBytes[:n]) != expected {
			t.Fatalf("Unexpected read value:\n\tExpected: %q\n\tActual: %q", expected, string(readBytes[:n]))
		}

	}
	SpawnClientServerTest(t, "localhost:12943", ClientSendWrapper(client), ServerReceiveWrapper(server))
}

func ClientSendWrapper(f func(t *testing.T, c libchan.Sender, s *SpdyTransport)) ClientRoutine {
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

		f(t, sender, session)

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

func ServerReceiveWrapper(f func(t *testing.T, c libchan.Receiver, s *SpdyTransport)) ServerRoutine {
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

		f(t, receiver, session)

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
