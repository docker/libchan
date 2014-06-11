package utils

import (
	"github.com/docker/libchan"
)

type Buffer []*libchan.Message

func (buf *Buffer) Send(msg *libchan.Message) (libchan.Receiver, error) {
	(*buf) = append(*buf, msg)
	return libchan.NopReceiver{}, nil
}

func (buf *Buffer) Close() error {
	(*buf) = nil
	return nil
}
