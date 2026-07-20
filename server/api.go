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
	doc, refsChanged, err := s.store.CreateDocument(req.Title, req.Content)
	if err != nil {
		writeError(w, err)
		return
	}
	s.hub.broadcast(event{Type: "doc.created", Doc: &doc})
	s.broadcastRefs(refsChanged)
	writeJSON(w, http.StatusCreated, doc)
}

func (s *Server) handleUpdateDoc(w http.ResponseWriter, r *http.Request) {
	var req docRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "invalid JSON body", http.StatusBadRequest)
		return
	}
	res, err := s.store.UpdateDocument(r.PathValue("id"), req.Title, req.Content)
	if err != nil {
		writeError(w, err)
		return
	}
	s.hub.broadcast(event{Type: "doc.updated", Doc: &res.Doc})
	for i := range res.Rewritten { // rename propagation touched other docs
		s.hub.broadcast(event{Type: "doc.updated", Doc: &res.Rewritten[i]})
	}
	s.broadcastRefs(res.RefsChanged)
	writeJSON(w, http.StatusOK, res.Doc)
}

func (s *Server) handleDeleteDoc(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	refsChanged, err := s.store.DeleteDocument(id)
	if err != nil {
		writeError(w, err)
		return
	}
	s.hub.broadcast(event{Type: "doc.deleted", ID: id})
	s.broadcastRefs(refsChanged)
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) handleDocReferences(w http.ResponseWriter, r *http.Request) {
	refs, err := s.store.References(r.PathValue("id"))
	if err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, refs)
}

func (s *Server) handleRefTypes(w http.ResponseWriter, r *http.Request) {
	types, err := s.store.RefTypes()
	if err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, types)
}

// broadcastRefs emits refs.updated for every document whose reference
// set changed, so backlinks views refresh in all tabs.
func (s *Server) broadcastRefs(ids []string) {
	for _, id := range ids {
		s.hub.broadcast(event{Type: "refs.updated", ID: id})
	}
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
