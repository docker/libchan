package http2

import (
	"encoding/binary"
	"errors"
	"github.com/ugorji/go/codec"
	"reflect"

	"github.com/docker/libchan"
)

type sessionHandler struct {
	session *Session
}

func (h *sessionHandler) encodeChannel(v reflect.Value) ([]byte, error) {
	rc := v.Interface().(Channel)
	if rc.stream == nil {
		return nil, errors.New("bad type")
	}
	if rc.session != h.session {
		return nil, errors.New("cannot decode channel from different session")
	}

	// Get stream identifier?
	streamId := rc.stream.Identifier()
	var buf [9]byte
	if rc.direction == libchan.In {
		buf[0] = 0x02 // Reverse direction
	} else if rc.direction == libchan.Out {
		buf[0] = 0x01 // Reverse direction
	} else {
		return nil, errors.New("Invalid direction")
	}
	written := binary.PutUvarint(buf[1:], uint64(streamId))
	if written > 4 {
		return nil, errors.New("wrote unexpected stream id size")
	}
	return buf[:(written + 1)], nil
}

func (h *sessionHandler) decodeChannel(v reflect.Value, b []byte) error {
	rc := v.Interface().(Channel)

	if b[0] == 0x01 {
		rc.direction = libchan.In
	} else if b[0] == 0x02 {
		rc.direction = libchan.Out
	} else {
		return errors.New("unexpected direction")
	}

	streamId, readN := binary.Uvarint(b[1:])
	if readN > 4 {
		return errors.New("read unexpected stream id size")
	}
	stream := h.session.conn.FindStream(uint32(streamId))
	if stream == nil {
		return errors.New("stream does not exist")
	}
	rc.session = h.session
	rc.stream = stream
	v.Set(reflect.ValueOf(rc))

	return nil
}

func (h *sessionHandler) encodeStream(v reflect.Value) ([]byte, error) {
	bs := v.Interface().(libchan.ByteStream)
	if bs.ReferenceId == 0 {
		return nil, errors.New("bad type")
	}
	var buf [8]byte
	written := binary.PutUvarint(buf[:], uint64(bs.ReferenceId))

	return buf[:written], nil
}

func (h *sessionHandler) decodeStream(v reflect.Value, b []byte) error {
	referenceId, readN := binary.Uvarint(b)
	if readN == 0 {
		return errors.New("bad reference id")
	}

	bs := h.session.GetByteStream(libchan.ReferenceId(referenceId))
	if bs != nil {
		v.Set(reflect.ValueOf(*bs))
	}

	return nil
}

func getMsgPackHandler(session *Session) *codec.MsgpackHandle {
	h := &sessionHandler{session: session}
	mh := &codec.MsgpackHandle{WriteExt: true}
	err := mh.AddExt(reflect.TypeOf(Channel{}), 1, h.encodeChannel, h.decodeChannel)
	if err != nil {
		panic(err)
	}

	err = mh.AddExt(reflect.TypeOf(libchan.ByteStream{}), 2, h.encodeStream, h.decodeStream)
	if err != nil {
		panic(err)
	}

	return mh
}
