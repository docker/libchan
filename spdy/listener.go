package spdy

import (
	"net"
	"time"
	"errors"
	"fmt"
)

// TransportListener is a listener which accepts new
// connections angi rd spawns spdy transports.
type TransportListener struct {
	listener net.Listener
	auth     Authenticator
	timeout  *time.Duration
}

// NewTransportListener creates a new listen transport using
// a network listeners and function to authenticate
// new connections.  TransportListener expects tls session
// handling to occur by the authenticator or the listener,
// TransportListener will not perform tls handshakes.
func NewTransportListener(listener net.Listener, auth Authenticator) (*TransportListener, error) {
	return &TransportListener{
		listener: listener,
		auth:     auth,
	}, nil
}

// Close closes the underlying listener
func (l *TransportListener) Close() error {
	return l.listener.Close()
}

// AcceptTransport waits for a new network connections
// and creates a new stream.  Connections which fail
// authentication will not be returned.
func (l *TransportListener) AcceptTransport() (*Transport, error) {
	tranChan := make(chan interface{})
	for {
		// The timeout channel has a buffer of 1
		// to allow the timeout goroutine to exit
		// if nothing is listening anymore. This prevents
		// it form hanging forever waiting on a receiver.
		timeoutChan := make(chan bool, 1)
		// Launch listener wait inside a goroutine passing
		// transport channel
		go l.waitForAccept(tranChan)
		// If timeout provided launch timeout goroutine with
		// duration to wait and timeout channel
		if l.timeout != nil {
			go waitForTimeout(*l.timeout, timeoutChan)
		}

		// Wait for new connection channel or timeout channel
		var conn net.Conn
		select {
		case x := <-tranChan:
			if x, ok := x.(net.Conn); ok {
				conn = x
			} else if x, ok := x.(error); ok {
				return nil, x
			}
		case <-timeoutChan:
			// We have timed out
			return nil, errors.New(fmt.Sprintf("listener timed out (%s)", l.timeout.String()))
		}

		authErr := l.auth(conn)
		if authErr != nil {
			// TODO log
			conn.Close()
			continue
		}

		return newSession(conn, true)
	}
}

func (l *TransportListener) waitForAccept(tranChan chan interface{}) {
	conn, err := l.listener.Accept()
	if err != nil {
		tranChan<-err
	}
	tranChan<-conn
	return
}

// Sets the timeout for this listener. AcceptTransport() will return if no
// connection is opened for t amount of time.
func (l *TransportListener) SetTimeout(t time.Duration) {
	l.timeout = &t
}

// Function to wait for a timeout condition and then signal to a channel
func waitForTimeout(d time.Duration, timeoutChan chan bool) {
	time.Sleep(d)
	timeoutChan<-true
}

