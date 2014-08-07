# libchan: like Go channels over the network [![Build Status](https://travis-ci.org/docker/libchan.png?branch=master)](https://travis-ci.org/docker/libchan)

Libchan is an ultra-lightweight networking library which lets network services communicate
in the same way that goroutines communicate using channels:

* Simple message passing
* Synchronization for concurrent programming
* Nesting: channels can send channels

Libchan supports the following transports out of the box:

* In-memory Go channel
* Unix socket
* Raw TCP
* TLS
* HTTP2/SPDY
* Websocket

This provides great flexibility in scaling an application by breaking it down into
loosely coupled concurrent services. The same application could be composed of
goroutines communicating over in-memory channels; then transition to separate
unix processes, each assigned to a processor core, and communicating over
high-performance IPC; then to a cluster of machines communicating over
authenticated TLS sessions. All along it benefits from the concurrency model
which has made Go so popular.

Not all transports have the same semantics. In-memory Go channels guarantee
exactly-once delivery; TCP, TLS, and the various HTTP socket families do not
guarantee delivery. Messages arrive in order but may be arbitrarily delayed or
lost. There are no ordering invariants across channels.

An explicit goal of libchan is simplicity of implementation and clarity of
spec. Porting it to any language should be as effortless as humanly possible.

## Focused on not reinventing the wheel

Because remote libchan sessions are regular HTTP2 over TLS sessions, they can
be used in combination with any standard proxy or authentication
middleware. This means libchan, when configured properly, can be safely exposed
on the public Internet. It can also be embedded in an existing rest API
using an http1 and websocket fallback.

## How is it different from RPC or REST?

Modern micro-services are not a great fit for classical RPC or REST
protocols because they often rely heavily on events, bi-directional
communication, stream multiplexing, and some form of data synchronization.
Sometimes these services have a component which requires raw socket access,
either for performance (file transfer, event firehose, database access) or
simply because they have their own protocol (dns, smtp, sql, ssh,
zeromq, etc). These components typically need a separate set of tools
because they are outside the scope of the REST and RPC tools. If there is
also a websocket or ServerEvents transport, those require yet another layer
of tools.

Instead of a clunky patchwork of tools, libchan implements in a single
minimalistic library all the primitives needed by modern micro-services:

* Request/response with arbitrary structured data

* Asynchronous events flowing in real-time in both directions

* Requests and responses can flow in any direction, and can be arbitrarily
nested, for example to implement a self-registering worker model

* Any message serialization format can be plugged in: json, msgpack, xml,
protobuf.

* Raw file descriptors can be "attached" to any message, and passed under
the hood using the best method available to each transport. The Go channel
transport just passes os.File pointers around. The unix socket transport
uses fd passing which makes it suitable for high-performance IPC. The
tcp transport uses dedicated http2 streams. And as a bonus extension, a
built-in  tcp gateway can be used to proxy raw network sockets without
extra overhead. That means libchan services can be used as smart gateways to a
sql database, ssh or file transfer service, with unified auth, discovery
and tooling and without performance penalty.

## Example usage

Here's an example implementing basic RPC-style request/response.  We gloss over error handling to tersely demonstrate the core concepts.

On the client:

```go
var ch libchan.Sender

// Send a message, indicate that we want a return channel to be automatically created
ret1, err := ch.Send(&libchan.Message{Data: []byte("request 1!"), Ret: libchan.RetPipe})

// Send another message on the same channel
ret2, err := ch.Send(&libchan.Message{Data: []byte("request 2!"), Ret: libchan.RetPipe})

// Wait for an answer from the first request.  Set flags to zero
// to indicate we don't want a nested return channel.
msg, err := ret1.Receive(0)
```

On the server:

```go
var ch libchan.Receiver

// Wait for messages in a loop
// Set the return channel flag to indicate that we
// want to receive nested channels (if any).
// Note: we don't send a nested return channel, but we could.
for {
	msg, err := ch.Receive(libchan.Ret)
	msg.Ret.Send(&libchan.Message{Data: []byte("this is an extremely useful response")});
}
```

## Creators

**Solomon Hykes**

- <http://twitter.com/solomonstre>
- <http://github.com/shykes>

## Additional Implementations

### Java
- https://github.com/ndeloof/jchan

### Javascript / Node.js
- https://github.com/GraftJS/jschan

## Copyright and license

Code and documentation copyright 2013-2014 Docker, inc. Code released under the Apache 2.0 license.
Docs released under Creative commons.
