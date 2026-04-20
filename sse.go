package main

import (
	"fmt"
	"sync"
)

type EventType string

const (
	EventTrackpoint EventType = "trackpoint"
	EventEntry      EventType = "entry"
)

type SSEEvent struct {
	Type EventType
	Data string
}

type SSEBroker struct {
	mu          sync.RWMutex
	subscribers map[string]map[chan SSEEvent]struct{} // tripID -> set of channels
}

func newSSEBroker() *SSEBroker {
	return &SSEBroker{
		subscribers: make(map[string]map[chan SSEEvent]struct{}),
	}
}

func (b *SSEBroker) Subscribe(tripID string) chan SSEEvent {
	b.mu.Lock()
	defer b.mu.Unlock()

	ch := make(chan SSEEvent, 16)
	if b.subscribers[tripID] == nil {
		b.subscribers[tripID] = make(map[chan SSEEvent]struct{})
	}
	b.subscribers[tripID][ch] = struct{}{}
	return ch
}

func (b *SSEBroker) Unsubscribe(tripID string, ch chan SSEEvent) {
	b.mu.Lock()
	defer b.mu.Unlock()

	if subs, ok := b.subscribers[tripID]; ok {
		delete(subs, ch)
		if len(subs) == 0 {
			delete(b.subscribers, tripID)
		}
	}
	close(ch)
}

func (b *SSEBroker) Publish(tripID string, event SSEEvent) {
	b.mu.RLock()
	defer b.mu.RUnlock()

	subs, ok := b.subscribers[tripID]
	if !ok {
		return
	}

	for ch := range subs {
		select {
		case ch <- event:
		default:
			// Drop event for slow clients
		}
	}
}

func formatSSE(event EventType, data string) string {
	return fmt.Sprintf("event: %s\ndata: %s\n\n", event, data)
}
