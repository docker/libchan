package bus

import (
	"fmt"
	"net"
	"testing"
	"time"

	"github.com/docker/libchan"
)

func TestSocketBus(t *testing.T) {
	bus := NewBus()
	c1, c2 := net.Pipe()
	err := bus.Connect(c2)
	if err != nil {
		t.Fatalf("Error connecting to bus: %s", err)
	}

	client, err := NewNetClient(c1, "test-client")
	if err != nil {
		t.Fatalf("Error creating client: %s", err)
	}

	if _, err := client.Register("test-name-1"); err != nil {
		t.Fatalf("Error registering: %s", err)
	}

	if _, err := client.Register("test-name-2"); err != nil {
		t.Fatalf("Error registering: %s", err)
	}
}

type SimpleMessage struct {
	Message string
}

type PluginMessage struct {
	Message string
	Ret     libchan.Sender
}

func TestMessage(t *testing.T) {
	plugin1 := func(client Client, start chan<- bool) error {
		res, err := client.Register("test-name-1")
		if err != nil {
			return err
		}
		var m PluginMessage
		if err := res.Receive(&m); err != nil {
			return err
		}
		if expected := "Test message from plugin 2"; m.Message != expected {
			return fmt.Errorf("unexpected message:\n\tExpected: %s\n\tExpected: %s", expected, m.Message)
		}
		response := &SimpleMessage{"A simple response"}
		if err := m.Ret.Send(response); err != nil {
			return err
		}
		return nil
	}
	plugin2 := func(client Client, start chan<- bool) error {
		recv1, remoteSender := libchan.Pipe()
		m := &PluginMessage{
			Message: "Test message from plugin 2",
			Ret:     remoteSender,
		}
		if err := client.Message("test-name-1", m); err != nil {
			return err
		}
		recv2, remoteSender := libchan.Pipe()
		m = &PluginMessage{
			Message: "Another test message from plugin 2",
			Ret:     remoteSender,
		}
		if err := client.Message("test-name-3", m); err != nil {
			return err
		}

		var response SimpleMessage
		if err := recv1.Receive(&response); err != nil {
			return err
		}
		if expected := "A simple response"; response.Message != expected {
			return fmt.Errorf("unexpected response:\n\tExpected: %s\n\tExpected: %s", expected, response.Message)
		}
		if err := recv2.Receive(&response); err != nil {
			return err
		}
		if expected := "A simple response from 3"; response.Message != expected {
			return fmt.Errorf("unexpected response:\n\tExpected: %s\n\tExpected: %s", expected, response.Message)
		}
		return nil
	}
	plugin3 := func(client Client, start chan<- bool) error {
		res, err := client.Register("test-name-3")
		if err != nil {
			return err
		}
		var m PluginMessage
		if err := res.Receive(&m); err != nil {
			return err
		}
		if expected := "Another test message from plugin 2"; m.Message != expected {
			return fmt.Errorf("unexpected message:\n\tExpected: %s\n\tExpected: %s", expected, m.Message)
		}
		response := &SimpleMessage{"A simple response from 3"}
		if err := m.Ret.Send(response); err != nil {
			return err
		}
		return nil
	}

	runTestPlugins(t, []PluginTest{plugin1, plugin2, plugin3})
}

type PluginTest func(Client, chan<- bool) error

func runTestPlugins(t *testing.T, plugins []PluginTest) {
	bus := NewBus()
	startChan := make(chan bool)
	type errStruct struct {
		i   int
		err error
	}
	errChan := make(chan *errStruct, len(plugins))
	for i := range plugins {
		c1, c2 := net.Pipe()
		if err := bus.Connect(c2); err != nil {
			t.Fatalf("Error connecting to bus: %s", err)
		}
		client, err := NewNetClient(c1, fmt.Sprintf("test-client-%d", i))
		if err != nil {
			t.Fatalf("Error creating client: %s", err)
		}
		go func(i int) {
			errChan <- &errStruct{
				i:   i + 1,
				err: plugins[i](client, startChan),
			}
		}(i)
	}
	time.Sleep(100 * time.Millisecond)
	close(startChan)

	for i := 0; i < len(plugins); i++ {
		res := <-errChan
		if res.err != nil {
			t.Fatalf("Error in plugin %d: %s", res.i, res.err)
		}
	}
}
