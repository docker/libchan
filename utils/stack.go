package utils

import (
	"container/list"
	"fmt"
	"github.com/docker/libchan"
	"strings"
	"sync"
)

// StackSender forwards libchan messages to a dynamic list of backend receivers.
// New backends are stacked on top. When a message is sent, each backend is
// tried until one succeeds. Any failing backends encountered along the way
// are removed from the queue.
type StackSender struct {
	stack *list.List
	l     sync.RWMutex
}

func NewStackSender() *StackSender {
	stack := list.New()
	return &StackSender{
		stack: stack,
	}
}

func (s *StackSender) Send(msg *libchan.Message) (ret libchan.Receiver, err error) {
	completed := s.walk(func(h libchan.Sender) (ok bool) {
		ret, err = h.Send(msg)
		fmt.Printf("[stacksender] sending %v to %#v returned %v\n", msg, h, err)
		if err == nil {
			return true
		}
		return false
	})
	// If walk was completed, it means we didn't find a valid handler
	if !completed {
		return ret, err
	}
	// Silently drop messages if no valid backend is available.
	return libchan.NopSender{}.Send(msg)
}

func (s *StackSender) Add(dst libchan.Sender) *StackSender {
	s.l.Lock()
	defer s.l.Unlock()
	prev := &StackSender{
		stack: list.New(),
	}
	prev.stack.PushFrontList(s.stack)
	fmt.Printf("[ADD] prev %#v\n", prev)
	s.stack.PushFront(dst)
	return prev
}

func (s *StackSender) Close() error {
	s.walk(func(h libchan.Sender) bool {
		h.Close()
		// remove all handlers
		return false
	})
	return nil
}

func (s *StackSender) _walk(f func(*list.Element) bool) bool {
	var e *list.Element
	s.l.RLock()
	e = s.stack.Front()
	s.l.RUnlock()
	for e != nil {
		fmt.Printf("[StackSender.Walk] %v\n", e.Value.(libchan.Sender))
		s.l.RLock()
		next := e.Next()
		s.l.RUnlock()
		cont := f(e)
		if !cont {
			return false
		}
		e = next
	}
	return true
}

func (s *StackSender) walk(f func(libchan.Sender) bool) bool {
	return s._walk(func(e *list.Element) bool {
		ok := f(e.Value.(libchan.Sender))
		if ok {
			// Found a valid handler. Stop walking.
			return false
		}
		// Invalid handler: remove.
		s.l.Lock()
		s.stack.Remove(e)
		s.l.Unlock()
		return true
	})
}

func (s *StackSender) Len() int {
	s.l.RLock()
	defer s.l.RUnlock()
	return s.stack.Len()
}

func (s *StackSender) String() string {
	var parts []string
	s._walk(func(e *list.Element) bool {
		parts = append(parts, fmt.Sprintf("%v", e.Value.(libchan.Sender)))
		return true
	})
	return fmt.Sprintf("%d:[%s]", len(parts), strings.Join(parts, "->"))
}
