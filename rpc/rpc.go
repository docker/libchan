package rpc

import (
	"errors"
	"io"
	"sync"

	log "github.com/Sirupsen/logrus"
	"github.com/docker/libchan"
)

type Cmd struct {
	Op   string
	Args []string
	KV   map[string]interface{}
	Out  libchan.Sender
}

type Event struct {
	Stream string
	Msg    string
	KV     map[string]interface{}
}

type Plug func(libchan.Receiver, libchan.Sender) error

type SwitchBoard struct {
	l         sync.RWMutex
	routes    map[string]libchan.Sender
	subRoutes map[libchan.Sender]map[string]libchan.Sender
}

func NewSwitchBoard() *SwitchBoard {
	return &SwitchBoard{
		routes:    map[string]libchan.Sender{},
		subRoutes: map[libchan.Sender]map[string]libchan.Sender{},
	}
}

func (sb *SwitchBoard) StartRouting(p Plug) <-chan error {
	remoteReceiver, sender := libchan.Pipe()
	receiver, remoteSender := libchan.Pipe()
	errChan := make(chan error, 1)

	go func() {
		for {
			var cmd Cmd
			if err := receiver.Receive(&cmd); err != nil {
				if err != io.EOF {
					log.WithField("error", err).Errorf("receive error")
				}
				break
			}
			switch cmd.Op {
			case "register":
				if len(cmd.Args) == 0 {
					continue
				}
				sb.l.Lock()
				m, exists := sb.subRoutes[sender]
				if !exists {
					m = map[string]libchan.Sender{}
					sb.subRoutes[sender] = m
				}
				for _, name := range cmd.Args {
					if name == "register" {
						log.Warnf("plugin attempt to register %q", name)
						continue
					}
					m[name] = sb.routes[name]
					sb.routes[name] = sender
				}
				sb.l.Unlock()
			default:
				var sendTo libchan.Sender
				var hasLocal bool
				sb.l.RLock()
				if m, ok := sb.subRoutes[sender]; ok {
					sendTo, hasLocal = m[cmd.Op]
				}
				if !hasLocal {
					sendTo = sb.routes[cmd.Op]
				}
				sb.l.RUnlock()
				if sendTo != nil {
					if err := sendTo.Send(cmd); err != nil {
						log.WithField("error", err).Errorf("error sending to plugin")
						cmd.Out.Close()
						continue
					}
				} else {
					log.WithField("op", cmd.Op).Debugf("no sender for operation")
				}
			}
		}
	}()

	go func() {
		defer close(errChan)
		if err := p(remoteReceiver, remoteSender); err != nil {
			log.WithField("error", err).Errorf("error running plug")
			errChan <- err
		}
		// Cleanup routes
		// Close sender
	}()

	return errChan
}

func (sb *SwitchBoard) Call(cmd *Cmd) error {
	route, exists := sb.routes[cmd.Op]
	if !exists {
		return errors.New("no routes to op")
	}
	return route.Send(cmd)
}
