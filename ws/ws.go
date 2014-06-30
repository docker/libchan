package ws

import (
	"errors"
	"github.com/docker/libchan"
	"github.com/docker/libchan/http2"
	"github.com/docker/spdystream/ws"
	"github.com/gorilla/websocket"
	"net/http"
)

type WebsocketSession interface {
	NewSender() (libchan.Sender, error)
	ReceiverWait() (libchan.Receiver, error)
}

// Connect to a libchan server over a Websocket connection as a client
func NewClientSession(wsConn *websocket.Conn) (WebsocketSession, error) {
	session, err := http2.NewStreamSession(ws.NewConnection(wsConn))
	if err != nil {
		return nil, err
	}
	return session, nil
}

// Upgrade an HTTP connection to a libchan over HTTP2 over
// Websockets connection.
type Upgrader struct {
	Upgrader websocket.Upgrader
}

func (u *Upgrader) Upgrade(w http.ResponseWriter, r *http.Request, responseHeader http.Header) (*http2.StreamSession, error) {
	wsConn, err := u.Upgrader.Upgrade(w, r, responseHeader)
	if err != nil {
		return nil, err
	}

	netConn := ws.NewConnection(wsConn)
	server, err := http2.NewServerStreamSession(netConn)
	if err != nil {
		netConn.Close()
		return nil, err
	}

	return server, nil
}

// Returns true if a handshake error occured in websockets, which means
// a response has already been written to the stream.
func IsHandshakeError(err error) bool {
	_, ok := err.(websocket.HandshakeError)
	return ok
}

type HandlerFunc func(WebsocketSession)

// Handler function for serving libchan over HTTP.  Will invoke f and
// then close the server's libchan endpoint after f returns.
func Serve(u *Upgrader, f HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != "GET" {
			u.Upgrader.Error(w, r, http.StatusMethodNotAllowed, errors.New("Method not allowed"))
			return
		}

		session, err := u.Upgrade(w, r, nil)
		if err != nil {
			if !IsHandshakeError(err) {
				u.Upgrader.Error(w, r, http.StatusInternalServerError, errors.New("Unable to open an HTTP2 connection over Websockets"))
			}
			return
		}
		defer session.Close()

		f(session)
	}
}
