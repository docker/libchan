package http2

import (
	"encoding/binary"
	"errors"
	"github.com/ugorji/go/codec"
	"reflect"
)

type sessionHandler struct {
	session *Session
}

func (h *sessionHandler) encodeSendChannel(v reflect.Value) ([]byte, error) {
	sc := v.Interface().(SendChannel)
	if sc.stream == nil {
		return nil, errors.New("bad type")
	}

	// Get stream identifier?
	streamId := sc.stream.Identifier()
	var buf [8]byte
	written := binary.PutUvarint(buf[:], uint64(streamId))
	if written > 4 {
		return nil, errors.New("wrote unexpected stream id size")
	}
	return buf[:written], nil
}

func (h *sessionHandler) decodeSendChannel(v reflect.Value, b []byte) error {
	sc := v.Interface().(SendChannel)

	streamId, read := binary.Uvarint(b)
	if read > 4 {
		return errors.New("read unexpected stream id size")
	}
	stream := h.session.conn.FindStream(uint32(streamId))
	if stream == nil {
		return errors.New("stream does not exist")
	}
	sc.stream = stream
	sc.remote = false
	v.Set(reflect.ValueOf(sc))

	return nil
}

func (h *sessionHandler) encodeReceiveChannel(v reflect.Value) ([]byte, error) {
	rc := v.Interface().(ReceiveChannel)
	if rc.stream == nil {
		return nil, errors.New("bad type")
	}

	// Get stream identifier?
	streamId := rc.stream.Identifier()
	var buf [8]byte
	written := binary.PutUvarint(buf[:], uint64(streamId))
	if written > 4 {
		return nil, errors.New("wrote unexpected stream id size")
	}
	return buf[:written], nil
}

func (h *sessionHandler) decodeReceiveChannel(v reflect.Value, b []byte) error {
	rc := v.Interface().(ReceiveChannel)

	streamId, readN := binary.Uvarint(b)
	if readN > 4 {
		return errors.New("read unexpected stream id size")
	}
	stream := h.session.conn.FindStream(uint32(streamId))
	if stream == nil {
		return errors.New("stream does not exist")
	}
	rc.stream = stream
	rc.remote = false
	v.Set(reflect.ValueOf(rc))

	return nil
}

func getMsgPackHandler(session *Session) *codec.MsgpackHandle {
	h := &sessionHandler{session: session}
	mh := &codec.MsgpackHandle{}
	err := mh.AddExt(reflect.TypeOf(SendChannel{}), 1, h.encodeSendChannel, h.decodeSendChannel)
	if err != nil {
		panic(err)
	}
	err = mh.AddExt(reflect.TypeOf(ReceiveChannel{}), 2, h.encodeReceiveChannel, h.decodeReceiveChannel)
	if err != nil {
		panic(err)
	}

	return mh
}
