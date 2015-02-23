package spdy

import (
	"encoding/binary"
	"errors"
	"io"
	"reflect"

	"github.com/dmcgowan/msgpack"
	"github.com/docker/libchan"
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

func (c *channel) channelBytes() ([]byte, error) {
	buf := make([]byte, 9)
	if c.direction == inbound {
		buf[0] = 0x02 // Reverse direction
	} else if c.direction == outbound {
		buf[0] = 0x01 // Reverse direction
	} else {
		return nil, errors.New("invalid direction")
	}
	written := encodeReferenceID(buf[1:], c.referenceID)
	return buf[:(written + 1)], nil
}

func (c *channel) copySendChannel(send libchan.Sender) (*channel, error) {
	recv, sendCopy, err := c.CreateNestedReceiver()
	if err != nil {
		return nil, err
	}
	// Start copying into sender
	go func() {
		libchan.Copy(send, recv)
		send.Close()
	}()
	return sendCopy.(*channel), nil
}

func (c *channel) copyReceiveChannel(recv libchan.Receiver) (*channel, error) {
	send, recvCopy, err := c.CreateNestedSender()
	if err != nil {
		return nil, err
	}
	// Start copying from receiver
	go func() {
		libchan.Copy(send, recv)
		send.Close()
	}()
	return recvCopy.(*channel), nil
}

func (c *channel) decodeChannel(v reflect.Value, b []byte) error {
	var d direction
	if b[0] == 0x01 {
		d = inbound
	} else if b[0] == 0x02 {
		d = outbound
	} else {
		return errors.New("unexpected direction")
	}
	referenceID, err := decodeReferenceID(b[1:])
	if err != nil {
		return err
	}

	ch := c.session.getChannel(referenceID)
	if ch == nil {
		return errors.New("channel does not exist")
	}
	// TODO lock channel while check and setting
	if ch.direction != 0 && ch.direction != d {
		return ErrWrongDirection
	}
	ch.direction = d

	v.Set(reflect.ValueOf(ch))

	return nil
}

func (b *byteStream) streamBytes() ([]byte, error) {
	var buf [8]byte
	written := encodeReferenceID(buf[:], b.referenceID)

	return buf[:written], nil
}

func (c *channel) encodeExtension(iv reflect.Value) (int, []byte, error) {
	switch v := iv.Interface().(type) {
	case *byteStream:
		if v.referenceID == 0 {
			return 0, nil, errors.New("bad type")
		}
		if v.session != c.session {
			streamCopy, err := c.session.createByteStream()
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
			v = streamCopy.(*byteStream)

		}
		b, err := v.streamBytes()
		return 2, b, err
	case io.Reader:
		// Either ReadWriteCloser, ReadWriter, or ReadCloser
		streamCopy, err := c.session.createByteStream()
		if err != nil {
			return 0, nil, err
		}
		go func() {
			io.Copy(streamCopy, v)
			streamCopy.Close()
		}()
		if wc, ok := v.(io.WriteCloser); ok {
			go func() {
				io.Copy(wc, streamCopy)
				wc.Close()
			}()
		} else if w, ok := v.(io.Writer); ok {
			go func() {
				io.Copy(w, streamCopy)
			}()
		}
		b, err := streamCopy.(*byteStream).streamBytes()
		return 2, b, err
	case io.Writer:
		streamCopy, err := c.session.createByteStream()
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
		b, err := streamCopy.(*byteStream).streamBytes()
		return 2, b, err
	case *channel:
		if v.stream == nil {
			return 0, nil, errors.New("bad type")
		}
		if v.session != c.session {
			var rc *channel
			var err error
			if c.direction == inbound {
				rc, err = c.copyReceiveChannel(v)
			} else {
				rc, err = c.copySendChannel(v)
			}
			if err != nil {
				return 0, nil, err
			}
			b, err := rc.channelBytes()
			return 1, b, err
		}
		b, err := v.channelBytes()
		return 1, b, err
	case libchan.Sender:
		copyCh, err := c.copySendChannel(v)
		if err != nil {
			return 0, nil, err
		}
		b, err := copyCh.channelBytes()
		return 1, b, err
	case libchan.Receiver:
		copyCh, err := c.copyReceiveChannel(v)
		if err != nil {
			return 0, nil, err
		}
		b, err := copyCh.channelBytes()
		return 1, b, err
	}
	return 0, nil, nil
}

func (s *Transport) decodeStream(v reflect.Value, b []byte) error {
	referenceID, err := decodeReferenceID(b)
	if err != nil {
		return err
	}

	bs := s.getByteStream(referenceID)
	if bs != nil {
		v.Set(reflect.ValueOf(bs))
	}

	return nil
}

func (c *channel) initializeExtensions() *msgpack.Extensions {
	exts := msgpack.NewExtensions()
	exts.SetEncoder(c.encodeExtension)
	exts.AddDecoder(1, reflect.TypeOf(&channel{}), c.decodeChannel)
	exts.AddDecoder(2, reflect.TypeOf(&byteStream{}), c.session.decodeStream)
	return exts
}
