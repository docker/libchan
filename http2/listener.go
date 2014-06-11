package http2

import (
	"github.com/docker/libchan"
	"github.com/docker/spdystream"
	"net"
	"sync"
)

// ListenSession is a session manager which accepts new
// connections and spawns spdy connection managers.
type ListenSession struct {
	listener       net.Listener
	streamChan     chan *spdystream.Stream
	streamLock     sync.RWMutex
	subStreamChans map[string]chan *spdystream.Stream
	auth           Authenticator
}

// NewListenSession creates a new listen session using
// a network listeners and function to authenticate
// new connections.  ListenSession expects tls session
// handling to occur by the authenticator or the listener,
// ListenSession will not perform tls handshakes.
func NewListenSession(listener net.Listener, auth Authenticator) (*ListenSession, error) {
	return &ListenSession{
		listener:       listener,
		streamChan:     make(chan *spdystream.Stream),
		subStreamChans: make(map[string]chan *spdystream.Stream),
		auth:           auth,
	}, nil
}

func (l *ListenSession) streamHandler(stream *spdystream.Stream) {
	streamChan := l.getStreamChan(stream.Parent())
	streamChan <- stream
}

func (l *ListenSession) addStreamChan(stream *spdystream.Stream, streamChan chan *spdystream.Stream) {
	l.streamLock.Lock()
	l.subStreamChans[stream.String()] = streamChan
	l.streamLock.Unlock()
}

func (l *ListenSession) getStreamChan(stream *spdystream.Stream) chan *spdystream.Stream {
	if stream == nil {
		return l.streamChan
	}
	l.streamLock.RLock()
	defer l.streamLock.RUnlock()
	streamChan, ok := l.subStreamChans[stream.String()]
	if ok {
		return streamChan
	}
	return l.streamChan
}

// Serve runs the listen accept loop, spawning connection manager
// for each accepted connection. This function will block until
// the listener is closed or an error occurs on accept.
func (l *ListenSession) Serve() {
	for {
		conn, err := l.listener.Accept()
		if err != nil {
			// TODO log
			break
		}

		go func() {
			authHandler, authErr := l.auth(conn)
			if authErr != nil {
				// TODO log
				conn.Close()
				return
			}

			spdyConn, spdyErr := spdystream.NewConnection(conn, true)
			if spdyErr != nil {
				// TODO log
				conn.Close()
				return
			}

			go spdyConn.Serve(l.streamHandler, authHandler)
		}()
	}
}

// Close closes the underlying listener
func (l *ListenSession) Close() error {
	return l.listener.Close()
}

// AcceptReceiver waits for a stream to be created in
// the session and returns a receiver for that stream.
func (l *ListenSession) AcceptReceiver() (libchan.Receiver, error) {
	stream := <-l.streamChan
	return &StreamReceiver{stream: stream, streamChans: l, ret: &StreamSender{stream: stream, streamChans: l}}, nil
}
