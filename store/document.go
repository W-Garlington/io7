package store

import (
	"fmt"
	"time"

	"github.com/W-Garlington/io7/graphdb"
)

// Document is a text document stored as a graph node. Content lives as a
// graph property for now — documents are plain text and small; large or
// binary attachments should become on-disk files with a path reference
// (see IOX_PLAN.md "Content storage split").
type Document struct {
	ID        string    `json:"id"`
	Title     string    `json:"title"`
	Content   string    `json:"content"`
	CreatedAt time.Time `json:"createdAt"`
	UpdatedAt time.Time `json:"updatedAt"`
}

const docFields = "d.id AS id, d.title AS title, d.content AS content, d.createdAt AS createdAt, d.updatedAt AS updatedAt"

// ListDocuments returns all documents without their content, newest first.
func (s *Store) ListDocuments() ([]Document, error) {
	rows, err := s.db.Query(
		`MATCH (d:Document)
		 RETURN d.id AS id, d.title AS title, d.createdAt AS createdAt, d.updatedAt AS updatedAt
		 ORDER BY d.updatedAt DESC`, nil)
	if err != nil {
		return nil, err
	}
	docs := make([]Document, 0, len(rows))
	for _, row := range rows {
		docs = append(docs, docFromRow(row))
	}
	return docs, nil
}

// GetDocument returns the document with the given id, or ErrNotFound.
func (s *Store) GetDocument(id string) (Document, error) {
	rows, err := s.db.Query(
		"MATCH (d:Document {id: $id}) RETURN "+docFields,
		map[string]any{"id": id})
	if err != nil {
		return Document{}, err
	}
	if len(rows) == 0 {
		return Document{}, fmt.Errorf("document %q: %w", id, ErrNotFound)
	}
	return docFromRow(rows[0]), nil
}

// CreateDocument creates a new document and returns it.
func (s *Store) CreateDocument(title, content string) (Document, error) {
	doc := Document{
		ID:        newID(),
		Title:     title,
		Content:   content,
		CreatedAt: time.Now().UTC().Truncate(time.Microsecond),
	}
	doc.UpdatedAt = doc.CreatedAt
	err := s.db.Exec(
		`CREATE (:Document {id: $id, title: $title, content: $content,
		 createdAt: $createdAt, updatedAt: $updatedAt})`,
		map[string]any{
			"id": doc.ID, "title": doc.Title, "content": doc.Content,
			"createdAt": doc.CreatedAt, "updatedAt": doc.UpdatedAt,
		})
	if err != nil {
		return Document{}, err
	}
	return doc, nil
}

// UpdateDocument replaces title and content of an existing document and
// returns the updated document, or ErrNotFound.
func (s *Store) UpdateDocument(id, title, content string) (Document, error) {
	rows, err := s.db.Query(
		`MATCH (d:Document {id: $id})
		 SET d.title = $title, d.content = $content, d.updatedAt = $updatedAt
		 RETURN `+docFields,
		map[string]any{
			"id": id, "title": title, "content": content,
			"updatedAt": time.Now().UTC().Truncate(time.Microsecond),
		})
	if err != nil {
		return Document{}, err
	}
	if len(rows) == 0 {
		return Document{}, fmt.Errorf("document %q: %w", id, ErrNotFound)
	}
	return docFromRow(rows[0]), nil
}

// DeleteDocument removes a document and all its edges, or ErrNotFound.
func (s *Store) DeleteDocument(id string) error {
	rows, err := s.db.Query(
		`MATCH (d:Document {id: $id}) DETACH DELETE d RETURN 1 AS deleted`,
		map[string]any{"id": id})
	if err != nil {
		return err
	}
	if len(rows) == 0 {
		return fmt.Errorf("document %q: %w", id, ErrNotFound)
	}
	return nil
}

func docFromRow(row graphdb.Row) Document {
	doc := Document{}
	doc.ID, _ = row["id"].(string)
	doc.Title, _ = row["title"].(string)
	doc.Content, _ = row["content"].(string)
	doc.CreatedAt, _ = row["createdAt"].(time.Time)
	doc.UpdatedAt, _ = row["updatedAt"].(time.Time)
	return doc
}
