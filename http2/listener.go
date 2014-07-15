package http2

import "net"

// SessionListener is a listener which accepts new
// connections angi rd spawns spdy sessions.
type SessionListener struct {
	listener net.Listener
	auth     Authenticator
}

// NewSessionListener creates a new listen session using
// a network listeners and function to authenticate
// new connections.  SessionListener expects tls session
// handling to occur by the authenticator or the listener,
// SessionListener will not perform tls handshakes.
func NewSessionListener(listener net.Listener, auth Authenticator) (*SessionListener, error) {
	return &SessionListener{
		listener: listener,
		auth:     auth,
	}, nil
}

// Close closes the underlying listener
func (l *SessionListener) Close() error {
	return l.listener.Close()
}

// AcceptSessions waits for a new network connections
// and creates a new stream.  Connections which fail
// authentication will not be returned.
func (l *SessionListener) AcceptSession() (*Session, error) {
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
