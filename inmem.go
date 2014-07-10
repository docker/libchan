package libchan

import (
	"encoding/binary"
	"errors"
	"github.com/ugorji/go/codec"
	"io"
	"reflect"
	"sync"
)

type InMemSession struct {
	receiveChan chan uint64
	pipeLock    sync.Mutex
	pipeId      uint64
	inPipes     map[uint64]*io.PipeReader
	outPipes    map[uint64]*io.PipeWriter

	handler codec.Handle

	referenceLock sync.Mutex
	referenceId   ReferenceId
	byteStreams   map[ReferenceId]*ByteStream
}

func NewInMemSession() *InMemSession {
	session := &InMemSession{
		receiveChan: make(chan uint64),
		inPipes:     make(map[uint64]*io.PipeReader),
		outPipes:    make(map[uint64]*io.PipeWriter),
		pipeId:      1,
		referenceId: 2,
		byteStreams: make(map[ReferenceId]*ByteStream),
	}
	session.handler = getMsgPackHandler(session)

	return session
}

func (s *InMemSession) createPipe() (uint64, *io.PipeReader, *io.PipeWriter) {
	in, out := io.Pipe()
	s.pipeLock.Lock()
	id := s.pipeId
	s.pipeId = id + 1
	s.inPipes[id] = in
	s.outPipes[id] = out
	s.pipeLock.Unlock()
	return id, in, out
}

func (s *InMemSession) RegisterListener(listener ByteStreamListener) {
	// Not supported
}

func (s *InMemSession) NewSendChannel() Channel {
	id, _, out := s.createPipe()

	s.receiveChan <- id

	return &InMemChannel{
		id:      id,
		session: s,
		out:     out,
		encoder: codec.NewEncoder(out, s.handler),
	}
}

func (s *InMemSession) WaitReceiveChannel() Channel {
	id := <-s.receiveChan

	in, ok := s.inPipes[id]
	if !ok {
		return nil
	}

	return &InMemChannel{
		id:      id,
		session: s,
		in:      in,
		decoder: codec.NewDecoder(in, s.handler),
	}
}

type InMemChannel struct {
	id      uint64
	session *InMemSession

	in      *io.PipeReader
	decoder *codec.Decoder

	out     *io.PipeWriter
	encoder *codec.Encoder
}

func (c *InMemChannel) CreateByteStream(provider ByteStreamDialer) (*ByteStream, error) {
	if provider != nil {
		return nil, errors.New("Byte stream providers not supported")
	}
	in1, out1 := io.Pipe()
	in2, out2 := io.Pipe()
	rw1 := &biDirectionalPipe{in1, out2}
	rw2 := &biDirectionalPipe{in2, out1}
	bs := &ByteStream{
		ReferenceId: c.session.referenceId,
		Stream:      rw1,
	}

	c.session.referenceLock.Lock()
	c.session.byteStreams[c.session.referenceId] = bs
	c.session.byteStreams[c.session.referenceId+1] = &ByteStream{
		ReferenceId: c.session.referenceId + 1,
		Stream:      rw2,
	}
	c.session.referenceId = c.session.referenceId + 2
	c.session.referenceLock.Unlock()

	return bs, nil
}

func (c *InMemChannel) CreateSubChannel(direction Direction) (Channel, error) {
	id, in, out := c.session.createPipe()
	if direction == In {
		return &InMemChannel{
			id:      id,
			session: c.session,
			in:      in,
			decoder: codec.NewDecoder(in, c.session.handler),
		}, nil
	}
	return &InMemChannel{
		id:      id,
		session: c.session,
		out:     out,
		encoder: codec.NewEncoder(out, c.session.handler),
	}, nil
}

func (c *InMemChannel) Communicate(message interface{}) error {
	if c.in != nil {
		return c.decoder.Decode(message)
	} else {
		return c.encoder.Encode(message)
	}
}

func (c *InMemChannel) Close() error {
	if c.in != nil {
		return c.in.Close()
	} else {
		return c.out.Close()
	}
}

func (c *InMemChannel) Direction() Direction {
	if c.in != nil {
		return In
	} else {
		return Out
	}
}

type sessionHandler struct {
	session *InMemSession
}

func (h *sessionHandler) encodeChannel(v reflect.Value) ([]byte, error) {
	rc := v.Interface().(InMemChannel)
	if rc.session != h.session {
		return nil, errors.New("cannot decode channel from different session")
	}

	var buf [9]byte
	if rc.in != nil {
		buf[0] = 0x02 // Reverse direction
	} else if rc.out != nil {
		buf[0] = 0x01 // Reverse direction
	} else {
		return nil, errors.New("Invalid direction")
	}
	written := binary.PutUvarint(buf[1:], rc.id)
	return buf[:(written + 1)], nil
}

func (h *sessionHandler) decodeChannel(v reflect.Value, b []byte) error {
	rc := v.Interface().(InMemChannel)

	id, _ := binary.Uvarint(b[1:])

	if b[0] == 0x01 {
		in, ok := h.session.inPipes[id]
		if !ok {
			return errors.New("missing pipe")
		}
		rc.in = in
		rc.decoder = codec.NewDecoder(in, h.session.handler)
	} else if b[0] == 0x02 {
		out, ok := h.session.outPipes[id]
		if !ok {
			return errors.New("missing pipe")
		}
		rc.out = out
		rc.encoder = codec.NewEncoder(out, h.session.handler)
	} else {
		return errors.New("unexpected direction")
	}

	rc.id = id
	rc.session = h.session

	v.Set(reflect.ValueOf(rc))

	return nil
}

func (h *sessionHandler) encodeStream(v reflect.Value) ([]byte, error) {
	bs := v.Interface().(ByteStream)
	if bs.ReferenceId == 0 {
		return nil, errors.New("bad type")
	}
	var buf [8]byte
	written := binary.PutUvarint(buf[:], uint64(bs.ReferenceId)^0x01)

	return buf[:written], nil
}

func (h *sessionHandler) decodeStream(v reflect.Value, b []byte) error {
	referenceId, readN := binary.Uvarint(b)
	if readN == 0 {
		return errors.New("bad reference id")
	}

	bs, ok := h.session.byteStreams[ReferenceId(referenceId)]
	if !ok {
		return errors.New("Byte stream does not exist")
	}

	if bs != nil {
		v.Set(reflect.ValueOf(*bs))
	}

	return nil
}

func getMsgPackHandler(session *InMemSession) *codec.MsgpackHandle {
	h := &sessionHandler{session: session}
	mh := &codec.MsgpackHandle{WriteExt: true}
	err := mh.AddExt(reflect.TypeOf(InMemChannel{}), 1, h.encodeChannel, h.decodeChannel)
	if err != nil {
		panic(err)
	}

	err = mh.AddExt(reflect.TypeOf(ByteStream{}), 2, h.encodeStream, h.decodeStream)
	if err != nil {
		panic(err)
	}

	return mh
}

type biDirectionalPipe struct {
	r io.Reader
	w io.WriteCloser
}

func (p *biDirectionalPipe) Read(b []byte) (int, error) {
	return p.r.Read(b)
}

func (p *biDirectionalPipe) Write(b []byte) (int, error) {
	return p.w.Write(b)
}

func (p *biDirectionalPipe) Close() error {
	return p.w.Close()
}

func Pipe() (*PipeReceiver, *PipeSender) {
	p := new(pipe)
	p.rwait.L = &p.l
	p.wwait.L = &p.l
	r := &PipeReceiver{p}
	w := &PipeSender{p}
	return r, w
}

type pipe struct {
	rwait sync.Cond
	wwait sync.Cond
	l     sync.Mutex
	rl    sync.Mutex
	wl    sync.Mutex
	rerr  error // if reader closed, error to give writes
	werr  error // if writer closed, error to give reads
	msg   *Message
}

func (p *pipe) psend(msg *Message) error {
	var err error
	// One writer at a time.
	p.wl.Lock()
	defer p.wl.Unlock()

	p.l.Lock()
	defer p.l.Unlock()
	p.msg = msg
	p.rwait.Signal()
	for {
		if p.msg == nil {
			break
		}
		if p.rerr != nil {
			err = p.rerr
			break
		}
		if p.werr != nil {
			err = io.ErrClosedPipe
		}
		p.wwait.Wait()
	}
	p.msg = nil // in case of rerr or werr
	return err
}

func (p *pipe) send(msg *Message) (ret Receiver, err error) {
	// Prepare nested Receiver if requested
	if RetPipe.Equals(msg.Ret) {
		ret, msg.Ret = Pipe()
	}
	err = p.psend(msg)
	return
}

func (p *pipe) preceive() (*Message, error) {
	p.rl.Lock()
	defer p.rl.Unlock()

	p.l.Lock()
	defer p.l.Unlock()
	for {
		if p.rerr != nil {
			return nil, io.ErrClosedPipe
		}
		if p.msg != nil {
			break
		}
		if p.werr != nil {
			return nil, p.werr
		}
		p.rwait.Wait()
	}
	msg := p.msg
	p.msg = nil
	p.wwait.Signal()
	return msg, nil
}

func (p *pipe) receive(mode int) (*Message, error) {
	msg, err := p.preceive()
	if err != nil {
		return nil, err
	}
	if msg.Ret == nil {
		msg.Ret = NopSender{}
	}
	if mode&Ret == 0 {
		msg.Ret.Close()
	}
	return msg, nil
}

func (p *pipe) rclose(err error) {
	if err == nil {
		err = io.ErrClosedPipe
	}
	p.l.Lock()
	defer p.l.Unlock()
	p.rerr = err
	p.rwait.Signal()
	p.wwait.Signal()
}

func (p *pipe) wclose(err error) {
	if err == nil {
		err = io.EOF
	}
	p.l.Lock()
	defer p.l.Unlock()
	p.werr = err
	p.rwait.Signal()
	p.wwait.Signal()
}

// PipeReceiver

type PipeReceiver struct {
	p *pipe
}

func (r *PipeReceiver) Receive(mode int) (*Message, error) {
	return r.p.receive(mode)
}

func (r *PipeReceiver) SendTo(dst Sender) (int, error) {
	var n int
	// If the destination is a PipeSender, we can cheat
	pdst, ok := dst.(*PipeSender)
	if !ok {
		return 0, ErrIncompatibleSender
	}
	for {
		pmsg, err := r.p.preceive()
		if err == io.EOF {
			break
		}
		if err != nil {
			return n, err
		}
		if err := pdst.p.psend(pmsg); err != nil {
			return n, err
		}
	}
	n++
	return n, nil
}

func (r *PipeReceiver) Close() error {
	return r.CloseWithError(nil)
}

func (r *PipeReceiver) CloseWithError(err error) error {
	r.p.rclose(err)
	return nil
}

// PipeSender

type PipeSender struct {
	p *pipe
}

func (w *PipeSender) Send(msg *Message) (Receiver, error) {
	return w.p.send(msg)
}

func (w *PipeSender) ReceiveFrom(src Receiver) (int, error) {
	var n int
	// If the destination is a PipeReceiver, we can cheat
	psrc, ok := src.(*PipeReceiver)
	if !ok {
		return 0, ErrIncompatibleReceiver
	}
	for {
		pmsg, err := psrc.p.preceive()
		if err == io.EOF {
			break
		}
		if err != nil {
			return n, err
		}
		if err := w.p.psend(pmsg); err != nil {
			return n, err
		}
		n++
	}
	return n, nil
}

func (w *PipeSender) Close() error {
	return w.CloseWithError(nil)
}

func (w *PipeSender) CloseWithError(err error) error {
	w.p.wclose(err)
	return nil
}
