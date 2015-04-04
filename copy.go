package libchan

import (
	"errors"
	"io"
)

var (
	// ErrIncompatibleSender is used when an object cannot support
	// SendTo for its given argument.
	ErrIncompatibleSender = errors.New("incompatible sender")
	// ErrIncompatibleReceiver is used when an object cannot support
	// ReceiveFrom for its given argument.
	ErrIncompatibleReceiver = errors.New("incompatible receiver")
)

// ReceiverFrom defines a type which can directly receive objects
// from a receiver.
type ReceiverFrom interface {
	// ReceiveFrom receives object from the given receiver. If the given
	// receiver is not a supported type, this function should return
	// ErrIncompatibleReceiver.
	ReceiveFrom(Receiver) (int, error)
}

// SenderTo defines a type which can directly send objects
// from a sender.
type SenderTo interface {
	// SendTo sends object to the given sender. If the given
	// sender is not a supported type, this function should return
	// ErrIncompatibleSender.
	SendTo(Sender) (int, error)
}

// Copy copies from a receiver to a sender until an EOF is
// received.  The number of copies made is returned along
// with any error that may have halted copying prior to an EOF.
func Copy(w Sender, r Receiver) (int, error) {
	if senderTo, ok := r.(SenderTo); ok {
		if n, err := senderTo.SendTo(w); err != ErrIncompatibleSender {
			return n, err
		}
	}
	if receiverFrom, ok := w.(ReceiverFrom); ok {
		if n, err := receiverFrom.ReceiveFrom(r); err != ErrIncompatibleReceiver {
			return n, err
		}
	}

	var n int
	for {
		var m interface{}
		err := r.Receive(&m)
		if err != nil {
			if err == io.EOF {
				break
			} else {
				return n, err
			}
		}

		err = w.Send(m)
		if err != nil {
			return n, err
		}
		n++
	}
	return n, nil
}
