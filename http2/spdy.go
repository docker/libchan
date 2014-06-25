// Package http2 provides a libchan implementation using
// spdy based http2 draft.  This package does not explicitly
// provide or enforce tls security, but allows being used with
// secured connections.
package http2

import (
	"encoding/base64"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"syscall"

	"github.com/docker/spdystream"
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

type streamChanProvider interface {
	addStreamChan(stream *spdystream.Stream, streamChan chan *spdystream.Stream)
	getStreamChan(stream *spdystream.Stream) chan *spdystream.Stream
}

func extractDataHeader(headers http.Header) ([]byte, error) {
	dataString := headers["Data"]
	if len(dataString) != 1 {
		if len(dataString) == 0 {
			return nil, errors.New("missing data header")
		} else {
			return nil, errors.New("multiple data headers")
		}
	}
	data, decodeErr := base64.URLEncoding.DecodeString(dataString[0])
	if decodeErr != nil {
		return nil, decodeErr
	}
	return data, nil
}

func createAttachment(stream *spdystream.Stream) (*os.File, error) {
	if stream.IsFinished() {
		return nil, fmt.Errorf("stream already finished")
	}

	socketFds, socketErr := syscall.Socketpair(syscall.AF_LOCAL, syscall.SOCK_STREAM|syscall.FD_CLOEXEC, 0)
	if socketErr != nil {
		return nil, socketErr
	}
	pipe := os.NewFile(uintptr(socketFds[1]), "")
	defer pipe.Close()
	conn, connErr := net.FileConn(pipe)
	if connErr != nil {
		return nil, connErr
	}

	go func() {
		io.Copy(conn, stream)
		conn.Close()
	}()
	go func() {
		io.Copy(stream, conn)
	}()

	return os.NewFile(uintptr(socketFds[0]), ""), nil
}
