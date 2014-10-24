package bus

import (
	"errors"
	"fmt"
	"log"
	"net"
	"sync"

	"github.com/docker/libchan"
	"github.com/docker/libchan/spdy"
)

var (
	// ErrNameAlreadyExists is used when attempting to register
	// a name which is already in use.
	ErrNameAlreadyExists = errors.New("name already exists")

	// ErrAddressDoesNotExist is returned when attempting to send a message
	// to a named address which does not exist.
	ErrAddressDoesNotExist = errors.New("message address does not exist")
)

// Bus handles routing between clients as well as name registration
type Bus struct {
	senders  map[string]libchan.Sender
	sendLock sync.Mutex
}

// Client is used to communicate with the bus.
type Client interface {
	// Message sends the data object on the bus using the provided
	// named address.
	Message(string, interface{}) error

	// Register registers the given name with the bus and returns a
	// receiver to receive any messages send to the named address.
	Register(string) (libchan.Receiver, error)
}

type localClient struct {
	name string
	bus  *Bus
}

// netClient is Client implement which is connected
// to a bus through a network connection.
type netClient struct {
	name         string
	transport    libchan.Transport
	registerChan libchan.Sender
	messageChan  libchan.Sender
}

type clientMessage struct {
	Name string
	Data interface{}
	Ret  libchan.Sender
}

type proxyMessage struct {
	Name string
	Data map[string]interface{}
	Ret  libchan.Sender
}

type registerMessage struct {
	Name   string
	Sender libchan.Sender
	Ret    libchan.Sender
}

type response struct {
	Status string
}

type initializeMessage struct {
	// Name is a client unique value, no two clients with the same name
	// can be connected at the same time.  If empty no name is registered
	// for the connection.
	Name string

	MessageChan  libchan.Receiver
	RegisterChan libchan.Receiver

	Ret libchan.Sender
}

// NewBus creates a new bus which can start receiving connections
func NewBus() *Bus {
	return &Bus{
		senders: map[string]libchan.Sender{},
	}
}

// attachSender directly attaches a libchan Sender to a named
// address on the bus.
func (b *Bus) attachSender(name string, sender libchan.Sender) error {
	b.sendLock.Lock()
	defer b.sendLock.Unlock()
	if _, ok := b.senders[name]; ok {
		return ErrNameAlreadyExists
	}
	b.senders[name] = sender
	return nil
}

func (b *Bus) handleRegister(r libchan.Receiver) {
	for {
		var m registerMessage
		err := r.Receive(&m)
		if err != nil {
			log.Printf("Error receiving register", err)
			break
		}

		var resp response
		if err := b.attachSender(m.Name, m.Sender); err == ErrNameAlreadyExists {
			resp.Status = "name already exists"
		} else if err != nil {
			log.Printf("Error attaching sender: %s", err)
			break
		} else {
			resp.Status = "ok"
		}

		log.Printf("Responding to %#v", m)

		if err := m.Ret.Send(&resp); err != nil {
			log.Printf("Error sending register response: %s", err)
		}
	}
}

// sendMessage routes a message to the libchan sender associated with
// the given named address.
func (b *Bus) sendMessage(address string, message interface{}) error {
	sender, ok := b.senders[address]
	if !ok {
		return ErrAddressDoesNotExist
	}
	return sender.Send(message)
}

// Connect establishes a libchan connection.  The bus will act as the
// libchan transport server and the other end of the connection should
// act as the client.
func (b *Bus) Connect(conn net.Conn) error {
	t, err := spdy.NewServerTransport(conn)
	if err != nil {
		return err
	}
	go func() {
		defer t.Close()

		// Bus connections only have one top-level channel
		registerChan, err := t.WaitReceiveChannel()
		if err != nil {
			log.Printf("Error receiving channel: %s", err)
			return
		}

		var m initializeMessage
		if err := registerChan.Receive(&m); err != nil {
			log.Printf("Error receiving initialize message: %s", err)
			return
		}

		go b.handleRegister(m.RegisterChan)

		if err := m.Ret.Send(&response{"ok"}); err != nil {
			log.Printf("Error sending initalize response: %s", err)
			return
		}

		for {
			var proxy proxyMessage
			if err := m.MessageChan.Receive(&proxy); err != nil {
				log.Printf("Error receiving message: %s", err)
				return
			}
			if proxy.Ret == nil {
				log.Printf("Received proxy message with no return")
				return
			}
			log.Printf("Received proxy message: %#v", proxy)

			err := b.sendMessage(proxy.Name, proxy.Data)
			var resp response
			if err == ErrAddressDoesNotExist {
				resp.Status = "name does not exist"
			} else if err != nil {
				log.Printf("Error forwarding message: %s", err)
				resp.Status = "send err"
			} else {
				resp.Status = "ok"

			}

			if err := proxy.Ret.Send(&resp); err != nil {
				log.Printf("Error sending message response: %s", err)
				return
			}
		}
	}()

	return nil
}

// ListenAndServe accepts connects on the given listener
// and attaches the connections to the bus.  New
// connections will create a libchan server transport and
// immediately being accepting initialize requests from
// clients.
func (b *Bus) ListenAndServe(l net.Listener) error {
	for {
		c, err := l.Accept()
		if err != nil {
			return err
		}
		err = b.Connect(c)
		if err != nil {
			return err
		}
	}
}

// NewNetClient creates a new libchan transport using the
// provided connection and initializes the transport
// with the bus.
func NewNetClient(conn net.Conn, name string) (Client, error) {
	t, err := spdy.NewClientTransport(conn)
	if err != nil {
		return nil, err
	}

	initSender, err := t.NewSendChannel()
	if err != nil {
		return nil, err
	}

	retChan, remoteRetChan := libchan.Pipe()
	remoteRegisterChan, registerChan := libchan.Pipe()
	remoteMessageChan, messageChan := libchan.Pipe()

	initMessage := &initializeMessage{
		Name:         name,
		MessageChan:  remoteMessageChan,
		RegisterChan: remoteRegisterChan,
		Ret:          remoteRetChan,
	}

	if err := initSender.Send(initMessage); err != nil {
		return nil, err
	}

	var resp response
	if err := retChan.Receive(&resp); err != nil {
		return nil, err
	}

	if resp.Status != "ok" {
		return nil, fmt.Errorf("unexpected response status: %s", resp.Status)
	}

	return &netClient{
		name:         name,
		transport:    t,
		registerChan: registerChan,
		messageChan:  messageChan,
	}, nil

}

func (c *netClient) Register(name string) (libchan.Receiver, error) {
	ret, remoteRet := libchan.Pipe()
	recv, remoteSender := libchan.Pipe()
	m := &registerMessage{
		Name:   name,
		Sender: remoteSender,
		Ret:    remoteRet,
	}

	if err := c.registerChan.Send(m); err != nil {
		return nil, err
	}

	var resp response
	if err := ret.Receive(&resp); err != nil {
		return nil, err
	}

	if resp.Status != "ok" {
		return nil, fmt.Errorf("unexpected response status: %s", resp.Status)
	}

	return recv, nil
}

func (c *netClient) Message(name string, data interface{}) error {
	ret, remoteRet := libchan.Pipe()
	m := &clientMessage{
		Name: name,
		Data: data,
		Ret:  remoteRet,
	}

	if err := c.messageChan.Send(m); err != nil {
		return err
	}

	var resp response
	err := ret.Receive(&resp)
	if err != nil {
		return err
	}
	if resp.Status != "ok" {
		return fmt.Errorf("unexpected response status: %s", resp.Status)
	}
	return nil
}

// NewLocalClient returns a client directly attached to the bus without
// sending requests over the network.
func NewLocalClient(bus *Bus, name string) (Client, error) {
	// TODO Register name with bus

	return &localClient{
		name: name,
		bus:  bus,
	}, nil
}

func (c *localClient) Register(name string) (libchan.Receiver, error) {
	recv, sender := libchan.Pipe()
	err := c.bus.attachSender(name, sender)
	if err != nil {
		return nil, err
	}

	return recv, nil
}

func (c *localClient) Message(name string, data interface{}) error {
	return c.bus.sendMessage(name, data)
}
