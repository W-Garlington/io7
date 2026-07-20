// Package server is io7's HTTP layer: REST CRUD over the store, a single
// multiplexed WebSocket for live events, and the embedded web frontend.
package server

import (
	"context"
	"errors"
	"log"
	"net"
	"net/http"
	"time"

	"github.com/W-Garlington/io7/store"
	"github.com/W-Garlington/io7/web"
)

// Server owns the HTTP mux and the event hub. Create one with New, then
// call Run.
type Server struct {
	store    *store.Store
	hub      *hub
	mux      *http.ServeMux
	shutdown context.CancelFunc // requests process exit (Ctrl-C equivalent)
}

// New wires routes for the given store. shutdown is called when the
// /shutdown endpoint is hit.
func New(st *store.Store, shutdown context.CancelFunc) *Server {
	s := &Server{
		store:    st,
		hub:      newHub(),
		mux:      http.NewServeMux(),
		shutdown: shutdown,
	}

	s.mux.HandleFunc("GET /api/docs", s.handleListDocs)
	s.mux.HandleFunc("POST /api/docs", s.handleCreateDoc)
	s.mux.HandleFunc("GET /api/docs/{id}", s.handleGetDoc)
	s.mux.HandleFunc("PUT /api/docs/{id}", s.handleUpdateDoc)
	s.mux.HandleFunc("DELETE /api/docs/{id}", s.handleDeleteDoc)
	s.mux.HandleFunc("GET /api/docs/{id}/references", s.handleDocReferences)
	s.mux.HandleFunc("GET /api/reftypes", s.handleRefTypes)
	s.mux.HandleFunc("GET /ws", s.handleWS)
	s.mux.HandleFunc("POST /shutdown", s.handleShutdown)
	s.mux.Handle("GET /", http.FileServerFS(web.Assets))

	return s
}

// Handler exposes the mux for tests and embedding.
func (s *Server) Handler() http.Handler {
	return s.mux
}

// Run serves on addr (loopback only — see main.go) until ctx is cancelled,
// then shuts down gracefully. Returns the address actually listened on via
// the ready callback before serving, so the caller can open a browser.
func (s *Server) Run(ctx context.Context, addr string, ready func(addr string)) error {
	listener, err := net.Listen("tcp", addr)
	if err != nil {
		return err
	}

	srv := &http.Server{Handler: s.mux}
	errc := make(chan error, 1)
	go func() { errc <- srv.Serve(listener) }()

	if ready != nil {
		ready(listener.Addr().String())
	}

	select {
	case err := <-errc:
		return err
	case <-ctx.Done():
	}

	shutdownCtx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	s.hub.closeAll()
	if err := srv.Shutdown(shutdownCtx); err != nil {
		return err
	}
	if err := <-errc; !errors.Is(err, http.ErrServerClosed) {
		return err
	}
	return nil
}

func (s *Server) handleShutdown(w http.ResponseWriter, r *http.Request) {
	log.Println("shutdown requested via /shutdown")
	w.WriteHeader(http.StatusNoContent)
	s.shutdown()
}
