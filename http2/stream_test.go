package http2

import (
	"bytes"
	"github.com/docker/libchan"
	//"github.com/docker/spdystream"
	"io"
	"net"
	"testing"
)

func TestBeamSession(t *testing.T) {
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

	sender, senderErr := NewStreamSession(conn)
	if senderErr != nil {
		t.Fatalf("Error creating sender: %s", senderErr)
	}

	// Ls interaction
	receiver, sendErr := sender.Send(&libchan.Message{Data: []byte("Ls"), Ret: libchan.RetPipe})
	if sendErr != nil {
		t.Fatalf("Error sending libchan message: %s", sendErr)
	}
	message, receiveErr := receiver.Receive(0)
	if receiveErr != nil {
		t.Fatalf("Error receiving libchan message: %s", receiveErr)
	}
	if bytes.Compare(message.Data, []byte("Set")) != 0 {
		t.Errorf("Unexpected message name:\nActual: %s\nExpected: %s", message.Data, "Set")
	}

	// Attach interactions
	receiver, sendErr = sender.Send(&libchan.Message{Data: []byte("Attach"), Ret: libchan.RetPipe})
	if sendErr != nil {
		t.Fatalf("Error sending libchan message: %s", sendErr)
	}
	message, receiveErr = receiver.Receive(libchan.Ret)
	if receiveErr != nil {
		t.Fatalf("Error receiving libchan message: %s", receiveErr)
	}
	if bytes.Compare(message.Data, []byte("Ack")) != 0 {
		t.Errorf("Unexpected message name:\nActual: %s\nExpected: %s", message.Data, "Ack")
	}

	// TODO full connect interaction
	//if message.Fd == nil {
	//	t.Fatalf("Missing attachment on message")
	//}

	//testBytes := []byte("Hello")
	//n, writeErr := message.Fd.Write(testBytes)
	//if writeErr != nil {
	//	t.Fatalf("Error writing bytes: %s", writeErr)
	//}
	//if n != 5 {
	//	t.Fatalf("Unexpected number of bytes read:\nActual: %d\nExpected: 5", n)
	//}

	//buf := make([]byte, 10)
	//n, readErr := message.Att.Read(buf)
	//if readErr != nil {
	//	t.Fatalf("Error writing bytes: %s", readErr)
	//}
	//if n != 5 {
	//	t.Fatalf("Unexpected number of bytes read:\nActual: %d\nExpected: 5", n)
	//}
	//if bytes.Compare(buf[:n], testBytes) != 0 {
	//	t.Fatalf("Did not receive expected message:\nActual: %s\nExpectd: %s", buf, testBytes)
	//}

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

	session, sessionErr := NewListenSession(listener, NoAuthenticator)
	if sessionErr != nil {
		t.Fatalf("Error creating session: %s", sessionErr)
	}

	go session.Serve()

	go func() {
		defer close(endChan)
		// Ls exchange
		message, receiveErr := session.Receive(libchan.Ret)
		if receiveErr != nil {
			t.Fatalf("Error receiving on server: %s", receiveErr)
		}
		if bytes.Compare(message.Data, []byte("Ls")) != 0 {
			t.Fatalf("Unexpected data: %s", message.Data)
		}
		receiver, sendErr := message.Ret.Send(&libchan.Message{Data: []byte("Set")})
		if sendErr != nil {
			t.Fatalf("Error sending set message: %s", sendErr)
		}
		_, receiveErr = receiver.Receive(0)
		if receiveErr == nil {
			t.Fatalf("No error received from empty receiver")
		}
		if receiveErr != io.EOF {
			t.Fatalf("Expected error from empty receiver: %s", receiveErr)
		}

		// Connect exchange
		message, receiveErr = session.Receive(libchan.Ret)
		if receiveErr != nil {
			t.Fatalf("Error receiving on server: %s", receiveErr)
		}
		if bytes.Compare(message.Data, []byte("Attach")) != 0 {
			t.Fatalf("Unexpected data: %s", message.Data)
		}
		receiver, sendErr = message.Ret.Send(&libchan.Message{Data: []byte("Ack")})
		if sendErr != nil {
			t.Fatalf("Error sending set message: %s", sendErr)
		}

		// TODO full connect interaction

	}()

	return listener, nil
}
