package engine

import (
	"bytes"
	"fmt"
	"io"
	"io/ioutil"
	"log"
	"net"
	"testing"

	"github.com/docker/libchan/bus"
)

// emptyTestHandler implements the "empty" job test handler
func emptyTestHandler(job *Job) error {
	b, err := ioutil.ReadAll(job.Stdin)
	if err != nil {
		return err
	}
	if len(b) > 0 {
		return fmt.Errorf("Expected 0 byte read")
	}

	return nil
}

// readTestHandler implements the "read" job test handler
func readTestHandler(job *Job) error {
	readBytes := []byte(job.Getenv("Data"))
	b, err := ioutil.ReadAll(job.Stdin)
	if err != nil {
		return err
	}
	if bytes.Compare(b, readBytes) != 0 {
		return fmt.Errorf("Wrong bytes value\n\tExpected: %s\n\tActual:   %s", readBytes, b)
	}

	return nil
}

// writeTestHandler implements the "write" job test handler
func writeTestHandler(job *Job) error {
	log.Printf("Writing bytes: %q", job.Getenv("Data"))
	if _, err := job.Stdout.Write([]byte(job.Getenv("Data"))); err != nil {
		return err
	}

	return nil
}

// combinedTestHandler implements the "combined" job test handler
func combinedTestHandler(job *Job) error {
	if _, err := job.Stdout.Write([]byte(job.Getenv("Data_Stdout"))); err != nil {
		return err
	}
	if _, err := job.Stderr.Write([]byte(job.Getenv("Data_Stderr"))); err != nil {
		return err
	}
	return nil
}

func runEmptyTest(t *testing.T, b *bus.Bus, receiver string) {
	c1, c2 := net.Pipe()
	if err := b.Connect(c1); err != nil {
		t.Fatalf("Error connecting to bus: %s", err)
	}
	engine, err := NewNetEngine(c2, "empty-test")
	if err != nil {
		t.Fatalf("Error creating engine: %s", err)
	}

	job, err := engine.Job("empty")
	if err != nil {
		t.Fatalf("Error creating job")
	}
	if err := job.Run(); err != nil {
		t.Fatalf("Error running job: %s", err)
	}
}

func runReadTest(t *testing.T, b *bus.Bus, receiver string) {
	c1, c2 := net.Pipe()
	if err := b.Connect(c1); err != nil {
		t.Fatalf("Error connecting to bus: %s", err)
	}
	engine, err := NewNetEngine(c2, "read-test")
	if err != nil {
		t.Fatalf("Error creating engine: %s", err)
	}

	job, err := engine.Job("read")
	if err != nil {
		t.Fatalf("Error creating job")
	}
	job.Setenv("Data", "read bytes")
	writer, err := job.StdinPipe()
	if err != nil {
		t.Fatalf("Error creating pipe: %s", err)
	}
	errChan := make(chan error)
	go func() {
		fmt.Fprintf(writer, "read bytes")
		errChan <- writer.Close()
	}()
	if err := job.Run(); err != nil {
		t.Fatalf("Error running job: %s", err)
	}
	if err := <-errChan; err != nil {
		t.Fatalf("Error closing writer: %s", err)
	} else if t.Failed() {
		t.FailNow()
	}
}

func runWriteTest(t *testing.T, b *bus.Bus, receiver string) {
	c1, c2 := net.Pipe()
	if err := b.Connect(c1); err != nil {
		t.Fatalf("Error connecting to bus: %s", err)
	}
	engine, err := NewNetEngine(c2, "write-test")
	if err != nil {
		t.Fatalf("Error creating engine: %s", err)
	}

	job, err := engine.Job("write")
	if err != nil {
		t.Fatalf("Error creating job")
	}
	job.Setenv("Data", "write bytes")
	reader, err := job.StdoutPipe()
	if err != nil {
		t.Fatalf("Error creating pipe: %s", err)
	}
	errChan := make(chan error)
	go func() {
		b, err := ioutil.ReadAll(reader)
		if err != nil {
			errChan <- err
			return
		}
		expected := []byte("write bytes")
		if bytes.Compare(b, expected) != 0 {
			t.Errorf("Unexpected written bytes\n\tExpected: %s\n\tActual:   %s", expected, b)
		}
		errChan <- reader.Close()
	}()
	if err := job.Run(); err != nil {
		t.Fatalf("Error running job: %s", err)
	}
	if err := <-errChan; err != nil {
		t.Fatalf("Error closing writer: %s", err)
	} else if t.Failed() {
		t.FailNow()
	}
}

func runCombinedTest(t *testing.T, b *bus.Bus, receiver string) {
	c1, c2 := net.Pipe()
	if err := b.Connect(c1); err != nil {
		t.Fatalf("Error connecting to bus: %s", err)
	}
	engine, err := NewNetEngine(c2, "combined-test")
	if err != nil {
		t.Fatalf("Error creating engine: %s", err)
	}

	job, err := engine.Job("combined")
	if err != nil {
		t.Fatalf("Error creating job")
	}
	job.Setenv("Data_Stdout", "(stdout)combined bytes")
	job.Setenv("Data_Stderr", "(stderr)combined bytes")
	r1, err := job.StdoutPipe()
	if err != nil {
		t.Fatalf("Error creating pipe: %s", err)
	}
	r2, err := job.StderrPipe()
	if err != nil {
		t.Fatalf("Error creating pipe: %s", err)
	}
	errChan1 := make(chan error)
	errChan2 := make(chan error)
	readStream := func(r io.ReadCloser, expected []byte, errChan chan error) {
		if b, err := ioutil.ReadAll(r); err != nil {
			errChan <- err
			return
		} else if bytes.Compare(b, expected) != 0 {
			t.Errorf("Unexpected written bytes\n\tExpected: %s\n\tActual:   %s", expected, b)
		}
		errChan <- r.Close()
	}

	go readStream(r1, []byte("(stdout)combined bytes"), errChan1)
	go readStream(r2, []byte("(stderr)combined bytes"), errChan2)

	if err := job.Run(); err != nil {
		t.Fatalf("Error running job: %s", err)
	} else if t.Failed() {
		t.FailNow()
	}
	if err := <-errChan1; err != nil {
		t.Fatalf("Error reading stdout: %s", err)
	}
	if err := <-errChan2; err != nil {
		t.Fatalf("Error reading stderr: %s", err)
	}
	if t.Failed() {
		t.FailNow()
	}
}

func runEngineTest(t *testing.T, b *bus.Bus, name string, eng *Engine) {
	eng.RegisterJob("empty", emptyTestHandler)
	eng.RegisterJob("read", readTestHandler)
	eng.RegisterJob("write", writeTestHandler)
	eng.RegisterJob("combined", combinedTestHandler)

	runEmptyTest(t, b, name)
	runReadTest(t, b, name)
	runWriteTest(t, b, name)
	runCombinedTest(t, b, name)
}

func TestLocalEngine(t *testing.T) {
	b := bus.NewBus()
	eng, err := NewLocalEngine(b, "test-local")
	if err != nil {
		t.Fatalf("Error creating engine: %s", err)
	}

	runEngineTest(t, b, "test-local", eng)
}

func TestNetEngine(t *testing.T) {
	b := bus.NewBus()

	// Create pipe
	c1, c2 := net.Pipe()
	if err := b.Connect(c1); err != nil {
		t.Fatalf("Error connecting to bus: %s", err)
	}

	eng, err := NewNetEngine(c2, "test-net-engine")
	if err != nil {
		t.Fatalf("Error creating engine: %s", err)
	}

	runEngineTest(t, b, "test-net-engine", eng)
}
