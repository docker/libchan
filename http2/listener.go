package http2

import (
	"net"
)

// ListenSession is a session manager which accepts new
// connections and spawns spdy connection managers.
type ListenSession struct {
	listener net.Listener
	auth     Authenticator
}

// NewListenSession creates a new listen session using
// a network listeners and function to authenticate
// new connections.  ListenSession expects tls session
// handling to occur by the authenticator or the listener,
// ListenSession will not perform tls handshakes.
func NewListenSession(listener net.Listener, auth Authenticator) (*ListenSession, error) {
	return &ListenSession{
		listener: listener,
		auth:     auth,
	}, nil
}

// Close closes the underlying listener
func (l *ListenSession) Close() error {
	return l.listener.Close()
}

// AcceptSessions waits for a new network connections
// and creates a new stream.  Connections which fail
// authentication will not be returned.
func (l *ListenSession) AcceptSession() (*StreamSession, error) {
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

		return newStreamSession(conn, true)
	}
}
