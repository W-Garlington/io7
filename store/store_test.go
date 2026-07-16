package store

import (
	"errors"
	"path/filepath"
	"testing"
)

func openTestStore(t *testing.T) *Store {
	t.Helper()
	s, err := Open(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	t.Cleanup(s.Close)
	return s
}

func TestDocumentCRUD(t *testing.T) {
	s := openTestStore(t)

	created, err := s.CreateDocument("First", "hello world")
	if err != nil {
		t.Fatalf("CreateDocument: %v", err)
	}
	if created.ID == "" || created.Title != "First" || created.Content != "hello world" {
		t.Fatalf("unexpected created doc: %#v", created)
	}
	if created.CreatedAt.IsZero() || !created.UpdatedAt.Equal(created.CreatedAt) {
		t.Errorf("unexpected timestamps: %#v", created)
	}

	got, err := s.GetDocument(created.ID)
	if err != nil {
		t.Fatalf("GetDocument: %v", err)
	}
	if got != created {
		t.Errorf("GetDocument = %#v, want %#v", got, created)
	}

	updated, err := s.UpdateDocument(created.ID, "Renamed", "new content")
	if err != nil {
		t.Fatalf("UpdateDocument: %v", err)
	}
	if updated.Title != "Renamed" || updated.Content != "new content" {
		t.Errorf("unexpected updated doc: %#v", updated)
	}
	if updated.UpdatedAt.Before(created.UpdatedAt) {
		t.Errorf("UpdatedAt went backwards: %#v", updated)
	}

	docs, err := s.ListDocuments()
	if err != nil {
		t.Fatalf("ListDocuments: %v", err)
	}
	if len(docs) != 1 || docs[0].ID != created.ID {
		t.Errorf("unexpected list: %#v", docs)
	}
	if docs[0].Content != "" {
		t.Errorf("list should not include content, got %q", docs[0].Content)
	}

	if err := s.DeleteDocument(created.ID); err != nil {
		t.Fatalf("DeleteDocument: %v", err)
	}
	if _, err := s.GetDocument(created.ID); !errors.Is(err, ErrNotFound) {
		t.Errorf("GetDocument after delete = %v, want ErrNotFound", err)
	}
}

func TestNotFound(t *testing.T) {
	s := openTestStore(t)

	if _, err := s.GetDocument("nope"); !errors.Is(err, ErrNotFound) {
		t.Errorf("GetDocument = %v, want ErrNotFound", err)
	}
	if _, err := s.UpdateDocument("nope", "t", "c"); !errors.Is(err, ErrNotFound) {
		t.Errorf("UpdateDocument = %v, want ErrNotFound", err)
	}
	if err := s.DeleteDocument("nope"); !errors.Is(err, ErrNotFound) {
		t.Errorf("DeleteDocument = %v, want ErrNotFound", err)
	}
}

func TestSchemaIsIdempotent(t *testing.T) {
	path := filepath.Join(t.TempDir(), "test.db")
	s, err := Open(path)
	if err != nil {
		t.Fatalf("first Open: %v", err)
	}
	if _, err := s.CreateDocument("persists", "body"); err != nil {
		t.Fatalf("CreateDocument: %v", err)
	}
	s.Close()

	s, err = Open(path)
	if err != nil {
		t.Fatalf("second Open: %v", err)
	}
	defer s.Close()
	docs, err := s.ListDocuments()
	if err != nil {
		t.Fatalf("ListDocuments: %v", err)
	}
	if len(docs) != 1 || docs[0].Title != "persists" {
		t.Errorf("unexpected docs after reopen: %#v", docs)
	}
}
