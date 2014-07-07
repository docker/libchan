package streamutil

import (
	"bufio"
	"fmt"
	"os"
)

func DebugCheckpoint(msg string, args ...interface{}) {
	if os.Getenv("DEBUG") == "" {
		return
	}
	os.Stdout.Sync()
	tty, _ := os.OpenFile("/dev/tty", os.O_RDWR, 0700)
	fmt.Fprintf(tty, msg, args...)
	bufio.NewScanner(tty).Scan()
	tty.Close()
}

// Framing:
// In order to handle framing in Send/Recieve, as these give frame
// boundaries we use a very simple 4 bytes header. It is a big endiand
// uint32 where the high bit is set if the message includes a file
// descriptor. The rest of the uint32 is the length of the next frame.
// We need the bit in order to be able to assign recieved fds to
// the right message, as multiple messages may be coalesced into
// a single recieve operation.
func MakeHeader(data []byte, fds []int) ([]byte, error) {
	header := make([]byte, 4)

	length := uint32(len(data))

	if length > 0x7fffffff {
		return nil, fmt.Errorf("Data to large")
	}

	if len(fds) != 0 {
		length = length | 0x80000000
	}
	header[0] = byte((length >> 24) & 0xff)
	header[1] = byte((length >> 16) & 0xff)
	header[2] = byte((length >> 8) & 0xff)
	header[3] = byte((length >> 0) & 0xff)

	return header, nil
}

func ParseHeader(header []byte) (uint32, bool) {
	length := uint32(header[0])<<24 | uint32(header[1])<<16 | uint32(header[2])<<8 | uint32(header[3])
	hasFd := length&0x80000000 != 0
	length = length & ^uint32(0x80000000)

	return length, hasFd
}
