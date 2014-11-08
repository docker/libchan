package plugin

import (
	"errors"
	"fmt"
	"io"
	"net"
	"os"
	"os/exec"
	"sync"
	"syscall"

	"github.com/docker/libchan"
	"github.com/docker/libchan/spdy"
)

var (
	ErrPluginAlreadyStarted = errors.New("plugin already started")
)

type PluginServer struct {
	config PluginConfiguration
	cmd    *exec.Cmd
	stdin  io.WriteCloser

	chanInTransport  *spdy.Transport
	chanOutTransport *spdy.Transport

	ChanInPipe  libchan.Sender
	ChanOutPipe libchan.Receiver

	l sync.Mutex
}

type PluginConfiguration struct {
	Command string
	Args    []string
}

func NewPluginServer(config *PluginConfiguration) *PluginServer {
	plugin := &PluginServer{}
	plugin.config = *config

	return plugin
}

func (p *PluginServer) Start() error {
	p.l.Lock()
	defer p.l.Unlock()

	if p.cmd != nil {
		return ErrPluginAlreadyStarted
	}

	chanInDescriptors, err := syscall.Socketpair(syscall.AF_LOCAL, syscall.SOCK_STREAM, 0)
	if err != nil {
		return err
	}
	chanOutDescriptors, err := syscall.Socketpair(syscall.AF_LOCAL, syscall.SOCK_STREAM, 0)
	if err != nil {
		return err
	}

	chanInChildSocket := os.NewFile(uintptr(chanInDescriptors[0]), "chanInChild")
	chanInParentSocket := os.NewFile(uintptr(chanInDescriptors[1]), "chanInParent")
	chanOutChildSocket := os.NewFile(uintptr(chanOutDescriptors[0]), "chanOutChild")
	chanOutParentSocket := os.NewFile(uintptr(chanOutDescriptors[1]), "chanOutParent")
	fmt.Printf("%#v %#v", chanInDescriptors, chanOutDescriptors)

	defer chanInChildSocket.Close()
	defer chanOutChildSocket.Close()

	chanInConn, err := net.FileConn(chanInParentSocket)
	if err != nil {
		chanInParentSocket.Close()
		return err
	}
	chanOutConn, err := net.FileConn(chanOutParentSocket)
	if err != nil {
		chanOutParentSocket.Close()
		return err
	}

	cmd := exec.Command(p.config.Command, p.config.Args...)
	cmd.Env = append(cmd.Env, os.Environ()...)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	cmd.ExtraFiles = []*os.File{chanInChildSocket, chanOutChildSocket}
	stdin, err := cmd.StdinPipe()
	if err != nil {
		return err
	}

	if err := cmd.Start(); err != nil {
		chanOutConn.Close()
		chanInConn.Close()
		return err
	}

	fmt.Println("New receive transport")
	rt, err := spdy.NewServerTransport(chanInConn)
	if err != nil {
		chanOutConn.Close()
		chanInConn.Close()
		return err
	}
	fmt.Println("New send channel")
	chanIn, err := rt.NewSendChannel()
	if err != nil {
		chanOutConn.Close()
		chanInConn.Close()
		return err
	}
	fmt.Println("New send transport")
	st, err := spdy.NewServerTransport(chanOutConn)
	if err != nil {
		chanOutConn.Close()
		chanInConn.Close()
		return err
	}
	fmt.Println("New receive channel")
	chanOut, err := st.WaitReceiveChannel()
	if err != nil {
		chanOutConn.Close()
		chanInConn.Close()
		return err
	}
	fmt.Println("Done")
	p.chanInTransport = rt
	p.chanOutTransport = st
	p.ChanInPipe = chanIn
	p.ChanOutPipe = chanOut

	p.cmd = cmd
	p.stdin = stdin

	return nil
}

func (p *PluginServer) Stop() error {
	if p.cmd == nil {
		return errors.New("Not started")
	}
	if p.stdin != nil {
		if err := p.stdin.Close(); err != nil {
			return err
		}
	}
	return p.cmd.Wait()
}

type Plugin struct {
	ChanIn           libchan.Receiver
	ChanOut          libchan.Sender
	chanInTransport  *spdy.Transport
	chanOutTransport *spdy.Transport
}

func getTransport(fd uintptr, name string) (*spdy.Transport, error) {
	f := os.NewFile(fd, name)
	conn, err := net.FileConn(f)
	if err != nil {
		return nil, err
	}
	return spdy.NewClientTransport(conn)
}

func InitializePlugin() (*Plugin, error) {
	it, err := getTransport(uintptr(3), "chanin")
	if err != nil {
		return nil, err
	}
	chanIn, err := it.WaitReceiveChannel()
	if err != nil {
		return nil, err
	}
	ot, err := getTransport(uintptr(4), "chanout")
	if err != nil {
		return nil, err
	}
	chanOut, err := ot.NewSendChannel()
	if err != nil {
		return nil, err
	}

	return &Plugin{
		ChanIn:           chanIn,
		ChanOut:          chanOut,
		chanInTransport:  it,
		chanOutTransport: ot,
	}, nil
}
