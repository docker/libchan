package http2

import (
	"encoding/base64"
	"fmt"
	"github.com/docker/libchan"
	"github.com/docker/spdystream"
	"io"
	"net"
	"net/http"
	"os"
	"syscall"
)

type Authenticator func(conn net.Conn) (spdystream.AuthHandler, error)

func NoAuthenticator(conn net.Conn) (spdystream.AuthHandler, error) {
	return func(header http.Header, slot uint8, parent uint32) bool {
		return true
	}, nil
}

type streamChanProvider interface {
	addStreamChan(stream *spdystream.Stream, streamChan chan *spdystream.Stream)
	getStreamChan(stream *spdystream.Stream) chan *spdystream.Stream
}

func createStreamMessage(stream *spdystream.Stream, mode int, streamChans streamChanProvider, ret libchan.Sender) (*libchan.Message, error) {
	dataString := stream.Headers()["Data"]
	if len(dataString) != 1 {
		if len(dataString) == 0 {
			return nil, fmt.Errorf("Stream(%s) is missing data header", stream)
		} else {
			return nil, fmt.Errorf("Stream(%s) has multiple data headers", stream)
		}
	}

	data, decodeErr := base64.URLEncoding.DecodeString(dataString[0])
	if decodeErr != nil {
		return nil, decodeErr
	}

	var attach *os.File
	if !stream.IsFinished() {
		socketFds, socketErr := syscall.Socketpair(syscall.AF_LOCAL, syscall.SOCK_STREAM|syscall.FD_CLOEXEC, 0)
		if socketErr != nil {
			return nil, socketErr
		}
		attach = os.NewFile(uintptr(socketFds[0]), "")
		conn, connErr := net.FileConn(os.NewFile(uintptr(socketFds[1]), ""))
		if connErr != nil {
			return nil, connErr
		}

		go func() {
			io.Copy(conn, stream)
		}()
		go func() {
			io.Copy(stream, conn)
		}()
	}

	retSender := ret
	if retSender == nil || libchan.RetPipe.Equals(retSender) {
		retSender = &StreamSender{stream: stream, streamChans: streamChans}
	}

	if mode&libchan.Ret == 0 {
		retSender.Close()
	}

	return &libchan.Message{
		Data: data,
		Fd:   attach,
		Ret:  retSender,
	}, nil
}
