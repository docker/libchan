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

// Pipe returns an inmemory Sender/Receiver pair.
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
	referenceID   uint64
	byteStreams   map[uint64]*byteStream
}

func createStreamSession() *streamSession {
	session := &streamSession{
		pipeReaders: make(map[uint64]*io.PipeReader),
		pipeWriters: make(map[uint64]*io.PipeWriter),
		referenceID: 2,
		byteStreams: make(map[uint64]*byteStream),
	}
	session.handler = getMsgPackHandler(session)
	return session
}

func (s *streamSession) createPipe() (Receiver, Sender) {
	r, w := io.Pipe()
	s.pipeLock.Lock()
	pipeID := s.pipeCount + 1
	s.pipeCount = pipeID
	s.pipeReaders[pipeID] = r
	s.pipeWriters[pipeID] = w
	s.pipeLock.Unlock()

	recv := &pipeReceiver{pipeID, s, r, codec.NewDecoder(r, s.handler)}
	send := &pipeSender{pipeID, s, w, codec.NewEncoder(w, s.handler)}
	return recv, send
}

func (s *streamSession) newByteStream() (io.ReadWriteCloser, error) {
	c1, c2 := net.Pipe()
	bs := &byteStream{
		Conn:        c1,
		referenceID: s.referenceID,
	}
	s.referenceLock.Lock()
	s.byteStreams[s.referenceID] = bs
	s.byteStreams[s.referenceID+1] = &byteStream{
		Conn:        c2,
		referenceID: s.referenceID + 1,
		session:     s,
	}
	s.referenceID = s.referenceID + 2
	s.referenceLock.Unlock()

	return bs, nil
}

func (s *streamSession) encodeReceiver(v reflect.Value) ([]byte, error) {
	bs := v.Interface().(pipeReceiver)
	if bs.pipeID == 0 {
		return nil, errors.New("bad type")
	}
	var buf [8]byte
	written := binary.PutUvarint(buf[:], uint64(bs.pipeID))

	return buf[:written], nil
}

func (s *streamSession) decodeReceiver(v reflect.Value, b []byte) error {
	pipeID, readN := binary.Uvarint(b)
	if readN == 0 {
		return errors.New("bad reference id")
	}

	r, ok := s.pipeReaders[pipeID]
	if !ok {
		return errors.New("receiver does not exist")
	}

	v.Set(reflect.ValueOf(pipeReceiver{pipeID, s, r, codec.NewDecoder(r, s.handler)}))

	return nil
}

func (s *streamSession) encodeSender(v reflect.Value) ([]byte, error) {
	sender := v.Interface().(pipeSender)
	if sender.pipeID == 0 {
		return nil, errors.New("bad type")
	}
	var buf [8]byte
	written := binary.PutUvarint(buf[:], uint64(sender.pipeID))

	return buf[:written], nil
}

func (s *streamSession) decodeSender(v reflect.Value, b []byte) error {
	pipeID, readN := binary.Uvarint(b)
	if readN == 0 {
		return errors.New("bad reference id")
	}

	w, ok := s.pipeWriters[pipeID]
	if !ok {
		return errors.New("receiver does not exist")
	}

	v.Set(reflect.ValueOf(pipeSender{pipeID, s, w, codec.NewEncoder(w, s.handler)}))

	return nil
}

func (s *streamSession) encodeStream(v reflect.Value) ([]byte, error) {
	bs := v.Interface().(byteStream)
	if bs.referenceID == 0 {
		return nil, errors.New("bad type")
	}
	var buf [8]byte
	written := binary.PutUvarint(buf[:], uint64(bs.referenceID)^0x01)

	return buf[:written], nil
}

func (s *streamSession) decodeStream(v reflect.Value, b []byte) error {
	referenceID, readN := binary.Uvarint(b)
	if readN == 0 {
		return errors.New("bad reference id")
	}

	bs, ok := s.byteStreams[referenceID]
	if !ok {
		return errors.New("byte stream does not exist")
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
	pipeID  uint64
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
	pipeID  uint64
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
	referenceID uint64
	session     *streamSession
}
