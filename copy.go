package libchan

import (
	"io"
)

// Copy copies from a receiver to a sender until an EOF is
// received.  The number of copies made is returned along
// with any error that may have halted copying prior to an EOF.
func Copy(w Sender, r Receiver) (int, error) {
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

func copyByteStream(sender Sender, stream io.ReadWriteCloser) (io.ReadWriteCloser, error) {
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

func copyChannelMessage(sender Sender, m map[string]interface{}) (map[string]interface{}, error) {
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
		case Sender:
			recv, send, err := sender.CreateNestedReceiver()
			if err != nil {
				return nil, err
			}
			go func() {
				Copy(val, recv)
				// TODO propagate or log error
			}()
			mCopy[k] = send
		case Receiver:
			send, recv, err := sender.CreateNestedSender()
			if err != nil {
				return nil, err
			}
			go func() {
				Copy(send, val)
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
