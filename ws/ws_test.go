package ws

import (
	"bytes"
	"github.com/docker/libchan"
	"github.com/docker/libchan/unix"
	"github.com/gorilla/websocket"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
)

func TestServe(t *testing.T) {
	gotAck := make(chan bool)
	u := &Upgrader{}
	server := httptest.NewServer(Serve(u, func(s WebsocketSession) {
		receiver, waitErr := s.ReceiverWait()
		if waitErr != nil {
			t.Fatalf("Error waiting for receiver: %s", waitErr)
		}

		// Ls exchange
		message, receiveErr := receiver.Receive(0)
		if receiveErr != nil {
			t.Fatalf("Error receiving on server: %s", receiveErr)
		}
		if bytes.Compare(message.Data, []byte("Ls")) != 0 {
			t.Fatalf("Unexpected data: %s", message.Data)
		}
		_, sendErr := message.Ret.Send(&libchan.Message{Data: []byte("Set")})
		if sendErr != nil {
			t.Fatalf("Error sending set message: %s", sendErr)
		}

		// Connect exchange
		message, receiveErr = receiver.Receive(0)
		if receiveErr != nil {
			t.Fatalf("Error receiving on server: %s", receiveErr)
		}
		if bytes.Compare(message.Data, []byte("Attach")) != 0 {
			t.Fatalf("Unexpected data: %s", message.Data)
		}

		// Attach response
		conn, att, attErr := getAttachment()
		if attErr != nil {
			t.Fatalf("Error creating attachment: %s", attErr)
		}

		_, sendErr = message.Ret.Send(&libchan.Message{Data: []byte("Ack"), Fd: att})
		if sendErr != nil {
			t.Fatalf("Error sending set message: %s", sendErr)
		}

		testBytes := []byte("Hello")
		buf := make([]byte, 10)
		n, readErr := conn.Read(buf)
		if readErr != nil {
			t.Fatalf("Error writing bytes: %s", readErr)
		}
		if n != 5 {
			t.Fatalf("Unexpected number of bytes read:\nActual: %d\nExpected: 5", n)
		}
		if bytes.Compare(buf[:n], testBytes) != 0 {
			t.Fatalf("Did not receive expected message:\nActual: %s\nExpected: %s", buf, testBytes)
		}

		testBytes2 := []byte("from server")
		n, writeErr := conn.Write(testBytes2)
		if writeErr != nil {
			t.Fatalf("Error writing bytes: %s", writeErr)
		}
		if n != 11 {
			t.Fatalf("Unexpected number of bytes written:\nActual: %d\nExpected: 11", n)
		}
		conn.Close()

		<-gotAck
	}))

	wsConn, _, err := websocket.DefaultDialer.Dial(strings.Replace(server.URL, "http://", "ws://", 1), http.Header{"Origin": {server.URL}})
	if err != nil {
		t.Fatal(err)
	}
	session, sessionErr := NewClientSession(wsConn)
	if sessionErr != nil {
		t.Fatalf("Error creating session: %s", sessionErr)
	}

	sender, senderErr := session.NewSender()
	if senderErr != nil {
		t.Fatalf("Error creating sender: %s", senderErr)
	}

	// Ls interaction
	receiver, sendErr := sender.Send(&libchan.Message{Data: []byte("Ls")})
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
	receiver, sendErr = sender.Send(&libchan.Message{Data: []byte("Attach")}) //, Ret: libchan.RetPipe
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

	if message.Fd == nil {
		t.Fatalf("Missing attachment on message")
	}

	testBytes := []byte("Hello")
	n, writeErr := message.Fd.Write(testBytes)
	if writeErr != nil {
		t.Fatalf("Error writing bytes: %s", writeErr)
	}
	if n != 5 {
		t.Fatalf("Unexpected number of bytes written:\nActual: %d\nExpected: 5", n)
	}

	testBytes2 := []byte("from server")
	buf := make([]byte, 15)
	n, readErr := message.Fd.Read(buf)
	if readErr != nil {
		t.Fatalf("Error reading bytes: %s", readErr)
	}
	if n != 11 {
		t.Fatalf("Unexpected number of bytes read:\nActual: %d\nExpected: 11", n)
	}
	if bytes.Compare(buf[:n], testBytes2) != 0 {
		t.Fatalf("Did not receive expected message:\nActual: %s\nExpectd: %s", buf, testBytes2)
	}

	closeErr := sender.Close()
	if closeErr != nil {
		t.Fatalf("Error closing sender: %s", closeErr)
	}

	gotAck <- true
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
