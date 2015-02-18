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
		session:     s,
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

// Wrappers to cue a copy of a previously decoded object

type pipeSenderWrapper struct{ *pipeSender }
type pipeReceiverWrapper struct{ *pipeReceiver }
type byteStreamWrapper struct{ *byteStream }

func (s *streamSession) decodeSenderWrapper(v reflect.Value, b []byte) error {
	ps := &pipeSender{}
	v.FieldByName("pipeSender").Set(reflect.ValueOf(ps))
	return s.decodeSender(reflect.ValueOf(ps).Elem(), b)
}

func (s *streamSession) decodeReceiverWrapper(v reflect.Value, b []byte) error {
	pr := &pipeReceiver{}
	v.FieldByName("pipeReceiver").Set(reflect.ValueOf(pr))
	return s.decodeReceiver(reflect.ValueOf(pr).Elem(), b)
}

func (s *streamSession) decodeByteStreamWrapper(v reflect.Value, b []byte) error {
	bs := &byteStream{}
	v.FieldByName("byteStream").Set(reflect.ValueOf(bs))
	return s.decodeStream(reflect.ValueOf(bs).Elem(), b)
}

// Internal definitions early, does not follow protocol because does not
// interact with external processes
func getMsgPackHandler(session *streamSession) *codec.MsgpackHandle {
	mh := &codec.MsgpackHandle{WriteExt: true}
	mh.RawToString = true

	err := mh.AddExt(reflect.TypeOf(pipeReceiverWrapper{}), 1, nil, session.decodeReceiverWrapper)
	if err != nil {
		panic(err)
	}

	err = mh.AddExt(reflect.TypeOf(pipeReceiver{}), 1, session.encodeReceiver, nil)
	if err != nil {
		panic(err)
	}

	err = mh.AddExt(reflect.TypeOf(pipeSenderWrapper{}), 2, nil, session.decodeSenderWrapper)
	if err != nil {
		panic(err)
	}

	err = mh.AddExt(reflect.TypeOf(pipeSender{}), 2, session.encodeSender, nil)
	if err != nil {
		panic(err)
	}

	err = mh.AddExt(reflect.TypeOf(byteStreamWrapper{}), 3, nil, session.decodeByteStreamWrapper)
	if err != nil {
		panic(err)
	}

	err = mh.AddExt(reflect.TypeOf(byteStream{}), 3, session.encodeStream, nil)
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
	mCopy, mErr := w.copyMessage(message)
	if mErr != nil {
		return mErr
	}
	return w.encoder.Encode(mCopy)
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

func (w *pipeSender) copyMessage(message interface{}) (interface{}, error) {
	mapCopy, mapOk := message.(map[string]interface{})
	if mapOk {
		return w.copyChannelMessage(mapCopy)
	}
	return w.copyValue(message)
}

func (w *pipeSender) copyValue(v interface{}) (interface{}, error) {
	switch val := v.(type) {
	case *byteStream:
		if val.session != w.session {
			return w.copyByteStream(val)
		}
	case *pipeSender:
		if val.session != w.session {
			return w.copySender(val)
		}
	case *pipeReceiver:
		if val.session != w.session {
			return w.copyReceiver(val)
		}
	case io.ReadWriteCloser:
		return w.copyByteStream(val)
	case io.ReadCloser:
		return w.copyByteReadStream(val)
	case io.WriteCloser:
		return w.copyByteWriteStream(val)
	case Sender:
		return w.copySender(val)
	case Receiver:
		return w.copyReceiver(val)
	case map[string]interface{}:
		return w.copyChannelMessage(val)
	case map[interface{}]interface{}:
		return w.copyChannelInterfaceMessage(val)
	default:
		if rv := reflect.ValueOf(v); rv.Kind() == reflect.Ptr {
			if rv.Elem().Kind() == reflect.Struct {
				return w.copyStructValue(rv.Elem())
			}
		} else if rv.Kind() == reflect.Struct {
			return w.copyStructValue(rv)
		}
	}
	return v, nil
}

func (w *pipeSender) copySender(val Sender) (Sender, error) {
	recv, send, err := w.CreateNestedReceiver()
	if err != nil {
		return nil, err
	}
	go func() {
		Copy(val, recv)
		val.Close()
	}()
	return send, nil
}

func (w *pipeSender) copyReceiver(val Receiver) (Receiver, error) {
	send, recv, err := w.CreateNestedSender()
	if err != nil {
		return nil, err
	}
	go func() {
		Copy(send, val)
		send.Close()
	}()
	return recv, nil
}

func (w *pipeSender) copyByteStream(stream io.ReadWriteCloser) (io.ReadWriteCloser, error) {
	streamCopy, err := w.session.newByteStream()
	if err != nil {
		return nil, err
	}
	go func() {
		io.Copy(streamCopy, stream)
		streamCopy.Close()
	}()
	go func() {
		io.Copy(stream, streamCopy)
		stream.Close()
	}()
	return streamCopy, nil
}

func (w *pipeSender) copyByteReadStream(stream io.ReadCloser) (io.ReadCloser, error) {
	streamCopy, err := w.session.newByteStream()
	if err != nil {
		return nil, err
	}
	go func() {
		io.Copy(streamCopy, stream)
		streamCopy.Close()
		stream.Close()
	}()
	return streamCopy, nil
}

func (w *pipeSender) copyByteWriteStream(stream io.WriteCloser) (io.WriteCloser, error) {
	streamCopy, err := w.session.newByteStream()
	if err != nil {
		return nil, err
	}
	go func() {
		io.Copy(stream, streamCopy)
		stream.Close()
	}()
	return streamCopy, nil
}

func (w *pipeSender) copyChannelMessage(m map[string]interface{}) (interface{}, error) {
	mCopy := make(map[string]interface{})
	for k, v := range m {
		vCopy, vErr := w.copyValue(v)
		if vErr != nil {
			return nil, vErr
		}
		mCopy[k] = vCopy
	}

	return mCopy, nil
}

func (w *pipeSender) copyChannelInterfaceMessage(m map[interface{}]interface{}) (interface{}, error) {
	mCopy := make(map[string]interface{})
	for k, v := range m {
		vCopy, vErr := w.copyValue(v)
		if vErr != nil {
			return nil, vErr
		}
		keyStr, ok := k.(string)
		if !ok {
			return nil, errors.New("invalid non string key")
		}
		mCopy[keyStr] = vCopy
	}

	return mCopy, nil
}

func (w *pipeSender) copyStructure(m interface{}) (interface{}, error) {
	v := reflect.ValueOf(m)
	if v.Kind() == reflect.Ptr {
		v = v.Elem()
	}
	if v.Kind() != reflect.Struct {
		return nil, errors.New("invalid non struct type")
	}
	return w.copyStructValue(v)
}

func (w *pipeSender) copyStructValue(v reflect.Value) (interface{}, error) {
	mCopy := make(map[string]interface{})
	t := v.Type()
	for i := 0; i < v.NumField(); i++ {
		vCopy, vErr := w.copyValue(v.Field(i).Interface())
		if vErr != nil {
			return nil, vErr
		}
		mCopy[t.Field(i).Name] = vCopy
	}
	return mCopy, nil
}
