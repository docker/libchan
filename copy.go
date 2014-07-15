package libchan

import (
	"io"
	"sync"
)

func Copy(dst Sender, src Receiver) (int, error) {
	var tasks sync.WaitGroup
	defer tasks.Wait()
	if senderTo, ok := src.(SenderTo); ok {
		if n, err := senderTo.SendTo(dst); err != ErrIncompatibleSender {
			return n, err
		}
	}
	if receiverFrom, ok := dst.(ReceiverFrom); ok {
		if n, err := receiverFrom.ReceiveFrom(src); err != ErrIncompatibleReceiver {
			return n, err
		}
	}
	var (
		n int
	)
	for {
		msg, err := src.Receive(Ret)
		if err == io.EOF {
			return n, nil
		}
		if err != nil {
			return n, err
		}
		if _, err := dst.Send(msg); err != nil {
			return n, err
		}
		n++
	}
}

func CopyChannel(w ChannelSender, r ChannelReceiver) (int, error) {
	var n int
	for {
		m := make(map[string]interface{})
		err := r.Receive(&m)
		if err != nil {
			if err == io.EOF {
				break
			} else {
				return n, err
			}
		}
		mCopy, err := copyChannelMessage(w, m)
		if err != nil {
			return n, err
		}

		err = w.Send(mCopy)
		if err != nil {
			return n, err
		}
		n++
	}
	return n, nil
}

func copyByteStream(sender ChannelSender, stream io.ReadWriteCloser) (io.ReadWriteCloser, error) {
	streamCopy, err := sender.CreateByteStream()
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

func copyChannelMessage(sender ChannelSender, m map[string]interface{}) (map[string]interface{}, error) {
	mCopy := make(map[string]interface{})
	for k, v := range m {
		// Throw error if tcp/udp connections?
		switch val := v.(type) {
		case io.ReadWriteCloser:
			streamCopy, err := copyByteStream(sender, val)
			if err != nil {
				return nil, err
			}
			mCopy[k] = streamCopy
		case ChannelSender:
			recv, send, err := sender.CreateNestedReceiver()
			if err != nil {
				return nil, err
			}
			go func() {
				CopyChannel(val, recv)
				// TODO propagate or log error
			}()
			mCopy[k] = send
		case ChannelReceiver:
			send, recv, err := sender.CreateNestedSender()
			if err != nil {
				return nil, err
			}
			go func() {
				CopyChannel(send, val)
				// TODO propagate or log error
			}()
			mCopy[k] = recv
		case map[string]interface{}:
			vCopy, err := copyChannelMessage(sender, val)
			if err != nil {
				return nil, err
			}
			mCopy[k] = vCopy
		default:
			mCopy[k] = v
		}
	}

	return mCopy, nil
}
