package http2

import (
	"bytes"
	"io"
	"net"
	"os"
	"testing"

	"github.com/docker/libchan"
	"github.com/docker/libchan/unix"
)

func TestLibchanSession(t *testing.T) {
	end := make(chan bool)
	listen := "localhost:7543"
	server, serverErr := runServer(listen, t, end)
	if serverErr != nil {
		t.Fatalf("Error initializing server: %s", serverErr)
	}

	conn, connErr := net.Dial("tcp", listen)
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

	// Ls interaction
	m := &libchan.Message{}
	encodeErr := m.Encode(&Command{"Ls"})
	if encodeErr != nil {
		t.Fatal(encodeErr)
	}
	receiver, sendErr := sender.Send(m)
	if sendErr != nil {
		t.Fatalf("Error sending libchan message: %s", sendErr)
	}
	message, receiveErr := receiver.Receive(0)
	if receiveErr != nil {
		t.Fatalf("Error receiving libchan message: %s", receiveErr)
	}
	var command Command
	decodeErr := message.Decode(&command)
	if decodeErr != nil {
		t.Fatal(decodeErr)
	}
	if command.Verb != "Set" {
		t.Errorf("Unexpected command verb:\nActual: %s\nExpected: %s", command.Verb, "Set")
	}

	// Attach interactions
	m = &libchan.Message{}
	encodeErr = m.Encode(&Command{"Attach"})
	if encodeErr != nil {
		t.Fatal(encodeErr)
	}
	receiver, sendErr = sender.Send(m) //, Ret: libchan.RetPipe
	if sendErr != nil {
		t.Fatalf("Error sending libchan message: %s", sendErr)
	}
	message, receiveErr = receiver.Receive(libchan.Ret)
	if receiveErr != nil {
		t.Fatalf("Error receiving libchan message: %s", receiveErr)
	}
	command = Command{}
	decodeErr = message.Decode(&command)
	if decodeErr != nil {
		t.Fatal(decodeErr)
	}
	if command.Verb != "Ack" {
		t.Errorf("Unexpected command verb:\nActual: %s\nExpected: %s", command.Verb, "Ack")
	}

	if message.Stream == nil {
		t.Fatalf("Missing attachment on message")
	}

	testBytes := []byte("Hello")
	n, writeErr := message.Stream.Write(testBytes)
	if writeErr != nil {
		t.Fatalf("Error writing bytes: %s", writeErr)
	}
	if n != 5 {
		t.Fatalf("Unexpected number of bytes written:\nActual: %d\nExpected: 5", n)
	}

	testBytes2 := []byte("from server")
	buf := make([]byte, 15)
	n, readErr := message.Stream.Read(buf)
	if readErr != nil {
		t.Fatalf("Error writing bytes: %s", readErr)
	}
	if n != 11 {
		t.Fatalf("Unexpected number of bytes read:\nActual: %d\nExpected: 11", n)
	}
	if bytes.Compare(buf[:n], testBytes2) != 0 {
		t.Fatalf("Did not receive expected message:\nActual: %s\nExpectd: %s", buf, testBytes2)
	}

	closeErr := server.Close()
	if closeErr != nil {
		t.Fatalf("Error closing server: %s", closeErr)
	}

	closeErr = sender.Close()
	if closeErr != nil {
		t.Fatalf("Error closing sender: %s", closeErr)
	}
	<-end
}

func runServer(listen string, t *testing.T, endChan chan bool) (io.Closer, error) {
	listener, lErr := net.Listen("tcp", listen)
	if lErr != nil {
		return nil, lErr
	}

	listenSession, listenSessionErr := NewListenSession(listener, NoAuthenticator)
	if listenSessionErr != nil {
		t.Fatalf("Error creating session listener: %s", listenSessionErr)
	}

	go func() {
		defer close(endChan)

		session, sessionErr := listenSession.AcceptSession()
		if sessionErr != nil {
			t.Fatalf("Error accepting session: %s", sessionErr)
		}

		receiver, receiverErr := session.ReceiverWait()
		if receiverErr != nil {
			t.Fatalf("Error accepting receiver: %s", receiverErr)
		}

		// Ls exchange
		message, receiveErr := receiver.Receive(0)
		if receiveErr != nil {
			t.Fatalf("Error receiving on server: %s", receiveErr)
		}

		var command Command
		decodeErr := message.Decode(&command)
		if decodeErr != nil {
			t.Fatal(decodeErr)
		}
		if command.Verb != "Ls" {
			t.Fatalf("Unexpected command: %s", command.Verb)
		}

		m := &libchan.Message{}
		encodeErr := m.Encode(&Command{"Set"})
		if encodeErr != nil {
			t.Fatal(encodeErr)
		}
		_, sendErr := message.Ret.Send(m)
		if sendErr != nil {
			t.Fatalf("Error sending set message: %s", sendErr)
		}

		// Connect exchange
		message, receiveErr = receiver.Receive(0)
		if receiveErr != nil {
			t.Fatalf("Error receiving on server: %s", receiveErr)
		}

		command = Command{}
		decodeErr = message.Decode(&command)
		if decodeErr != nil {
			t.Fatal(decodeErr)
		}
		if command.Verb != "Attach" {
			t.Fatalf("Unexpected command: %s", command.Verb)
		}

		// Attach response
		conn, att, attErr := getAttachment()
		if attErr != nil {
			t.Fatalf("Error creating attachment: %s", attErr)
		}
		defer att.Close()
		defer conn.Close()

		m = &libchan.Message{Stream: att}
		encodeErr = m.Encode(&Command{"Ack"})
		if encodeErr != nil {
			t.Fatal(encodeErr)
		}
		_, sendErr = message.Ret.Send(m)
		if sendErr != nil {
			t.Fatalf("Error sending set message: %s", sendErr)
		}

		testBytes := []byte("Hello")
		buf := make([]byte, 10)
		n, readErr := conn.Read(buf)
		if readErr != nil {
			t.Fatalf("Error reading bytes: %s", readErr)
		}
		if n != 5 {
			t.Fatalf("Unexpected number of bytes read:\nActual: %d\nExpected: 5", n)
		}
		if bytes.Compare(buf[:n], testBytes) != 0 {
			t.Fatalf("Did not receive expected message:\nActual: %s\nExpectd: %s", buf, testBytes)
		}

		testBytes2 := []byte("from server")
		n, writeErr := conn.Write(testBytes2)
		if writeErr != nil {
			t.Fatalf("Error writing bytes: %s", writeErr)
		}
		if n != 11 {
			t.Fatalf("Unexpected number of bytes written:\nActual: %d\nExpected: 11", n)
		}
	}()

	return listener, nil
}

func getAttachment() (io.ReadWriteCloser, *os.File, error) {
	f1, f2, err := unix.SocketPair()
	if err != nil {
		return nil, nil, err
	}
	fConn, err := net.FileConn(f1)
	if err != nil {
		return nil, nil, err
	}
	return fConn, f2, nil
}
