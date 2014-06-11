package http2

import (
	"github.com/docker/libchan"
	"github.com/docker/spdystream"
	"net"
	"sync"
)

type ListenSession struct {
	listener       net.Listener
	streamChan     chan *spdystream.Stream
	streamLock     sync.RWMutex
	subStreamChans map[string]chan *spdystream.Stream
	auth           Authenticator
}

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

func (l *ListenSession) Close() error {
	return l.listener.Close()
}

func (l *ListenSession) AcceptReceiver() (libchan.Receiver, error) {
	stream := <-l.streamChan
	return &StreamReceiver{stream: stream, streamChans: l, ret: &StreamSender{stream: stream, streamChans: l}}, nil
}
