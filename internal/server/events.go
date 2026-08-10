package server

import (
	"encoding/json"
	"fmt"
	"net/http"
	"sync"
	"sync/atomic"
	"time"
)

// fileEvent is deliberately small: clients reload the authoritative file list
// after receiving an event, so a slow client may safely miss intermediate events.
type fileEvent struct {
	Version uint64    `json:"version"`
	Kind    string    `json:"kind"`
	Name    string    `json:"name,omitempty"`
	At      time.Time `json:"at"`
}

type eventBroker struct {
	mu      sync.Mutex
	clients map[chan fileEvent]struct{}
	version atomic.Uint64
}

func newEventBroker() *eventBroker {
	return &eventBroker{clients: make(map[chan fileEvent]struct{})}
}

func (b *eventBroker) publish(kind, name string) {
	event := fileEvent{
		Version: b.version.Add(1),
		Kind:    kind,
		Name:    name,
		At:      time.Now().UTC(),
	}

	b.mu.Lock()
	defer b.mu.Unlock()
	for client := range b.clients {
		select {
		case client <- event:
		default:
			// Events are invalidation signals rather than a durable log. Dropping an
			// event keeps one slow browser from blocking uploads for every client.
		}
	}
}

func (b *eventBroker) subscribe() (<-chan fileEvent, func()) {
	client := make(chan fileEvent, 8)
	b.mu.Lock()
	b.clients[client] = struct{}{}
	b.mu.Unlock()

	return client, func() {
		b.mu.Lock()
		if _, exists := b.clients[client]; exists {
			delete(b.clients, client)
			close(client)
		}
		b.mu.Unlock()
	}
}

func (b *eventBroker) subscriberCount() int {
	b.mu.Lock()
	defer b.mu.Unlock()
	return len(b.clients)
}

func (s *Server) handleEvents(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache, no-transform")
	w.Header().Set("X-Accel-Buffering", "no")

	controller := http.NewResponseController(w)
	events, unsubscribe := s.events.subscribe()
	defer unsubscribe()

	ready := fileEvent{
		Version: s.events.version.Load(),
		Kind:    "ready",
		At:      time.Now().UTC(),
	}
	if err := writeServerEvent(w, ready); err != nil {
		return
	}
	if err := controller.Flush(); err != nil {
		return
	}

	heartbeat := time.NewTicker(15 * time.Second)
	defer heartbeat.Stop()
	for {
		select {
		case <-r.Context().Done():
			return
		case event, ok := <-events:
			if !ok {
				return
			}
			if err := writeServerEvent(w, event); err != nil {
				return
			}
			if err := controller.Flush(); err != nil {
				return
			}
		case <-heartbeat.C:
			if _, err := fmt.Fprint(w, ": keep-alive\n\n"); err != nil {
				return
			}
			if err := controller.Flush(); err != nil {
				return
			}
		}
	}
}

func writeServerEvent(w http.ResponseWriter, event fileEvent) error {
	data, err := json.Marshal(event)
	if err != nil {
		return err
	}
	_, err = fmt.Fprintf(w, "id: %d\nevent: files\ndata: %s\n\n", event.Version, data)
	return err
}
