package api

import "sync"

// EventBus implements the Events interface with a thread-safe
// pub/sub model. Handlers are called synchronously in the
// goroutine that calls Emit.
type EventBus struct {
	mu     sync.RWMutex
	subs   map[string][]subscription
	nextID uint64
}

type subscription struct {
	id      uint64
	handler EventHandler
}

// NewEventBus creates a new event bus ready for use.
func NewEventBus() *EventBus {
	return &EventBus{subs: make(map[string][]subscription)}
}

// Subscribe registers a handler for the given event type and returns
// an unsubscribe function that removes it.
func (b *EventBus) Subscribe(eventType string, handler EventHandler) UnsubscribeFunc {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.nextID++
	id := b.nextID
	b.subs[eventType] = append(b.subs[eventType], subscription{id: id, handler: handler})
	return func() {
		b.mu.Lock()
		defer b.mu.Unlock()
		subs := b.subs[eventType]
		for i, s := range subs {
			if s.id == id {
				b.subs[eventType] = append(subs[:i], subs[i+1:]...)
				return
			}
		}
	}
}

// Emit dispatches an event to all subscribers of its type.
// Handlers are called synchronously in the caller's goroutine.
func (b *EventBus) Emit(event Event) {
	b.mu.RLock()
	handlers := make([]EventHandler, 0, len(b.subs[event.Type]))
	for _, s := range b.subs[event.Type] {
		handlers = append(handlers, s.handler)
	}
	b.mu.RUnlock()
	for _, h := range handlers {
		h(event)
	}
}
