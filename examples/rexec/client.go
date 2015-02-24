package main

import (
	"crypto/tls"
	"io"
	"log"
	"net"
	"os"

	"github.com/dmcgowan/streams/spdy"
	"github.com/docker/libchan"
	"github.com/docker/libchan/encoding/msgpack"
	"github.com/docker/libchan/netchan"
)

// RemoteCommand is the run parameters to be executed remotely
type RemoteCommand struct {
	Cmd        string
	Args       []string
	Stdin      io.Writer
	Stdout     io.Reader
	Stderr     io.Reader
	StatusChan libchan.Sender
}

// CommandResponse is the returned response object from the remote execution
type CommandResponse struct {
	Status int
}

func main() {
	if len(os.Args) < 2 {
		log.Fatal("usage: <command> [<arg> ]")
	}

	var client net.Conn
	var err error
	if os.Getenv("USE_TLS") != "" {
		client, err = tls.Dial("tcp", "127.0.0.1:9323", &tls.Config{InsecureSkipVerify: true})
	} else {
		client, err = net.Dial("tcp", "127.0.0.1:9323")
	}
	if err != nil {
		log.Fatal(err)
	}

	provider, err := spdy.NewSpdyStreamProvider(client, false)
	if err != nil {
		log.Fatal(err)
	}
	transport := netchan.NewTransport(provider, &msgpack.Codec{})
	sender, err := transport.NewSendChannel()
	if err != nil {
		log.Fatal(err)
	}

	receiver, remoteSender := libchan.Pipe()

	command := &RemoteCommand{
		Cmd:        os.Args[1],
		Args:       os.Args[2:],
		Stdin:      os.Stdin,
		Stdout:     os.Stdout,
		Stderr:     os.Stderr,
		StatusChan: remoteSender,
	}

	err = sender.Send(command)
	if err != nil {
		log.Fatal(err)
	}

	response := &CommandResponse{}
	err = receiver.Receive(response)
	if err != nil {
		log.Fatal(err)
	}

	os.Exit(response.Status)
}
