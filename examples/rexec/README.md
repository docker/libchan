# rexec

rexec is a sample application demonstrating libchan nested channels and
bytestreams through remote executation calls.  This is a minimal
implementation to demonstrate the usage of libchan.

### Usage

Server
~~~~
$ cd rexec_server
$ go build .
$ ./rexec_server
~~~~

Client
~~~~
$ go build .
$ ./rexec /bin/echo "hello"
hello
$ ./rexec /bin/sh -c "exit 4"
$ echo $?
4
~~~~

### Usage with TLS
Server
~~~~
$ TLS_CERT=./cert.pem TLS_KEY=./key.pem ./rexec_server
~~~~

Client
~~~~
$ USE_TLS=true ./rexec /bin/echo "hello"
hello
~~~~
