package spdy

import (
	"io"
	"os"
	"runtime/pprof"
	"testing"
	"time"

	"github.com/docker/libchan"
)

type SimpleStruct struct {
	Value int
}

func SimpleStructFuncs(b *testing.B) (BenchMessageSender, BenchMessageReceiver) {
	sender := func(i int, s libchan.Sender) {
		if err := s.Send(&SimpleStruct{i}); err != nil {
			b.Fatalf("Error sending message: %s", err)
		}
	}
	receiver := func(i int, r libchan.Receiver) {
		var v SimpleStruct
		if err := r.Receive(&v); err != nil {
			b.Fatalf("Error receiving: %s", err)
		}
		if v.Value != i {
			b.Fatalf("Unexpected value received: %d != %d", v, i)
		}
	}
	return sender, receiver
}

func BenchmarkSimpleCopy(b *testing.B) {
	sender, receiver := SimpleStructFuncs(b)
	SpawnProxyBench(b, sender, receiver)
}

func BenchmarkSimpleLocalProxy(b *testing.B) {
	sender, receiver := SimpleStructFuncs(b)
	SpawnLocalProxyBench(b, sender, receiver)
}

func BenchmarkSimpleSpdyProxy(b *testing.B) {
	sender, receiver := SimpleStructFuncs(b)
	SpawnSpdyProxyBench(b, sender, receiver)
}

func BenchmarkSimpleNoProxy(b *testing.B) {
	sender, receiver := SimpleStructFuncs(b)
	SpawnNoProxyBench(b, sender, receiver)
}

func BenchmarkSimpleLocalPipe(b *testing.B) {
	sender, receiver := SimpleStructFuncs(b)
	SpawnLocalPipeBench(b, sender, receiver)
}

type ComplexStruct struct {
	Value  int
	Sender libchan.Sender
	Reader io.Reader
}

func ComplexStructFuncs(b *testing.B) (BenchMessageSender, BenchMessageReceiver) {
	sender := func(i int, s libchan.Sender) {
		r, _ := io.Pipe()
		_, send := libchan.Pipe()
		v := &ComplexStruct{
			Value:  i,
			Sender: send,
			Reader: r,
		}
		if err := s.Send(v); err != nil {
			b.Fatalf("Error sending message: %s", err)
		}
	}
	receiver := func(i int, r libchan.Receiver) {
		var v ComplexStruct
		if err := r.Receive(&v); err != nil {
			b.Fatalf("Error receiving: %s", err)
		}
		if v.Value != i {
			b.Fatalf("Unexpected value received: %d != %d", v.Value, i)
		}
	}
	return sender, receiver
}

func BenchmarkComplexCopy(b *testing.B) {
	sender, receiver := ComplexStructFuncs(b)
	SpawnProxyBench(b, sender, receiver)
}

func BenchmarkComplexLocalProxy(b *testing.B) {
	sender, receiver := ComplexStructFuncs(b)
	SpawnLocalProxyBench(b, sender, receiver)
}

func BenchmarkComplexSpdyProxy(b *testing.B) {
	sender, receiver := ComplexStructFuncs(b)
	SpawnSpdyProxyBench(b, sender, receiver)
}

func BenchmarkComplexNoProxy(b *testing.B) {
	sender, receiver := ComplexStructFuncs(b)
	SpawnNoProxyBench(b, sender, receiver)
}

func BenchmarkComplexLocalPipe(b *testing.B) {
	sender, receiver := ComplexStructFuncs(b)
	SpawnLocalPipeBench(b, sender, receiver)
}

type BenchMessageSender func(int, libchan.Sender)
type BenchMessageReceiver func(int, libchan.Receiver)

func BenchProxy(b *testing.B, c chan bool, sender libchan.Sender, receiver libchan.Receiver, count int) {
	defer close(c)
	n, err := libchan.Copy(sender, receiver)
	if err != nil {
		b.Errorf("Error proxying: %s", err)
	}
	err = sender.Close()
	if err != nil {
		b.Errorf("Error closing sender: %s", err)
	}
	if n != count {
		b.Errorf("Wrong proxy count\n\tExpected: %d\n\tActual: %d", count, n)
	}
}

func BenchClient(b *testing.B, c chan bool, sender libchan.Sender, sendFunc BenchMessageSender, count int) {
	defer close(c)
	for i := 0; i < count; i++ {
		sendFunc(i, sender)
	}
	err := sender.Close()
	if err != nil {
		b.Fatalf("Error closing sender: %s", err)
	}
}

func BenchServer(b *testing.B, c chan bool, receiver libchan.Receiver, recvFunc BenchMessageReceiver, count int) {
	defer close(c)
	for i := 0; i < count; i++ {
		recvFunc(i, receiver)
	}
}

func SpawnProxyBench(b *testing.B, sender BenchMessageSender, receiver BenchMessageReceiver) {
	endClient := make(chan bool)
	endServer := make(chan bool)
	endProxy := make(chan bool)

	receiver1, sender1, err := testPipe()
	if err != nil {
		b.Fatalf("Error creating pipe: %s", err)
	}
	receiver2, sender2, err := testPipe()
	if err != nil {
		b.Fatalf("Error creating pipe: %s", err)
	}

	go BenchProxy(b, endProxy, sender2, receiver1, b.N)
	go BenchClient(b, endClient, sender1, sender, b.N)
	go BenchServer(b, endServer, receiver2, receiver, b.N)

	timeout := time.After(time.Duration(b.N+1) * 50 * time.Millisecond)

	for endClient != nil || endServer != nil || endProxy != nil {
		select {
		case <-endProxy:
			if b.Failed() {
				b.Fatal("Proxy failed")
			}
			endProxy = nil
		case <-endClient:
			if b.Failed() {
				b.Fatal("Client failed")
			}
			endClient = nil
		case <-endServer:
			if b.Failed() {
				b.Fatal("Server failed")
			}
			endServer = nil
		case <-timeout:
			if DumpStackOnTimeout {
				pprof.Lookup("goroutine").WriteTo(os.Stdout, 1)
			}
			b.Fatal("Timeout")
		}
	}
}

func SpawnLocalProxyBench(b *testing.B, sender BenchMessageSender, receiver BenchMessageReceiver) {
	endClient := make(chan bool)
	endServer := make(chan bool)
	endProxy1 := make(chan bool)
	endProxy2 := make(chan bool)

	receiver1, sender1, err := testPipe()
	if err != nil {
		b.Fatalf("Error creating pipe: %s", err)
	}
	receiver2, sender2 := libchan.Pipe()
	receiver3, sender3, err := testPipe()
	if err != nil {
		b.Fatalf("Error creating pipe: %s", err)
	}

	go BenchProxy(b, endProxy1, sender2, receiver1, b.N)
	go BenchProxy(b, endProxy2, sender3, receiver2, b.N)
	go BenchClient(b, endClient, sender1, sender, b.N)
	go BenchServer(b, endServer, receiver3, receiver, b.N)

	timeout := time.After(time.Duration(b.N+1) * 100 * time.Millisecond)

	for endClient != nil || endServer != nil || endProxy1 != nil || endProxy2 != nil {
		select {
		case <-endProxy1:
			if b.Failed() {
				b.Fatal("Proxy failed")
			}
			endProxy1 = nil
		case <-endProxy2:
			if b.Failed() {
				b.Fatal("Proxy failed")
			}
			endProxy2 = nil
		case <-endClient:
			if b.Failed() {
				b.Fatal("Client failed")
			}
			endClient = nil
		case <-endServer:
			if b.Failed() {
				b.Fatal("Server failed")
			}
			endServer = nil
		case <-timeout:
			if DumpStackOnTimeout {
				pprof.Lookup("goroutine").WriteTo(os.Stdout, 1)
			}
			b.Fatal("Timeout")
		}
	}
}

func SpawnSpdyProxyBench(b *testing.B, sender BenchMessageSender, receiver BenchMessageReceiver) {
	endClient := make(chan bool)
	endServer := make(chan bool)
	endProxy1 := make(chan bool)
	endProxy2 := make(chan bool)

	receiver1, sender1, err := testPipe()
	if err != nil {
		b.Fatalf("Error creating pipe: %s", err)
	}
	receiver2, sender2, err := testPipe()
	if err != nil {
		b.Fatalf("Error creating pipe: %s", err)
	}
	receiver3, sender3, err := testPipe()
	if err != nil {
		b.Fatalf("Error creating pipe: %s", err)
	}

	go BenchProxy(b, endProxy1, sender2, receiver1, b.N)
	go BenchProxy(b, endProxy2, sender3, receiver2, b.N)
	go BenchClient(b, endClient, sender1, sender, b.N)
	go BenchServer(b, endServer, receiver3, receiver, b.N)

	timeout := time.After(time.Duration(b.N+1) * 200 * time.Millisecond)

	for endClient != nil || endServer != nil || endProxy1 != nil || endProxy2 != nil {
		select {
		case <-endProxy1:
			if b.Failed() {
				b.Fatal("Proxy failed")
			}
			endProxy1 = nil
		case <-endProxy2:
			if b.Failed() {
				b.Fatal("Proxy failed")
			}
			endProxy2 = nil
		case <-endClient:
			if b.Failed() {
				b.Fatal("Client failed")
			}
			endClient = nil
		case <-endServer:
			if b.Failed() {
				b.Fatal("Server failed")
			}
			endServer = nil
		case <-timeout:
			if DumpStackOnTimeout {
				pprof.Lookup("goroutine").WriteTo(os.Stdout, 1)
			}
			b.Fatal("Timeout")
		}
	}
}

func SpawnNoProxyBench(b *testing.B, sender BenchMessageSender, receiver BenchMessageReceiver) {
	endClient := make(chan bool)
	endServer := make(chan bool)

	receiver1, sender1, err := testPipe()
	if err != nil {
		b.Fatalf("Error creating pipe: %s", err)
	}

	go BenchClient(b, endClient, sender1, sender, b.N)
	go BenchServer(b, endServer, receiver1, receiver, b.N)

	timeout := time.After(time.Duration(b.N+1) * 50 * time.Millisecond)

	for endClient != nil || endServer != nil {
		select {
		case <-endClient:
			if b.Failed() {
				b.Fatal("Client failed")
			}
			endClient = nil
		case <-endServer:
			if b.Failed() {
				b.Fatal("Server failed")
			}
			endServer = nil
		case <-timeout:
			if DumpStackOnTimeout {
				pprof.Lookup("goroutine").WriteTo(os.Stdout, 1)
			}
			b.Fatal("Timeout")
		}
	}
}

func SpawnLocalPipeBench(b *testing.B, sender BenchMessageSender, receiver BenchMessageReceiver) {
	endClient := make(chan bool)
	endServer := make(chan bool)

	receiver1, sender1 := libchan.Pipe()

	go BenchClient(b, endClient, sender1, sender, b.N)
	go BenchServer(b, endServer, receiver1, receiver, b.N)

	timeout := time.After(time.Duration(b.N+1) * 50 * time.Millisecond)

	for endClient != nil || endServer != nil {
		select {
		case <-endClient:
			if b.Failed() {
				b.Fatal("Client failed")
			}
			endClient = nil
		case <-endServer:
			if b.Failed() {
				b.Fatal("Server failed")
			}
			endServer = nil
		case <-timeout:
			if DumpStackOnTimeout {
				pprof.Lookup("goroutine").WriteTo(os.Stdout, 1)
			}
			b.Fatal("Timeout")
		}
	}
}
