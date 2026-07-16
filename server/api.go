package server

import (
	"encoding/json"
	"errors"
	"log"
	"net/http"

	"github.com/W-Garlington/io7/store"
)

// docRequest is the JSON body for creating or updating a document.
type docRequest struct {
	Title   string `json:"title"`
	Content string `json:"content"`
}

func (s *Server) handleListDocs(w http.ResponseWriter, r *http.Request) {
	docs, err := s.store.ListDocuments()
	if err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, docs)
}

func (s *Server) handleGetDoc(w http.ResponseWriter, r *http.Request) {
	doc, err := s.store.GetDocument(r.PathValue("id"))
	if err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, doc)
}

func (s *Server) handleCreateDoc(w http.ResponseWriter, r *http.Request) {
	var req docRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "invalid JSON body", http.StatusBadRequest)
		return
	}
	if req.Title == "" {
		req.Title = "Untitled"
	}
	doc, err := s.store.CreateDocument(req.Title, req.Content)
	if err != nil {
		writeError(w, err)
		return
	}
	s.hub.broadcast(event{Type: "doc.created", Doc: &doc})
	writeJSON(w, http.StatusCreated, doc)
}

func (s *Server) handleUpdateDoc(w http.ResponseWriter, r *http.Request) {
	var req docRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "invalid JSON body", http.StatusBadRequest)
		return
	}
	doc, err := s.store.UpdateDocument(r.PathValue("id"), req.Title, req.Content)
	if err != nil {
		writeError(w, err)
		return
	}
	s.hub.broadcast(event{Type: "doc.updated", Doc: &doc})
	writeJSON(w, http.StatusOK, doc)
}

func (s *Server) handleDeleteDoc(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	if err := s.store.DeleteDocument(id); err != nil {
		writeError(w, err)
		return
	}
	s.hub.broadcast(event{Type: "doc.deleted", ID: id})
	w.WriteHeader(http.StatusNoContent)
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	if err := json.NewEncoder(w).Encode(v); err != nil {
		log.Printf("writing response: %v", err)
	}
}

func writeError(w http.ResponseWriter, err error) {
	if errors.Is(err, store.ErrNotFound) {
		http.Error(w, err.Error(), http.StatusNotFound)
		return
	}
	log.Printf("internal error: %v", err)
	http.Error(w, "internal error", http.StatusInternalServerError)
}
