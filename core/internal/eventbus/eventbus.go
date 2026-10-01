// Package eventbus is the in-process publish/subscribe fabric used by the
// core. Slow subscribers are dropped rather than allowed to block the voice
// path (SRS 5.4).
package eventbus

import (
	"sync"

	"github.com/yui-companion/core/internal/model"
)

// Bus fans out events to topic subscribers.
type Bus struct {
	mu      sync.RWMutex
	subs    map[string][]*subscription
	dropped uint64
}

type subscription struct {
	topic string
	ch    chan model.Event
}

func New() *Bus { return &Bus{subs: make(map[string][]*subscription)} }

// Subscribe returns a receive-only channel and a cancel function.
func (b *Bus) Subscribe(topic string, buffer int) (<-chan model.Event, func()) {
	if buffer <= 0 {
		buffer = 32
	}
	s := &subscription{topic: topic, ch: make(chan model.Event, buffer)}
	b.mu.Lock()
	b.subs[topic] = append(b.subs[topic], s)
	b.mu.Unlock()
	return s.ch, func() { b.unsubscribe(s) }
}

func (b *Bus) unsubscribe(s *subscription) {
	b.mu.Lock()
	defer b.mu.Unlock()
	list := b.subs[s.topic]
	for i, cur := range list {
		if cur == s {
			b.subs[s.topic] = append(list[:i], list[i+1:]...)
			close(s.ch)
			return
		}
	}
}

// Publish delivers an event without blocking. Events for full subscribers are
// counted and discarded.
func (b *Bus) Publish(topic string, ev model.Event) {
	b.mu.RLock()
	list := append([]*subscription(nil), b.subs[topic]...)
	all := append([]*subscription(nil), b.subs["*"]...)
	b.mu.RUnlock()
	for _, s := range append(list, all...) {
		select {
		case s.ch <- ev:
		default:
			b.mu.Lock()
			b.dropped++
			b.mu.Unlock()
		}
	}
}

// Dropped reports how many events were discarded due to backpressure.
func (b *Bus) Dropped() uint64 {
	b.mu.RLock()
	defer b.mu.RUnlock()
	return b.dropped
}
