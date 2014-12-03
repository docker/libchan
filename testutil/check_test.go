package testutil

import (
	"github.com/docker/libchan"

	"testing"
)

// TestTypeTransmission tests the main package (to avoid import cycles) and
// provides and example of how this test should be used in other packages.
func TestTypeTransmission(t *testing.T) {
	receiver, sender := libchan.Pipe()
	CheckTypeTransmission(t, receiver, sender)
}
