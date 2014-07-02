package libchan

import (
	"github.com/ugorji/go/codec"
)

var mh codec.MsgpackHandle

func (msg *Message) Decode(data interface{}) error {
	decoder := codec.NewDecoderBytes(msg.Data, &mh)
	return decoder.Decode(data)
}

func (msg *Message) Encode(data interface{}) error {
	if msg.Data == nil {
		msg.Data = make([]byte, 0)
	}
	encoder := codec.NewEncoderBytes(&msg.Data, &mh)
	return encoder.Encode(data)
}
