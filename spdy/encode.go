package spdy

import (
	"encoding/binary"
	"errors"
	"net"
	"reflect"
	"time"

	"github.com/dmcgowan/go/codec"
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

func (s *Transport) encodeChannel(v reflect.Value) ([]byte, error) {
	rc := v.Interface().(channel)
	if rc.stream == nil {
		return nil, errors.New("bad type")
	}
	if rc.session != s {
		return nil, errors.New("cannot decode channel from different session")
	}

	// Get stream identifier?
	buf := make([]byte, 9)
	if rc.direction == inbound {
		buf[0] = 0x02 // Reverse direction
	} else if rc.direction == outbound {
		buf[0] = 0x01 // Reverse direction
	} else {
		return nil, errors.New("invalid direction")
	}
	written := encodeReferenceID(buf[1:], rc.referenceID)
	return buf[:(written + 1)], nil
}

func (s *Transport) decodeChannel(v reflect.Value, b []byte) error {
	rc := v.Interface().(channel)

	if b[0] == 0x01 {
		rc.direction = inbound
	} else if b[0] == 0x02 {
		rc.direction = outbound
	} else {
		return errors.New("unexpected direction")
	}
	referenceID, err := decodeReferenceID(b[1:])
	if err != nil {
		return err
	}

	c := s.getChannel(referenceID)
	if c == nil {
		return errors.New("channel does not exist")
	}
	v.Set(reflect.ValueOf(*c))

	return nil
}

// Wrapper around channel to cue a copy, should not be encoded directly
type channelWrapper struct {
	*channel
}

// Wrapper around byte to cue a copy, should not be encoded directly
type byteStreamWrapper struct {
	*byteStream
}

func (s *Transport) decodeChannelWrapper(v reflect.Value, b []byte) error {
	c := &channel{}
	v.FieldByName("channel").Set(reflect.ValueOf(c))
	return s.decodeChannel(reflect.ValueOf(c).Elem(), b)
}

func (s *Transport) encodeStream(v reflect.Value) ([]byte, error) {
	bs := v.Interface().(byteStream)
	if bs.referenceID == 0 {
		return nil, errors.New("bad type")
	}
	var buf [8]byte
	written := encodeReferenceID(buf[:], bs.referenceID)

	return buf[:written], nil
}

func (s *Transport) decodeStream(v reflect.Value, b []byte) error {
	referenceID, err := decodeReferenceID(b)
	if err != nil {
		return err
	}

	bs := s.getByteStream(referenceID)
	if bs != nil {
		v.Set(reflect.ValueOf(*bs))
	}

	return nil
}

func (s *Transport) decodeWrapper(v reflect.Value, b []byte) error {
	bs := &byteStream{}
	s.decodeStream(reflect.ValueOf(bs).Elem(), b)
	v.FieldByName("byteStream").Set(reflect.ValueOf(bs))
	return nil
}

func (s *Transport) waitConn(network, local, remote string, timeout time.Duration) (net.Conn, error) {
	timeoutChan := time.After(timeout)
	connChan := make(chan net.Conn)

	go func() {
		defer close(connChan)
		s.netConnC.L.Lock()
		defer s.netConnC.L.Unlock()
		key := addrKey(local, remote)
		networkType, ok := s.networks[network]
		for !ok {
			select {
			case <-timeoutChan:
				return // Don't wait, timeout already occured
			default:
			}
			s.netConnC.Wait()
			networkType, ok = s.networks[network]
		}
		networks := s.netConns[networkType]
		conn, ok := networks[key]
		for !ok {
			select {
			case <-timeoutChan:
				return // Don't bother, timeout already occured
			default:
			}
			s.netConnC.Wait()
			conn, ok = networks[key]
		}
		connChan <- conn
	}()

	select {
	case conn := <-connChan:
		return conn, nil
	case <-timeoutChan:
		return nil, errors.New("timeout")
	}
}

func encodeString3(s1, s2, s3 string) ([]byte, error) {
	if len(s1) > 0x7F || len(s2) > 0x7F || len(s3) > 0x7F {
		return nil, errors.New("invalid string length")
	}
	b := make([]byte, len(s1)+len(s2)+len(s3)+3)
	b[0] = byte(len(s1))
	copy(b[1:], s1)
	b[len(s1)+1] = byte(len(s2))
	copy(b[len(s1)+2:], s2)
	b[len(s1)+len(s2)+2] = byte(len(s3))
	copy(b[len(s1)+len(s2)+3:], s3)
	return b, nil
}

func readString(b []byte) (int, string, error) {
	if len(b) == 0 {
		return 0, "", errors.New("invalid length")
	}
	l := int(b[0])
	if len(b) < l+1 {
		return 0, "", errors.New("invalid length")
	}
	s := make([]byte, l)
	copy(s, b[1:l+1])
	return l + 1, string(s), nil
}

func decodeString3(b []byte) (string, string, string, error) {
	n, s1, err := readString(b)
	if err != nil {
		return "", "", "", err
	}
	b = b[n:]
	n, s2, err := readString(b)
	if err != nil {
		return "", "", "", err
	}
	b = b[n:]
	n, s3, err := readString(b)
	if err != nil {
		return "", "", "", err
	}
	return s1, s2, s3, nil
}

func (s *Transport) encodeNetConn(v reflect.Value) ([]byte, error) {
	var conn net.Conn
	switch t := v.Interface().(type) {
	case net.TCPConn:
		conn = &t
	case net.UDPConn:
		conn = &t
	case net.Conn:
		conn = t
	default:
		return nil, errors.New("unknown type")
	}

	// Flip remote and local for encoding
	return encodeString3(conn.LocalAddr().Network(), conn.RemoteAddr().String(), conn.LocalAddr().String())
}

func (s *Transport) decodeNetConn(v reflect.Value, b []byte) error {
	network, local, remote, err := decodeString3(b)
	if err != nil {
		return err
	}
	conn, err := s.waitConn(network, local, remote, 10*time.Second)
	if err != nil {
		return err
	}

	v.Set(reflect.ValueOf(conn).Elem())

	return nil
}

func (s *Transport) initializeHandler() *codec.MsgpackHandle {
	mh := &codec.MsgpackHandle{WriteExt: true}
	mh.RawToString = true
	err := mh.AddExt(reflect.TypeOf(channelWrapper{}), 0x01, nil, s.decodeChannelWrapper)
	if err != nil {
		panic(err)
	}

	err = mh.AddExt(reflect.TypeOf(channel{}), 0x01, s.encodeChannel, nil)
	if err != nil {
		panic(err)
	}

	err = mh.AddExt(reflect.TypeOf(byteStreamWrapper{}), 0x02, nil, s.decodeWrapper)
	if err != nil {
		panic(err)
	}

	// never decode directly,  ensures byte streams are always wrapped
	err = mh.AddExt(reflect.TypeOf(byteStream{}), 0x02, s.encodeStream, nil)
	if err != nil {
		panic(err)
	}

	// Register networks
	s.networks["tcp"] = 0x04
	s.netConns[0x04] = make(map[string]net.Conn)
	err = mh.AddExt(reflect.TypeOf(net.TCPConn{}), 0x04, s.encodeNetConn, s.decodeNetConn)
	if err != nil {
		panic(err)
	}

	s.networks["udp"] = 0x05
	s.netConns[0x05] = make(map[string]net.Conn)
	err = mh.AddExt(reflect.TypeOf(net.UDPConn{}), 0x05, s.encodeNetConn, s.decodeNetConn)
	if err != nil {
		panic(err)
	}

	// TODO add unix network as 0x06

	return mh
}
