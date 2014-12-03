package spdy

import (
	"encoding"
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
	case *channelWrapper:
		if val.direction == inbound {
			return c.copyReceiver(val)
		}
		return c.copySender(val)
	case *net.TCPConn:
		// Do nothing until socket support is added
	case *net.UDPConn:
		// Do nothing until socket support is added
	case io.ReadWriteCloser:
		return c.copyByteStream(val)
	case io.ReadCloser:
		return c.copyByteReadStream(val)
	case io.WriteCloser:
		return c.copyByteWriteStream(val)
	case libchan.Sender:
		return c.copySender(val)
	case libchan.Receiver:
		return c.copyReceiver(val)
	case map[string]interface{}:
		return c.copyChannelMessage(val)
	case map[interface{}]interface{}:
		return c.copyChannelInterfaceMessage(val)
	case encoding.BinaryMarshaler:
		p, err := val.MarshalBinary()
		if err != nil {
			return nil, err
		}

		return c.copyValue(p)
	default:
		if rv := reflect.ValueOf(v); rv.Kind() == reflect.Ptr {
			if rv.Elem().Kind() == reflect.Struct {
				return c.copyStructValue(rv.Elem())
			}
		} else if rv.Kind() == reflect.Struct {
			return c.copyStructValue(rv)
		}
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

func (c *channel) copyByteReadStream(stream io.ReadCloser) (io.ReadCloser, error) {
	streamCopy, err := c.session.createByteStream()
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

func (c *channel) copyByteWriteStream(stream io.WriteCloser) (io.WriteCloser, error) {
	streamCopy, err := c.session.createByteStream()
	if err != nil {
		return nil, err
	}
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

func (c *channel) copyChannelInterfaceMessage(m map[interface{}]interface{}) (interface{}, error) {
	mCopy := make(map[string]interface{})
	for k, v := range m {
		vCopy, vErr := c.copyValue(v)
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

func (c *channel) copyStructure(m interface{}) (interface{}, error) {
	v := reflect.Indirect(reflect.ValueOf(m))

	if v.Kind() != reflect.Struct {
		return nil, errors.New("invalid non struct type")
	}
	return c.copyStructValue(v)
}

func (c *channel) copyStructValue(v reflect.Value) (interface{}, error) {
	mCopy := make(map[string]interface{})
	t := v.Type()
	for i := 0; i < v.NumField(); i++ {
		// TODO(stevvooe): Calling Interface without checking if a type can be
		// interfaced may lead to panics. This value copier may need to be
		// refactored to handle arbitrary types.
		vCopy, vErr := c.copyValue(v.Field(i).Interface())
		if vErr != nil {
			return nil, vErr
		}
		mCopy[t.Field(i).Name] = vCopy
	}
	return mCopy, nil
}
