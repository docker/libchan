package main

import (
	"log"

	"github.com/docker/libchan/plugin"
)

func main() {
	config := &plugin.PluginConfiguration{
		Command: "./plugin1",
	}
	p := plugin.NewPluginServer(config)
	err := p.Start()
	if err != nil {
		log.Fatalf("Error starting plugin: %s", err)
	}

	log.Printf("Receiving")

	var m map[string]interface{}
	if err := p.ChanOutPipe.Receive(&m); err != nil {
		log.Fatalf("Error receiving: %s", err)
	}
	log.Printf("Received:\n%#v", m)

	m["server_key"] = "val"
	if err := p.ChanInPipe.Send(m); err != nil {
		log.Fatalf("Error receiving: %s", err)
	}
	log.Printf("Sent:\n%#v", m)

	if err := p.Stop(); err != nil {
		log.Fatalf("Error stopping plugin: %s", err)
	}

	log.Printf("Exiting")
}
