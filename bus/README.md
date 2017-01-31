# libchan/bus

Libchan/Bus is an implementation of a low-level bus used for interprocess communication.
It is building off the work being done with libchan and spdy to achieve an easy to use, flexible, and performant IPC system.
Ideally a higher level interface wraps direct bus usage in an application and the bus is only used to aid in communication.
Libchan/Bus is intentially simple and modeled after existing systems such as D-Bus.


## Usage
Example bus setup in which the clients are in the same process or process subtree

Bus:
~~~~
bus := NewBus()
client1Conn, bus1 := net.Pipe()
client2Conn, bus2 := net.Pipe()

bus.Connect(bus1) // error ignored
bus.Connect(bus2) // error ignored
~~~~

Client 1:
~~~~
client, _ := NewNetClient(client1Conn, "client-1") //client1Conn inherited or passed from Bus
recv, _ := client.Register("test-address-1")

var v map[string]interface{}
recv.Receive(&v) // error ignored

fmt.Printf("%#v\n", v)
~~~~

Client 2:
~~~~
client, _ := NewNetClient(client2Conn, "client-2") //client2Conn inherited or passed from Bus

client.Message("test-address-1", map[string]interface{}{"key": "any libchan value"}) // error ignored
~~~~

## Design

The bus is designed to be a thin layer which achieves message routing using libchan.
Communication with the bus will still use libchan objects and all transport is done over libchan transports.
The message objects are limited to what can be sent by libchan, but the bus does not add extra limitations.

### Bus

The message router which manages connections and addresses.

### Client

Named client capable of registering addresses and sending messages.  The client name must be unique.

### Addresses/Names

Addresses are string values which are used to route messages to clients.
Clients register addresses with the bus and get a receiver channel which messages are routed to.

### Messages

Messages are key/value objects (struct or map) which are routed to clients by an address.
The values can be any type that libchan supports included nested channels and streams.

## Not yet implemented

### Signaling/Broadcasting

The current implementation does not have a method for signaling all attached clients.
This is possible to implement using the current design, but ideally is not used as an event or pub/sub system.
If a pub/sub system is needed, it can be implemented as a bus client.
