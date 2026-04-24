package agent

import "sync"

// EventType identifies what happened in the agent loop.
type EventType string

const (
	EventAgentStart   EventType = "agent_start"
	EventTurnStart    EventType = "turn_start"
	EventTokens       EventType = "tokens"       // streaming content tokens
	EventThinking     EventType = "thinking"      // streaming thinking tokens
	EventToolStart    EventType = "tool_start"
	EventToolUpdate   EventType = "tool_update"
	EventToolEnd      EventType = "tool_end"
	EventTurnEnd      EventType = "turn_end"
	EventAgentEnd     EventType = "agent_end"
	EventError        EventType = "error"
)

// Event carries a typed payload from the agent loop to subscribers.
type Event struct {
	Type    EventType
	Payload any
}

// Payloads — subscribers type-assert Payload based on Type.

type PayloadTokens struct{ Token string }
type PayloadThinking struct{ Token string }
type PayloadToolStart struct{ Name string; Args map[string]any }
type PayloadToolUpdate struct{ Name string; Output string }
type PayloadToolEnd struct{ Name string; Result string; IsError bool }
type PayloadError struct{ Err error }
type PayloadTurnEnd struct{ HasMore bool } // HasMore = model wants another turn

// Bus is a fan-out event publisher. Subscribers receive all events on a channel.
// Safe for concurrent use. The agent loop calls Publish; UI layers subscribe.
type Bus struct {
	mu   sync.RWMutex
	subs []chan Event
}

// Subscribe returns a receive-only channel and an unsubscribe function.
// The channel is buffered (256) so a slow subscriber doesn't stall the agent.
func (b *Bus) Subscribe() (<-chan Event, func()) {
	ch := make(chan Event, 256)
	b.mu.Lock()
	b.subs = append(b.subs, ch)
	b.mu.Unlock()

	unsub := func() {
		b.mu.Lock()
		defer b.mu.Unlock()
		for i, s := range b.subs {
			if s == ch {
				b.subs = append(b.subs[:i], b.subs[i+1:]...)
				close(ch)
				return
			}
		}
	}
	return ch, unsub
}

// Publish delivers an event to all current subscribers.
// Non-blocking: if a subscriber's buffer is full the event is dropped for that subscriber.
func (b *Bus) Publish(e Event) {
	b.mu.RLock()
	defer b.mu.RUnlock()
	for _, ch := range b.subs {
		select {
		case ch <- e:
		default:
		}
	}
}
