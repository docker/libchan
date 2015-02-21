package spdy

import (
	"encoding/binary"
	"errors"
	"io"
	"reflect"
	"time"

	"github.com/dmcgowan/msgpack"
	"github.com/docker/libchan"
	"github.com/docker/libchan/encoding"
)

const (
	duplexStreamCode    = 1
	inboundStreamCode   = 2
	outboundStreamCode  = 3
	inboundChannelCode  = 4
	outboundChannelCode = 5
	timeCode            = 6
)

type cproducer struct {
	encoding.ChanProducer
}

type creceiver struct {
	encoding.ChanReceiver
}

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

func encodeReferenceID(referenceID uint64) []byte {
	if referenceID > 0xffffffff {
		buf := make([]byte, 8)
		binary.BigEndian.PutUint64(buf, referenceID)
		return buf
	}
	buf := make([]byte, 4)
	binary.BigEndian.PutUint32(buf, uint32(referenceID))
	return buf
}

func (p *cproducer) copySendChannel(send libchan.Sender) (uint64, error) {
	recv, copyID, err := p.CreateReceiver()
	if err != nil {
		return 0, err
	}
	// Start copying into sender
	go func() {
		libchan.Copy(send, recv)
		send.Close()
	}()
	return copyID, nil
}

func (p *cproducer) copyReceiveChannel(recv libchan.Receiver) (uint64, error) {
	send, copyID, err := p.CreateSender()
	if err != nil {
		return 0, err
	}
	// Start copying from receiver
	go func() {
		libchan.Copy(send, recv)
		send.Close()
	}()
	return copyID, nil
}

func (r *creceiver) decodeStream(b []byte) (io.ReadWriteCloser, error) {
	referenceID, err := decodeReferenceID(b)
	if err != nil {
		return nil, err
	}

	return r.GetStream(referenceID)
}

func (r *creceiver) decodeReceiver(v reflect.Value, b []byte) error {
	referenceID, err := decodeReferenceID(b)
	if err != nil {
		return err
	}

	recv, err := r.GetReceiver(referenceID)
	if err != nil {
		return err
	}

	v.Set(reflect.ValueOf(recv))

	return nil
}

func (r *creceiver) decodeSender(v reflect.Value, b []byte) error {
	referenceID, err := decodeReferenceID(b)
	if err != nil {
		return err
	}

	send, err := r.GetSender(referenceID)
	if err != nil {
		return err
	}

	v.Set(reflect.ValueOf(send))

	return nil
}

func (r *creceiver) decodeWStream(v reflect.Value, b []byte) error {
	bs, err := r.decodeStream(b)
	if err != nil {
		return err
	}

	v.Set(reflect.ValueOf(bs))

	return nil
}

func (r *creceiver) decodeRStream(v reflect.Value, b []byte) error {
	bs, err := r.decodeStream(b)
	if err != nil {
		return err
	}

	v.Set(reflect.ValueOf(bs))

	return bs.Close()
}

func entimeCode(t *time.Time) ([]byte, error) {
	var b [12]byte
	binary.BigEndian.PutUint64(b[0:8], uint64(t.Unix()))
	binary.BigEndian.PutUint32(b[8:12], uint32(t.Nanosecond()))
	return b[:], nil
}

func detimeCode(v reflect.Value, b []byte) error {
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

func (p *cproducer) encodeExtended(iv reflect.Value) (i int, b []byte, e error) {
	switch v := iv.Interface().(type) {
	case libchan.Sender:
		copyCh, err := p.copySendChannel(v)
		if err != nil {
			return 0, nil, err
		}
		return inboundChannelCode, encodeReferenceID(copyCh), nil
	case libchan.Receiver:
		copyCh, err := p.copyReceiveChannel(v)
		if err != nil {
			return 0, nil, err
		}
		return outboundChannelCode, encodeReferenceID(copyCh), nil

	case io.Reader:
		// Either ReadWriteCloser, ReadWriter, or ReadCloser
		streamCopy, copyID, err := p.CreateStream()
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
		return code, encodeReferenceID(copyID), nil
	case io.Writer:
		streamCopy, copyID, err := p.CreateStream()
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
		return inboundStreamCode, encodeReferenceID(copyID), nil
	case *time.Time:
		b, err := entimeCode(v)
		return timeCode, b, err
	}
	return 0, nil, nil
}

// MsgpackCodec implements the libchan encoding using msgpack5.
type MsgpackCodec struct{}

// NewEncoder returns a libchan encoder which encodes given objects
// to msgpack5 on the given datastream using the given encoding
// channel producer.
func (codec *MsgpackCodec) NewEncoder(w io.Writer, p encoding.ChanProducer) encoding.Encoder {
	prd := &cproducer{p}
	encoder := msgpack.NewEncoder(w)
	exts := msgpack.NewExtensions()
	exts.SetEncoder(prd.encodeExtended)
	encoder.AddExtensions(exts)
	return encoder
}

// NewDecoder returns a libchan decoder which decodes objects from
// the given data stream from msgpack5 into provided object using
// the provided types for libchan interfaces.
func (codec *MsgpackCodec) NewDecoder(r io.Reader, recv encoding.ChanReceiver, streamT, recvT, sendT reflect.Type) encoding.Decoder {
	rec := &creceiver{recv}
	decoder := msgpack.NewDecoder(r)
	exts := msgpack.NewExtensions()
	exts.AddDecoder(duplexStreamCode, streamT, rec.decodeWStream)
	exts.AddDecoder(inboundStreamCode, streamT, rec.decodeWStream)
	exts.AddDecoder(outboundStreamCode, streamT, rec.decodeRStream)
	exts.AddDecoder(inboundChannelCode, sendT, rec.decodeSender)
	exts.AddDecoder(outboundChannelCode, recvT, rec.decodeReceiver)
	exts.AddDecoder(timeCode, reflect.TypeOf(&time.Time{}), detimeCode)
	decoder.AddExtensions(exts)
	return decoder
}

// NewRawMessage returns a transit object which will copy a
// msgpack5 datastream and allow decoding that object
// using a Decoder from the codec object.
func (codec *MsgpackCodec) NewRawMessage() encoding.Decoder {
	return new(msgpack.RawMessage)
}
