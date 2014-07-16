package libchan

import (
	"encoding/binary"
	"errors"
	"io"
	"net"
	"reflect"
	"sync"

	"github.com/dmcgowan/go/codec"
)

func Pipe() (Receiver, Sender) {
	session := createStreamSession()
	return session.createPipe()
}

type streamSession struct {
	pipeLock    sync.Mutex
	pipeCount   uint64
	pipeReaders map[uint64]*io.PipeReader
	pipeWriters map[uint64]*io.PipeWriter

	handler codec.Handle

	referenceLock sync.Mutex
	referenceId   uint64
	byteStreams   map[uint64]*byteStream
}

func createStreamSession() *streamSession {
	session := &streamSession{
		pipeReaders: make(map[uint64]*io.PipeReader),
		pipeWriters: make(map[uint64]*io.PipeWriter),
		referenceId: 2,
		byteStreams: make(map[uint64]*byteStream),
	}
	session.handler = getMsgPackHandler(session)
	return session
}

func (s *streamSession) createPipe() (Receiver, Sender) {
	r, w := io.Pipe()
	s.pipeLock.Lock()
	pipeId := s.pipeCount + 1
	s.pipeCount = pipeId
	s.pipeReaders[pipeId] = r
	s.pipeWriters[pipeId] = w
	s.pipeLock.Unlock()

	recv := &pipeReceiver{pipeId, s, r, codec.NewDecoder(r, s.handler)}
	send := &pipeSender{pipeId, s, w, codec.NewEncoder(w, s.handler)}
	return recv, send
}

func (s *streamSession) newByteStream() (io.ReadWriteCloser, error) {
	c1, c2 := net.Pipe()
	bs := &byteStream{
		Conn:        c1,
		referenceId: s.referenceId,
	}
	s.referenceLock.Lock()
	s.byteStreams[s.referenceId] = bs
	s.byteStreams[s.referenceId+1] = &byteStream{
		Conn:        c2,
		referenceId: s.referenceId + 1,
		session:     s,
	}
	s.referenceId = s.referenceId + 2
	s.referenceLock.Unlock()

	return bs, nil
}

func (s *streamSession) encodeReceiver(v reflect.Value) ([]byte, error) {
	bs := v.Interface().(pipeReceiver)
	if bs.pipeId == 0 {
		return nil, errors.New("bad type")
	}
	var buf [8]byte
	written := binary.PutUvarint(buf[:], uint64(bs.pipeId))

	return buf[:written], nil
}

func (s *streamSession) decodeReceiver(v reflect.Value, b []byte) error {
	pipeId, readN := binary.Uvarint(b)
	if readN == 0 {
		return errors.New("bad reference id")
	}

	r, ok := s.pipeReaders[pipeId]
	if !ok {
		return errors.New("Receiver does not exist")
	}

	v.Set(reflect.ValueOf(pipeReceiver{pipeId, s, r, codec.NewDecoder(r, s.handler)}))

	return nil
}

func (s *streamSession) encodeSender(v reflect.Value) ([]byte, error) {
	sender := v.Interface().(pipeSender)
	if sender.pipeId == 0 {
		return nil, errors.New("bad type")
	}
	var buf [8]byte
	written := binary.PutUvarint(buf[:], uint64(sender.pipeId))

	return buf[:written], nil
}

func (s *streamSession) decodeSender(v reflect.Value, b []byte) error {
	pipeId, readN := binary.Uvarint(b)
	if readN == 0 {
		return errors.New("bad reference id")
	}

	w, ok := s.pipeWriters[pipeId]
	if !ok {
		return errors.New("Receiver does not exist")
	}

	v.Set(reflect.ValueOf(pipeSender{pipeId, s, w, codec.NewEncoder(w, s.handler)}))

	return nil
}

func (s *streamSession) encodeStream(v reflect.Value) ([]byte, error) {
	bs := v.Interface().(byteStream)
	if bs.referenceId == 0 {
		return nil, errors.New("bad type")
	}
	var buf [8]byte
	written := binary.PutUvarint(buf[:], uint64(bs.referenceId)^0x01)

	return buf[:written], nil
}

func (s *streamSession) decodeStream(v reflect.Value, b []byte) error {
	referenceId, readN := binary.Uvarint(b)
	if readN == 0 {
		return errors.New("bad reference id")
	}

	bs, ok := s.byteStreams[referenceId]
	if !ok {
		return errors.New("Byte stream does not exist")
	}

	if bs != nil {
		v.Set(reflect.ValueOf(*bs))
	}

	return nil
}

func (s *streamSession) encodeWrapper(v reflect.Value) ([]byte, error) {
	wrapper := v.Interface().(ByteStreamWrapper)
	bs, err := s.newByteStream()
	if err != nil {
		return nil, err
	}

	go func() {
		io.Copy(bs, wrapper)
		bs.Close()
	}()

	go func() {
		io.Copy(wrapper, bs)
		wrapper.Close()
	}()

	return s.encodeStream(reflect.ValueOf(bs).Elem())
}

func (s *streamSession) decodeWrapper(v reflect.Value, b []byte) error {
	bs := &byteStream{}
	s.decodeStream(reflect.ValueOf(bs).Elem(), b)
	v.FieldByName("ReadWriteCloser").Set(reflect.ValueOf(bs))
	return nil
}

func getMsgPackHandler(session *streamSession) *codec.MsgpackHandle {
	mh := &codec.MsgpackHandle{WriteExt: true}
	mh.RawToString = true

	err := mh.AddExt(reflect.TypeOf(pipeReceiver{}), 1, session.encodeReceiver, session.decodeReceiver)
	if err != nil {
		panic(err)
	}

	err = mh.AddExt(reflect.TypeOf(pipeSender{}), 2, session.encodeSender, session.decodeSender)
	if err != nil {
		panic(err)
	}

	err = mh.AddExt(reflect.TypeOf(byteStream{}), 3, session.encodeStream, session.decodeStream)
	if err != nil {
		panic(err)
	}

	err = mh.AddExt(reflect.TypeOf(ByteStreamWrapper{}), 4, session.encodeWrapper, session.decodeWrapper)
	if err != nil {
		panic(err)
	}

	return mh
}

type pipeSender struct {
	pipeId  uint64
	session *streamSession
	p       *io.PipeWriter
	encoder *codec.Encoder
}

func (w *pipeSender) Send(message interface{}) error {
	return w.encoder.Encode(message)
}

func (w *pipeSender) Close() error {
	return w.p.Close()
}

func (w *pipeSender) CreateByteStream() (io.ReadWriteCloser, error) {
	return w.session.newByteStream()
}

func (w *pipeSender) CreateNestedReceiver() (Receiver, Sender, error) {
	recv, send := w.session.createPipe()
	return recv, send, nil

}

func (w *pipeSender) CreateNestedSender() (Sender, Receiver, error) {
	recv, send := w.session.createPipe()
	return send, recv, nil
}

type pipeReceiver struct {
	pipeId  uint64
	session *streamSession
	p       *io.PipeReader
	decoder *codec.Decoder
}

func (r *pipeReceiver) Receive(message interface{}) error {
	return r.decoder.Decode(message)
}

func (r *pipeReceiver) Close() error {
	return r.p.Close()
}

type byteStream struct {
	net.Conn
	referenceId uint64
	session     *streamSession
}
