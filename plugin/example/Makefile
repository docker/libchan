all: build

plugin1:
	go build plugin1.go

server:
	go build server.go

clean:
	rm -f ./plugin1 ./server

build: plugin1 server

run: build
	./server

