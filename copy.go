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

		err = w.Send(m)
		if err != nil {
			return n, err
		}
		n++
	}
	return n, nil
}
