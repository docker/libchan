package unix

import (
	"fmt"
	"os"

	lch "github.com/docker/libchan"
)

func Pair() (*Conn, *Conn, error) {
	c1, c2, err := USocketPair()
	if err != nil {
		return nil, nil, err
	}
	return &Conn{c1}, &Conn{c2}, nil
}

type Conn struct {
	*UnixConn
}

func sendablePair() (conn *UnixConn, remoteFd *os.File, err error) {
	// Get 2 *os.File
	local, remote, err := SocketPair()
	if err != nil {
		return nil, nil, err
	}
	defer func() {
		if err != nil {
			local.Close()
			remote.Close()
		}
	}()
	// Convert 1 to *net.UnixConn
	conn, err = FileConn(local)
	if err != nil {
		return nil, nil, err
	}
	local.Close()
	// Return the "mismatched" pair
	return conn, remote, nil
}

// This implements lch.Sender.Close which *only closes the sender*.
// This is similar to the pattern of only closing go channels from
// the sender's side.
// If you want to close the entire connection, call Conn.UnixConn.Close.
func (c *Conn) Close() error {
	return c.UnixConn.CloseWrite()
}

func (c *Conn) Send(msg *lch.Message) (lch.Receiver, error) {
	if msg.Stream != nil {
		return nil, fmt.Errorf("stream attachment not yet implemented in unix transport")
	}
	// Setup nested streams
	var (
		fd  *os.File
		ret lch.Receiver
		err error
	)
	// Caller requested a return pipe
	if lch.RetPipe.Equals(msg.Ret) {
		local, remote, err := sendablePair()
		if err != nil {
			return nil, err
		}
		fd = remote
		ret = &Conn{local}
		// Caller specified its own return channel
	} else if msg.Ret != nil {
		// The specified return channel is a unix conn: engaging cheat mode!
		if retConn, ok := msg.Ret.(*Conn); ok {
			fd, err = retConn.UnixConn.File()
			if err != nil {
				return nil, fmt.Errorf("error passing return channel: %v", err)
			}
			// Close duplicate fd
			retConn.UnixConn.Close()
			// The specified return channel is an unknown type: proxy messages.
		} else {
			local, remote, err := sendablePair()
			if err != nil {
				return nil, fmt.Errorf("error passing return channel: %v", err)
			}
			fd = remote
			// FIXME: do we need a reference no all these background tasks?
			go func() {
				// Copy messages from the remote return channel to the local return channel.
				// When the remote return channel is closed, also close the local return channel.
				localConn := &Conn{local}
				lch.Copy(msg.Ret, localConn)
				msg.Ret.Close()
				localConn.Close()
			}()
		}
	}
	if err := c.UnixConn.Send(msg.Data, fd); err != nil {
		return nil, err
	}
	return ret, nil
}

func (c *Conn) Receive(mode int) (*lch.Message, error) {
	b, fd, err := c.UnixConn.Receive()
	if err != nil {
		return nil, err
	}
	msg := &lch.Message{Data: b}

	// Apply mode mask
	if fd != nil {
		subconn, err := FileConn(fd)
		if err != nil {
			return nil, err
		}
		fd.Close()
		if mode&lch.Ret != 0 {
			msg.Ret = &Conn{subconn}
		} else {
			subconn.CloseWrite()
		}
	}
	return msg, nil
}
