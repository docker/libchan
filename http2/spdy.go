// Package http2 provides a libchan implementation using
// spdy based http2 draft.  This package does not explicitly
// provide or enforce tls security, but allows being used with
// secured connections.
package http2

import (
	"net"
)

// Authenticator is a function to provide authentication to
// a new connection.  Authenticator allows tls handshakes to
// occur or any desired authentication of a network connection
// before passing off the connection to session management.
type Authenticator func(conn net.Conn) error

// NoAuthenticator is an implementation of authenticator which
// does no security.  This should only be used for testing or with
// caution when network connections are already guarenteed to
// be secure.
func NoAuthenticator(conn net.Conn) error {
	return nil
}
