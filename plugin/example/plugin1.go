package main

import (
	"io/ioutil"
	"log"
	"os"

	"github.com/docker/libchan/plugin"
)

type s1 struct {
	Key string
}

func main() {
	p, err := plugin.InitializePlugin()
	if err != nil {
		log.Fatalf("Error initalizing plugin: %s", err)
	}

	if err := p.ChanOut.Send(&s1{"hello server"}); err != nil {
		log.Fatalf("Error sending message: %s", err)
	}

	m := map[string]interface{}{}
	if err := p.ChanIn.Receive(&m); err != nil {
		log.Fatalf("Error sending message: %s", err)
	}
	log.Printf("Client receive: %#v", m)

	if b, err := ioutil.ReadAll(os.Stdin); err != nil {
		log.Fatalf("Error reading from stdin: %s", err)
	} else {
		log.Printf("Read %q", b)
	}

	// Close plugin
}
