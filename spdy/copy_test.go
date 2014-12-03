package spdy

import (
	"reflect"
	"testing"
	"time"
)

func TestTypeTransmission(t *testing.T) {

	// Add problem types to this type definition. For now, we just care about
	// time.Time.
	type A struct {
		T time.Time
	}

	expected := A{T: time.Now()}

	sender, receiver, err := Pipe()
	if err != nil {
		t.Fatalf("error creating pipe: %v", err)
	}

	go func() {
		if err := sender.Send(expected); err != nil {
			t.Fatalf("unexpected error sending: %v", err)
		}
	}()

	var received A
	if err := receiver.Receive(&received); err != nil {
		t.Fatalf("unexpected error receiving: %v", err)
	}

	if !reflect.DeepEqual(received, expected) {
		t.Fatalf("expected structs to be equal: %#v != %#v", received, expected)
	}
}
