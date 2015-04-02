package spdy

import (
	"testing"

	"github.com/docker/libchan/testutil"
)

func TestTypeTransmission(t *testing.T) {
	sender, receiver, err := Pipe()
	if err != nil {
		t.Fatalf("error creating pipe: %v", err)
	}

	testutil.CheckTypeTransmission(t, receiver, sender)
}
