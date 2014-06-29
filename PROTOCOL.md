# libchan protocol specification

Extreme portability is a key design goal of libchan.

This document specifies the libchan protocol to allow multiple implementations to co-exist with
full interoperability.

## Version

No version yet.

## Author

Solomon Hykes <solomon@docker.com>

## Status

This specification is still work in progress. Things will change, probably in reverse-incompatible ways.
We hope to reach full API stability soon.

## Terminology

### Channel

A `channel` is an object which allows 2 concurrent programs to communicate with each other. The semantics
of a libchan channel are very similar (but not identical) to those of Go's native channels.

A channel has 2 ends: a `Sender` end and a `Receiver` end. The Sender can send messages and close the channel.
The Receiver can receive messages. Messages arrive in the same order they were sent.

A channel is uni-directional: messages can only flow in one direction. So channels are more similar to pipes
than to sockets.

### Message

A message is a discrete packet of data which can be sent on a channel. Messages are structured into multiple
fields. The protocol defines which data types can be carried by a message, and how transports should encode and
decode them.

### Byte stream

A byte stream is an object which implements raw IO with `Read`, `Write` and `Close` methods.
Typical byte streams are text files, network sockets, memory buffers, pipes, and so on.

One distinct characteristic of libchan is that it can encode byte streams as first class fields
in a message, alongside more basic types like integers or strings.

### Nesting

Libchan supports nesting. This means that a libchan message can include a channel, which itself
can be used to send and receive messages, and so on all the way down.

Nesting is a fundamental property of libchan.

## Underlying transport

The libchan protocol requires a reliable, 2-way byte stream as a transport.
The most popular options are TCP connections, unix stream sockets and TLS sessions. 

It is also possible to use websocket as an underlying transport, which allows exposing
a libchan endpoint at an HTTP1 url.

## Authentication and encryption

Libchan can optionally use TLS to authenticate and encrypt communications. After the initial
handshake and protocol negotiation, the TLS session is simply used as the transport for
the libchan wire protocol.

## Wire protocol

Libchan uses SPDY (protocol draft 3) as its wire protocol, with no modification.

## Control protocol

Once 2 libchan endpoints have established a SPDY session, they communicate with the following
control protocol.

### Top-level channels

Each SPDY session may carry multiple concurrent channels, in both directions, using standard
SPDY framing and stream multiplexing. Each libchan channel is implemented by an underlying
SPDY stream.

To use a SPDY session, either endpoint may initiate new channels, wait for its peer to
initiate new channels, or a combination of both. Channels initiated in this way are called
*top-level channels*.

* To initiate a new top-level channel, either endpoint may initiate a new SPDY stream, then
start sending messages to it (see *"sending messages"*).

* The endpoint initiating a top-level channel MAY NOT allow the application to receive messages
from it and MUST ignore inbound messages received on that stream.

* When an endpoint receives a new inbound SPDY stream, and the initial headers DO NOT include
the key `libchan-ref`, it MUST queue a new `Receiver` channel to pass to the application.

* The endpoint receiving a top-level channel MAY NOT allow the application to send messages to
it.


### Sending messages on a channel

Once a SPDY stream is initiated, it can be used as a channel, with the initiating endpoint holding
the `Sender` end of the channel, and the recipient endpoint holding the `Receiver` end.

* To send a message, the sender MUST encode it using the [msgpack](https://msgpack.org) encoding format, and
send a single data frame on the corresponding SPDY stream, with the encoded message as the exact content of
the frame.

* When receiving a data frame on any active SPDY stream, the receiver MUST decode it using msgpack. If
the decoding fails, the receiver MUST close the underlying stream, and future calls to `Receive` on that
channel MUST return an error.

* A valid msgpack decode operation with leftover trailing or leading data is considered an *invalid* msgpack
decode operation, and MUST yield the corresponding error.

### Closing a channel

The endpoint which initiated a channel MAY close it by closing the underlying SPDY stream.

*FIXME: provide more details*

### Sending byte streams

Libchan messages support a special type called *byte streams*. Unlike regular types like integers or strings,
byte streams are not fully encoded in the message. Instead, the message encodes a *reference* which allows
the receiving endpoint to reconstitute the byte stream after receiving the message, and pass it to the
application.

*FIXME: specify use of msgpack extended types to encode byte streams*

Libchan supports 2 methods for sending byte streams: a default method which is supported on all transports,
and an optional method which requires unix stream sockets. All implementations MUST support both methods.

#### Default method: SPDY streams

The primary method for sending a byte stream is to send it over a SPDY stream, with the following protocol:

* When encoding a message including 1 or more byte stream values, the sender MUST assign to each value
an identifier unique to the session, and store these identifiers for future use.

* After sending the encoded message, the sender MUST initiate 1 new SPDY stream for each byte stream value
in the message.

* Each of those SPDY stream MUST include an initial header with as a key the string "*libchan-ref*", and
as a value the identifier of the corresponding byte stream.

Conversely, the receiver must follow this protocol:

* When decoding a message including 1 or more byte stream values, the receiver MUST store the unique identifier
of each value in a session-wide table of pending byte streams. It MAY then immediately pass the decoded message to the application.

* The sender SHOULD cap the size of its pending byte streams table to a reasonable value. It MAY make that value
configurable by the application. If it receives a message with 1 or more byte stream references, and the table
is full, the sender MAY suspend processing of the message until there is room in the table.

* When receiving new SPDY streams which include the header key "*libchan-ref*", the receiver MUST lookup that
header value in the table of pending byte streams. If the value is registered in the table, that SPDY stream
MUST be passed to the application.

On either end, once the SPDY stream for a byte-stream value is established, it MUST be exposed to the application
as follows:

* After sending each of those SPDY streams, each write operation by the application to a byte-stream field MUST
trigger the sending of a single data frame on the corresponding SPDY stream.

* Each read operation by the application from a byte-stream field MUST yield the content of the next
data frame received on the corresponding SPDY stream. If the reading end of the SPDY stream is closed,
the read operation MUST yield EOF.

* A close operation by the application on the a byte-stream field MUST trigger the closing of the writing end
of the corresponding SPDY stream.

#### Optional method: file descriptor passing

*FIXME*

### Sending nested channels

*FIXME*
