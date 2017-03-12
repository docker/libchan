package rpc

import (
	"fmt"
	"io"
	"testing"
	"time"

	"github.com/docker/libchan"
)

func plug1(r libchan.Receiver, s libchan.Sender) error {
	c := &Cmd{
		Op:   "register",
		Args: []string{"test"},
	}
	if err := s.Send(c); err != nil {
		return err
	}
	var cmd Cmd
	if err := r.Receive(&cmd); err != nil {
		if err != io.EOF {
			return err
		}
	}

	if cmd.Op != "test" {
		return fmt.Errorf("Unexpected operation to test plug: %s", cmd.Op)
	}

	if len(cmd.Args) != 3 {
		return fmt.Errorf("Expected 3 arguments")
	}
	if cmd.Args[0] != "some" {
		return fmt.Errorf("Expected first arg as %q, received %q", "some", cmd.Args[0])
	}
	if cmd.Args[1] != "test" {
		return fmt.Errorf("Expected first arg as %q, received %q", "some", cmd.Args[1])
	}
	if cmd.Args[2] != "args" {
		return fmt.Errorf("Expected first arg as %q, received %q", "some", cmd.Args[2])
	}

	e := &Event{
		Stream: "data",
		Msg:    "return test value",
		KV: map[string]interface{}{
			"test_key": []string{"v1", "v2"},
		},
	}

	return cmd.Out.Send(e)
}

func TestSwitchBoard(t *testing.T) {
	sb := NewSwitchBoard()
	errChan := sb.StartRouting(plug1)

	time.Sleep(50 * time.Millisecond)

	r, s := libchan.Pipe()
	sb.Call(&Cmd{
		Op:   "test",
		Args: []string{"some", "test", "args"},
		Out:  s,
	})

	var e Event
	if err := r.Receive(&e); err != nil {
		t.Fatalf("Error receiving event")
	}

	if e.Stream != "data" {
		t.Fatalf("Unexpected stream value\n\tExpected: %s\n\tActual: %s", "data", e.Stream)
	}

	if expected := "return test value"; e.Msg != expected {
		t.Fatalf("Unexpected return value\n\tExpected: %s\n\tActual: %s", expected, e.Msg)
	}

	if err := <-errChan; err != nil {
		t.Fatalf("Error running plug: %s")
	}
}
