package spdy

import (
	"encoding/binary"
	"errors"
	"io"
	"reflect"
	"time"

	"github.com/dmcgowan/msgpack"
	"github.com/docker/libchan"
)

const (
	duplexStreamCode    = 1
	inboundStreamCode   = 2
	outboundStreamCode  = 3
	inboundChannelCode  = 4
	outboundChannelCode = 5
	timeCode            = 6
)

func decodeReferenceID(b []byte) (referenceID uint64, err error) {
	if len(b) == 8 {
		referenceID = binary.BigEndian.Uint64(b)
	} else if len(b) == 4 {
		referenceID = uint64(binary.BigEndian.Uint32(b))
	} else {
		err = errors.New("bad reference id")
	}
	return
}

func encodeReferenceID(b []byte, referenceID uint64) (n int) {
	if referenceID > 0xffffffff {
		binary.BigEndian.PutUint64(b, referenceID)
		n = 8
	} else {
		binary.BigEndian.PutUint32(b, uint32(referenceID))
		n = 4
	}
	return
}

func (s *stream) channelBytes() ([]byte, error) {
	buf := make([]byte, 8)
	written := encodeReferenceID(buf, s.referenceID)
	return buf[:written], nil
}

func (s *stream) copySendChannel(send libchan.Sender) (*nopSender, error) {
	recv, sendCopy, err := s.CreateNestedReceiver()
	if err != nil {
		return nil, err
	}
	// Start copying into sender
	go func() {
		libchan.Copy(send, recv)
		send.Close()
	}()
	return sendCopy.(*nopSender), nil
}

func (s *stream) copyReceiveChannel(recv libchan.Receiver) (*nopReceiver, error) {
	send, recvCopy, err := s.CreateNestedSender()
	if err != nil {
		return nil, err
	}
	// Start copying from receiver
	go func() {
		libchan.Copy(send, recv)
		send.Close()
	}()
	return recvCopy.(*nopReceiver), nil
}

func (s *stream) decodeStream(b []byte) (*stream, error) {
	referenceID, err := decodeReferenceID(b)
	if err != nil {
		return nil, err
	}

	gs := s.session.getStream(referenceID)
	if gs == nil {
		return nil, errors.New("stream does not exist")
	}

	return gs, nil
}

func (s *stream) decodeReceiver(v reflect.Value, b []byte) error {
	bs, err := s.decodeStream(b)
	if err != nil {
		return err
	}

	v.Set(reflect.ValueOf(&receiver{stream: bs}))

	return nil
}

func (s *stream) decodeSender(v reflect.Value, b []byte) error {
	bs, err := s.decodeStream(b)
	if err != nil {
		return err
	}

	v.Set(reflect.ValueOf(&sender{stream: bs}))

	return nil
}

func (s *stream) streamBytes() ([]byte, error) {
	var buf [8]byte
	written := encodeReferenceID(buf[:], s.referenceID)

	return buf[:written], nil
}

func (s *stream) decodeWStream(v reflect.Value, b []byte) error {
	bs, err := s.decodeStream(b)
	if err != nil {
		return err
	}

	v.Set(reflect.ValueOf(bs))

	return nil
}

func (s *stream) decodeRStream(v reflect.Value, b []byte) error {
	bs, err := s.decodeStream(b)
	if err != nil {
		return err
	}

	v.Set(reflect.ValueOf(bs))

	return nil
}

func encodeTime(t *time.Time) ([]byte, error) {
	var b [12]byte
	binary.BigEndian.PutUint64(b[0:8], uint64(t.Unix()))
	binary.BigEndian.PutUint32(b[8:12], uint32(t.Nanosecond()))
	return b[:], nil
}

func decodeTime(v reflect.Value, b []byte) error {
	if len(b) != 12 {
		return errors.New("Invalid length")
	}
	t := time.Unix(int64(binary.BigEndian.Uint64(b[0:8])), int64(binary.BigEndian.Uint32(b[8:12])))

	if v.Kind() == reflect.Ptr {
		v.Set(reflect.ValueOf(&t))
	} else {
		v.Set(reflect.ValueOf(t))
	}

	return nil
}

func (s *stream) encodeExtended(iv reflect.Value) (i int, b []byte, e error) {
	switch v := iv.Interface().(type) {
	case *nopSender:
		if v.stream == nil {
			return 0, nil, errors.New("bad type")
		}
		if v.stream.session != s.session {
			rc, err := s.copySendChannel(v)
			if err != nil {
				return 0, nil, err
			}
			b, err := rc.stream.channelBytes()
			return inboundChannelCode, b, err
		}

		b, err := v.stream.channelBytes()
		return inboundChannelCode, b, err
	case *nopReceiver:
		if v.stream == nil {
			return 0, nil, errors.New("bad type")
		}
		if v.stream.session != s.session {
			rc, err := s.copyReceiveChannel(v)
			if err != nil {
				return 0, nil, err
			}
			b, err := rc.stream.channelBytes()
			return outboundChannelCode, b, err
		}

		b, err := v.stream.channelBytes()
		return outboundChannelCode, b, err
	case *stream:
		if v.referenceID == 0 {
			return 0, nil, errors.New("bad type")
		}
		if v.session != s.session {
			streamCopy, err := s.createByteStream()
			if err != nil {
				return 0, nil, err
			}
			go func(r io.Reader) {
				io.Copy(streamCopy, r)
				streamCopy.Close()
			}(v)
			go func(w io.WriteCloser) {
				io.Copy(w, streamCopy)
				w.Close()
			}(v)
			v = streamCopy

		}
		b, err := v.channelBytes()
		return duplexStreamCode, b, err
	case libchan.Sender:
		copyCh, err := s.copySendChannel(v)
		if err != nil {
			return 0, nil, err
		}
		b, err := copyCh.stream.channelBytes()
		return inboundChannelCode, b, err
	case libchan.Receiver:
		copyCh, err := s.copyReceiveChannel(v)
		if err != nil {
			return 0, nil, err
		}
		b, err := copyCh.stream.channelBytes()
		return outboundChannelCode, b, err

	case io.Reader:
		// Either ReadWriteCloser, ReadWriter, or ReadCloser
		streamCopy, err := s.createByteStream()
		if err != nil {
			return 0, nil, err
		}
		go func() {
			io.Copy(streamCopy, v)
			streamCopy.Close()
		}()
		code := outboundStreamCode
		if wc, ok := v.(io.WriteCloser); ok {
			go func() {
				io.Copy(wc, streamCopy)
				wc.Close()
			}()
			code = duplexStreamCode
		} else if w, ok := v.(io.Writer); ok {
			go func() {
				io.Copy(w, streamCopy)
			}()
			code = duplexStreamCode
		}
		b, err := streamCopy.streamBytes()
		return code, b, err
	case io.Writer:
		streamCopy, err := s.createByteStream()
		if err != nil {
			return 0, nil, err
		}
		if wc, ok := v.(io.WriteCloser); ok {
			go func() {
				io.Copy(wc, streamCopy)
				wc.Close()
			}()
		} else {
			go func() {
				io.Copy(v, streamCopy)
			}()
		}

		b, err := streamCopy.streamBytes()
		return inboundStreamCode, b, err
	case *time.Time:
		b, err := encodeTime(v)
		return timeCode, b, err
	}
	return 0, nil, nil
}

func (s *stream) initializeExtensions() *msgpack.Extensions {
	exts := msgpack.NewExtensions()
	exts.SetEncoder(s.encodeExtended)
	exts.AddDecoder(duplexStreamCode, reflect.TypeOf(&stream{}), s.decodeWStream)
	exts.AddDecoder(inboundStreamCode, reflect.TypeOf(&stream{}), s.decodeWStream)
	exts.AddDecoder(outboundStreamCode, reflect.TypeOf(&stream{}), s.decodeRStream)
	exts.AddDecoder(inboundChannelCode, reflect.TypeOf(&sender{}), s.decodeSender)
	exts.AddDecoder(outboundChannelCode, reflect.TypeOf(&receiver{}), s.decodeReceiver)
	exts.AddDecoder(timeCode, reflect.TypeOf(&time.Time{}), decodeTime)
	return exts
}
