# libchan protocol specification

Extreme portability is a key design goal of libchan.

This document specifies the libchan protocol to allow multiple implementations
to co-exist with full interoperability.

## Version

0.2.0

## Authors

Solomon Hykes <solomon@docker.com>
Derek McGowan <derek@docker.com>
Matteo Collina <matteo.collina@gmail.com>

## Status

This specification is nearing a stable release. The protocol still may change in
reverse-incompatible ways.

## Terminology

### Channel

A `channel` is an object which allows 2 concurrent programs to communicate with
each other. The semantics of a libchan channel are very similar (but not
identical) to those of Go's native channels. A channel may be used
synchronously, but do not support synchronization primitives such as Go's
channel select semantics.

A channel has 2 ends: a `Sender` end and a `Receiver` end. The Sender can send
messages and close the channel. The Receiver can receive messages. Messages
arrive in the same order they were sent.

A channel is uni-directional: messages can only flow in one direction. So
channels are more similar to pipes than to sockets.

### Message

A message is a discrete packet of data which can be sent on a channel. The
protocol defines which data types can be carried sent as a message, and how
transports should encode and decode them. A message is similar to a JSON object
containing [custom types](#custom-types) to represent channels and byte streams.

### Byte stream

A byte stream is an object which implements raw IO with `Read`, `Write` and
`Close` methods. Typical byte streams are text files, network sockets, memory
buffers, pipes, and so on. A byte stream may either be read only, write only, or
full duplex.

One distinct characteristic of libchan is that it can encode byte streams as
first class fields in a message, alongside more basic types like integers or
strings.

### Nesting

Libchan supports nesting. This means that a libchan message can include a
channel, which itself can be used to send and receive messages, and so on all
the way down.

Nesting is a fundamental property of libchan.

## Underlying transport

The libchan protocol requires a reliable, 2-way byte stream with support for
multiplexing as a transport. The underlying byte stream protocol is abstracted
to the libchan protocol through a simple multiplexed stream interface which may
use SPDY/3.1, HTTP/2, or SSH over over TCP connections, unix stream sockets and
TLS sessions.

When a reliable stream transport is not available but a non-multiplexed
connection is available, a multiplexing protocol (such as SPDY or another
simple multiplexing protocol) may be done on top of the existing connection.
This also makes using websockets as an underlying byte stream transport
possible, which allows exposing a libchan endpoint at an HTTP/1 url.

## Authentication and encryption

Libchan can optionally use TLS to authenticate and encrypt communications. After
the initial handshake and protocol negotiation, the TLS session is simply used
as the transport for the libchan multiplexed stream provider.

## Stream Provider

Libchan uses a stream provider to establish new channels and byte streams over
an underlying byte stream. The stream provider must be able to send
headers when creating new streams and retrieve headers for remotely created
streams.

### Headers
The stream provider must support sending key-value headers on stream creation.
 
- *"libchan-ref"* - String representation of a unique 64 bit integer identifier
for the established stream.
- *"libchan-parent-ref"* - String representation of a unique 64 bit integer 
identifier for parent of the established stream. (see *"Sending nested
channels"* and *"Sending byte streams"*)
- *"content-type"* - MIME type for data being sent on the stream.
- *"accept"* - Content types which are acceptable to receive on the stream.
- *":status"* - An HTTP status code sent with the stream reply.

### MIME types
- `application/vnd.libchan.send+msgpack5` - Encoded message stream in msgpack5
- `application/vnd.libchan.recv+msgpack5` - Encoded response stream in msgpack5
- `application/vnd.libchan.send+cbor` - Encoded message stream in cbor
- `application/vnd.libchan.recv+cbor` - Encoded response stream in cbor
- `application/octet-stream` - Byte stream

### Streams
The stream provider provides the functionality for creating new streams as
well as accepting streams created remotely.  A stream is create with
a set of headers and an accepted stream has a method for returning the
headers. Closing a stream must put the stream in a half-closed state and
not allow anymore data to be written. If the remote side has already 
closed, the stream is fully closed. Reseting a stream forces the stream
into a fully closed state and should only be used in error cases.
Resetting does not give the remote a chance to finish sending data and
cleanly close.

## Stream identifiers
Libchan creates a unique identifier for every stream created by the stream
provider. The identifiers are integer values and should never be reused.
The identifier is only unique to a given endpoint, meaning both sides of a
connection may have the same identifier for two different streams. The
identifiers received from the remote endpoint should only be used to reference
streams from that endpoint, and never streams created locally. A remote
endpoint's stream identifier should never be sent in a libchan message.
To send a stream created remotely, a new stream should be created
locally, copied from the remote stream, and the identifier to the local copy
should be used.

## Control protocol

Once 2 libchan endpoints have established a multiplexed stream session, they
communicate with the following control protocol.

### Top-level channels

Each libchan session may carry multiple concurrent channels, in both directions,
using stream multiplexing. Each libchan channel is implemented by an underlying
stream.

To use a libchan session, either endpoint may initiate new channels, wait for
its peer to initiate new channels, or a combination of both. Channels initiated
in this way are called *top-level channels*.

* To initiate a new top-level channel, either endpoint may initiate a new
stream, then start sending messages to it (see *"sending messages"*).

* The endpoint initiating a top-level channel MAY NOT allow the application to
receive messages from it and MUST interpret inbound messages received on that
stream as an ack or error message.

* The endpoint initiating the channel must create a unique identifier for the
channel and include the value in the *"libchan-ref"* header when creating
the new stream.

* When an endpoint receives a new stream without the header
*"libchan-parent-ref"*, it MUST interpret the stream as an inbound top-level
channel and queue a new `Receiver` channel to pass to the application.

* The endpoint receiving a top-level channel MAY NOT allow the application to
send messages to it.


### Sending messages on a channel

Once a stream is initiated, it can be used as a channel, with the initiating
endpoint holding the `Sender` end of the channel, and the recipient endpoint
holding the `Receiver` end.

* To send a message, the sender MUST encode it using the message encoding format
(see *"message encoding"*), and send the encoded message on the corresponding
stream.

* When receiving a data on any active stream, the receiver MUST decode it using
the same message encoding format. If the decoding fails, the receiver MUST close
the underlying stream, and future calls to `Receive` on that channel MUST return
an error.

* Every send message should have a corresponding receive of an ack message from
the peer. The ack message is a map with at least one field named `code`. The
`code` field should have an integer value, with an a value of zero considered
a successful ack and non-zero as an error. An error should be accompanied with
an additional `message` field of type string, describing the error. If an error
is received, the sender should close and pass an error to the application.

### Sending nested channels

* When sending a nested channel, in addition to the *"libchan-ref"* header, the
*"libchan-parent-ref"* header must be sent identifying the channel used to
create the nested channel.

### Closing a channel

The endpoint which holds the send side of a channel MAY close it which will
half-close the stream. The receive side should respond by closing the stream,
putting the stream in a fully closed state. Any send or receive call from the
application after close should return an error.

When an error is received on a channel, the underlying stream should be
closed by both ends.

### Sending byte streams

Libchan messages support a special type called *byte streams*. Unlike regular
types like integers or strings, byte streams are not fully encoded in the
message. Instead, the message encodes a *reference* which allows the receiving
endpoint to reconstitute the byte stream after receiving the message, and pass
it to the application.

Byte streams use the raw stream returned by the stream provider.

* When encoding a message including 1 or more byte stream values, the sender
MUST assign to each value an identifier unique to the session, and store these
identifiers for future use.

* After sending the encoded message, the sender MUST create 1 new stream for
each byte stream value in the message.

* Each of new stream MUST include a header with the key *"libchan-ref"* and
a value of the identifier of the corresponding byte stream. It must also
include a header with the key *"libchan-parent-ref"* and a value of the
stream identifier for the message channel which created the byte stream.

Conversely, the receiver must follow this protocol:

* When decoding a message including 1 or more byte stream values, the receiver
MUST store the unique identifier of each value in a session-wide table of
pending byte streams. It MAY then immediately pass the decoded message to the
application.

* The sender SHOULD cap the size of its pending byte streams table to a
reasonable value. It MAY make that value configurable by the application. If it
receives a message with 1 or more byte stream references, and the table
is full, the sender MAY suspend processing of the message until there is room in
the table.

* When receiving new streams which include the header key "*libchan-ref*", the
receiver MUST lookup that header value in the table of pending byte streams. If
the value is registered in the table, that stream MUST be passed to the
application.

On either end, once the stream for a byte-stream value is established, it MUST
be exposed to the application as follows:

* After sending each of those streams, each write operation by the application
to a byte-stream field MUST trigger the sending of a single data frame on the
corresponding stream.

* Each read operation by the application from a byte-stream field MUST yield
the content of the next data frame received on the corresponding stream. If the
reading end of the stream is closed, the read operation MUST yield EOF.

* A close operation by the application on the a byte-stream field MUST trigger
the closing of the writing end of the corresponding SPDY stream.

## Message encoding
A message may be any type which supported by the libchan encoder.  A libchan
message encoder must support encoding raw byte stream types as well as channels.
In addition to the libchan data types, time must also be encoded as a custom
type to increase portability of the protocol.

Currently supported message encoders are msgpack5 and soon CBOR.

### Custom Types
Each custom type defines a type code and the byte layout to represent that type.
Directions of descriptions are from the point of view of the endpoint encoding.
All multi-byte integers are encoded big endian. The length of bytes of the
encoded value will be provided by the encoding format, allowing integer values
to be variable length.

| Type | Code | Byte Layout|
|---|---|---|
| Duplex Byte Stream | 1 | 4 or 8 byte integer identifier |
| Inbound Byte Stream | 2 | 4 or 8 byte integer identifier |
| Outbound Byte Stream | 3 | 4 or 8 byte integer identifier |
| Inbound channel | 4 | 4 or 8 byte integer identifier |
| Outbound channel | 5 | 4 or 8 byte integer identifier |
| time | 6 | 8 byte integer seconds + 4 byte integer nanoseconds  |

## Version History

0.2.0
 - Define stream provider to support multiple multiplexing protocols
 - Support CBOR in addition to Msgpack for channel message encoding
 - Add extended type codes definition
 - Require byte-streams to send *"libchan-parent-ref"*
 - Allow byte-streams as duplex or half-duplex
 - Add channel synchronization through ack definition
 - Add channel errors
 - Update description of relationship to Go channels
 - Add MIME types for streams

0.1.0
 - Initial specification
 - Message channels
 - Nested message channels
 - Duplex byte streams
 - Msgpack channel message encoding
 - SPDY stream multiplexing
