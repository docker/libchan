// Package testutil contains checks that implementations of libchan transports
// can use to check compliance. For now, this will only work with Go, but
// cross-language tests could be added here, as well.
package testutil

import (
	"io"
	"io/ioutil"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/docker/libchan"
)

func CheckTypeTransmission(t *testing.T, receiver libchan.Receiver, sender libchan.Sender) {
	// Add types that should be transmitted by value to this struct. Their
	// equality will be tested with reflect.DeepEquals.
	type ValueTypes struct {
		I int
		T time.Time
	}

	// Add other types, that may include readers or stateful items.
	type A struct {
		// TODO(stevvooe): Ideally, this would be embedded but libchan doesn't
		// seem to transmit embedded structs correctly.
		V      ValueTypes
		Reader io.ReadCloser // TODO(stevvooe): Only io.ReadCloser is support for now.
	}

	readerContent := "asdf"
	expected := A{
		V: ValueTypes{
			I: 1234,
			T: time.Now(),
		},
		Reader: ioutil.NopCloser(strings.NewReader(readerContent)),
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

	if !reflect.DeepEqual(received.V, expected.V) {
		t.Fatalf("expected structs to be equal: %#v != %#v", received.V, expected.V)
	}

	receivedContent, _ := ioutil.ReadAll(received.Reader)
	if string(receivedContent) != readerContent {
		t.Fatalf("reader transmitted different content %q != %q", receivedContent, readerContent)
	}

}
