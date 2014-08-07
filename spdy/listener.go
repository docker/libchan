package spdy

import (
	"net"
)

// TransportListener is a listener which accepts new
// connections angi rd spawns spdy transports.
type TransportListener struct {
	listener net.Listener
	auth     Authenticator
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
	for {
		conn, err := l.listener.Accept()
		if err != nil {
			return nil, err
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
