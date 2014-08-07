package spdy

import (
	"errors"
	"io"
	"net"
	"reflect"

	"github.com/docker/libchan"
)

// copyMessage copies a key-value structure into a map, recursively
// copying any streams or channels using the provided sender.
func (c *channel) copyMessage(message interface{}) (interface{}, error) {
	mapCopy, mapOk := message.(map[string]interface{})
	if mapOk {
		return c.copyChannelMessage(mapCopy)
	}
	return c.copyStructure(message)
}

func (c *channel) copyValue(v interface{}) (interface{}, error) {
	switch val := v.(type) {
	case *byteStream:
		if val.session != c.session {
			return c.copyByteStream(val)
		}
	case *channel:
		if val.session != c.session {
			if val.direction == inbound {
				return c.copyReceiver(val)
			}
			return c.copySender(val)
		}
	case *net.TCPConn:
		// Do nothing until socket support is added
	case *net.UDPConn:
		// Do nothing until socket support is added
	case io.ReadWriteCloser:
		return c.copyByteStream(val)
	case libchan.Sender:
		return c.copySender(val)
	case libchan.Receiver:
		return c.copyReceiver(val)
	case map[string]interface{}:
		return c.copyChannelMessage(val)
	case struct{}:
		return c.copyStructure(v)
	case *struct{}:
		return c.copyStructure(v)
	default:
	}
	return v, nil
}

func (c *channel) copySender(val libchan.Sender) (libchan.Sender, error) {
	recv, send, err := c.CreateNestedReceiver()
	if err != nil {
		return nil, err
	}
	go func() {
		libchan.Copy(val, recv)
		val.Close()
	}()
	return send, nil
}

func (c *channel) copyReceiver(val libchan.Receiver) (libchan.Receiver, error) {
	send, recv, err := c.CreateNestedSender()
	if err != nil {
		return nil, err
	}
	go func() {
		libchan.Copy(send, val)
		send.Close()
	}()
	return recv, nil
}

func (c *channel) copyByteStream(stream io.ReadWriteCloser) (io.ReadWriteCloser, error) {
	streamCopy, err := c.session.createByteStream()
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

func (c *channel) copyChannelMessage(m map[string]interface{}) (interface{}, error) {
	mCopy := make(map[string]interface{})
	for k, v := range m {
		vCopy, vErr := c.copyValue(v)
		if vErr != nil {
			return nil, vErr
		}
		mCopy[k] = vCopy
	}

	return mCopy, nil
}

func (c *channel) copyStructure(m interface{}) (interface{}, error) {
	v := reflect.ValueOf(m)
	if v.Kind() == reflect.Ptr {
		v = v.Elem()
	}
	if v.Kind() != reflect.Struct {
		return nil, errors.New("invalid non struct type")
	}
	mCopy := make(map[string]interface{})
	t := v.Type()
	for i := 0; i < v.NumField(); i++ {
		vCopy, vErr := c.copyValue(v.Field(i).Interface())
		if vErr != nil {
			return nil, vErr
		}
		mCopy[t.Field(i).Name] = vCopy
	}
	return mCopy, nil
}
