package spdy

import (
	"testing"
	"time"
	"net"
)

// Test that server detects client session is dead while waiting for receive channel
func TestListenerTimeout(t *testing.T) {
	// Test default behavior without timeout
	noWait(t)
	// Test enabling timeout behavior
	withWait(t)
}

func noWait(t *testing.T) {
	// Start listener, ensure it doesn't throw error after 100 ms of no connecton
	timeoutChan := make(chan bool)
	go func() {
		time.Sleep(time.Millisecond * 200)
		close(timeoutChan)
	}()
	// Start listener, ensure it does throw error after 100 ms of no connection
	listener, _ := net.Listen("tcp", "localhost:12945")
	go func() {
		transportListener, _ := NewTransportListener(listener, NoAuthenticator)
		_, err := transportListener.AcceptTransport()
		t.Fatal(err)
	}()
	<-timeoutChan
}

func withWait(t *testing.T) {
	timeoutChan := make(chan bool)
	go func() {
		time.Sleep(time.Millisecond * 200)
		timeoutChan<-false
	}()
	// Start listener, ensure it does throw error after 100 ms of no connection
	listener, _ := net.Listen("tcp", "localhost:12946")
	go func() {
		transportListener, _ := NewTransportListener(listener, NoAuthenticator)
		transportListener.SetTimeout(time.Millisecond * 100)
		_, err := transportListener.AcceptTransport()
		if err.Error() != "listener timed out (100ms)" {
			t.Fatal(err.Error() + ", should have timed out at (100ms)")
		}
		timeoutChan<-true
	}()
	select {
	case ok := <-timeoutChan:
		if !ok {
			t.Fatal("timeout expected and did not occur")
		}
	}
}
