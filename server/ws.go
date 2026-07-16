package server

import (
	"context"
	"encoding/json"
	"log"
	"net/http"
	"sync"
	"time"

	"github.com/coder/websocket"

	"github.com/W-Garlington/io7/store"
)

// event is the single message shape pushed over the WebSocket. All event
// kinds share one connection (see IOX_PLAN.md "Live updates"); clients
// switch on Type. New kinds (graph updates, fs events) add Type values,
// not endpoints.
type event struct {
	Type string          `json:"type"`
	ID   string          `json:"id,omitempty"`
	Doc  *store.Document `json:"doc,omitempty"`
}

// hub fans events out to every connected WebSocket client, keeping
// multiple browser tabs in sync.
type hub struct {
	mu      sync.Mutex
	clients map[chan []byte]struct{}
}

func newHub() *hub {
	return &hub{clients: make(map[chan []byte]struct{})}
}

func (h *hub) add() chan []byte {
	ch := make(chan []byte, 16)
	h.mu.Lock()
	h.clients[ch] = struct{}{}
	h.mu.Unlock()
	return ch
}

func (h *hub) remove(ch chan []byte) {
	h.mu.Lock()
	delete(h.clients, ch)
	h.mu.Unlock()
}

func (h *hub) broadcast(ev event) {
	msg, err := json.Marshal(ev)
	if err != nil {
		log.Printf("marshaling event: %v", err)
		return
	}
	h.mu.Lock()
	defer h.mu.Unlock()
	for ch := range h.clients {
		select {
		case ch <- msg:
		default: // client too slow; drop rather than block the writer
		}
	}
}

// closeAll disconnects every client channel; used during shutdown.
func (h *hub) closeAll() {
	h.mu.Lock()
	defer h.mu.Unlock()
	for ch := range h.clients {
		close(ch)
		delete(h.clients, ch)
	}
}

func (s *Server) handleWS(w http.ResponseWriter, r *http.Request) {
	conn, err := websocket.Accept(w, r, nil)
	if err != nil {
		log.Printf("websocket accept: %v", err)
		return
	}
	defer conn.CloseNow()

	ch := s.hub.add()
	defer s.hub.remove(ch)

	// Reads are discarded but must be pumped for control frames (ping,
	// close) to be processed; a read error means the client went away.
	readErr := make(chan struct{})
	go func() {
		defer close(readErr)
		for {
			if _, _, err := conn.Read(r.Context()); err != nil {
				return
			}
		}
	}()

	for {
		select {
		case msg, ok := <-ch:
			if !ok {
				conn.Close(websocket.StatusGoingAway, "server shutting down")
				return
			}
			writeCtx, cancel := context.WithTimeout(r.Context(), 5*time.Second)
			err := conn.Write(writeCtx, websocket.MessageText, msg)
			cancel()
			if err != nil {
				return
			}
		case <-readErr:
			return
		}
	}
}
