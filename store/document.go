package store

import (
	"fmt"
	"strings"
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

// CreateDocument creates a new document, indexes its references, and
// resolves links elsewhere that were waiting for this title. The second
// return value lists documents whose reference sets changed.
func (s *Store) CreateDocument(title, content string) (Document, []string, error) {
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
		return Document{}, nil, err
	}
	changed := make(map[string]bool)
	if err := s.reindexDoc(doc.ID, content, changed); err != nil {
		return Document{}, nil, err
	}
	// Late resolution: [[title]] links elsewhere now resolve (or, if the
	// title was already taken, just became ambiguous and must drop edges).
	if err := s.reindexLinking(title, changed); err != nil {
		return Document{}, nil, err
	}
	return doc, sortedKeys(changed), nil
}

// UpdateResult is what a document save touched beyond the document itself.
type UpdateResult struct {
	Doc         Document
	Rewritten   []Document // other docs rewritten by rename propagation
	RefsChanged []string   // ids of docs whose reference sets changed
}

// UpdateDocument replaces title and content of an existing document,
// reindexes its references, and — on a rename — rewrites links in every
// referring document so a rename never breaks a resolved link
// (docs/references.md). Returns ErrNotFound if the document is missing.
func (s *Store) UpdateDocument(id, title, content string) (UpdateResult, error) {
	old, err := s.GetDocument(id)
	if err != nil {
		return UpdateResult{}, err
	}
	// Case-only renames keep resolving (matching is case-insensitive), so
	// only a real title change triggers propagation. Links to a title
	// shared with another document were ambiguous, not ours to rewrite.
	renamed := !strings.EqualFold(old.Title, title)
	oldTitleWasOurs := false
	if renamed {
		rows, err := s.db.Query(
			`MATCH (d:Document) WHERE d.id <> $id AND lower(d.title) = lower($title)
			 RETURN 1 AS one LIMIT 1`,
			map[string]any{"id": id, "title": old.Title})
		if err != nil {
			return UpdateResult{}, err
		}
		oldTitleWasOurs = len(rows) == 0
		if oldTitleWasOurs {
			content, _ = rewriteTargets(content, old.Title, title)
		}
	}

	rows, err := s.db.Query(
		`MATCH (d:Document {id: $id})
		 SET d.title = $title, d.content = $content, d.updatedAt = $updatedAt
		 RETURN `+docFields,
		map[string]any{
			"id": id, "title": title, "content": content,
			"updatedAt": time.Now().UTC().Truncate(time.Microsecond),
		})
	if err != nil {
		return UpdateResult{}, err
	}
	if len(rows) == 0 {
		return UpdateResult{}, fmt.Errorf("document %q: %w", id, ErrNotFound)
	}
	res := UpdateResult{Doc: docFromRow(rows[0])}

	changed := make(map[string]bool)
	if err := s.reindexDoc(id, content, changed); err != nil {
		return UpdateResult{}, err
	}
	if renamed {
		if oldTitleWasOurs {
			res.Rewritten, err = s.propagateRename(id, old.Title, title, changed)
			if err != nil {
				return UpdateResult{}, err
			}
		} else {
			// The old title may have just become unambiguous for others.
			if err := s.reindexLinking(old.Title, changed); err != nil {
				return UpdateResult{}, err
			}
		}
		// Links already written as [[title]] elsewhere resolve now.
		if err := s.reindexLinking(title, changed); err != nil {
			return UpdateResult{}, err
		}
	}
	res.RefsChanged = sortedKeys(changed)
	return res, nil
}

// propagateRename rewrites [[oldTitle]] links in every other document and
// reindexes the ones it touched, returning them.
func (s *Store) propagateRename(id, oldTitle, newTitle string, changed map[string]bool) ([]Document, error) {
	rows, err := s.db.Query(
		`MATCH (d:Document) WHERE d.id <> $id RETURN `+docFields,
		map[string]any{"id": id})
	if err != nil {
		return nil, err
	}
	var rewritten []Document
	for _, row := range rows {
		doc := docFromRow(row)
		content, ok := rewriteTargets(doc.Content, oldTitle, newTitle)
		if !ok {
			continue
		}
		upd, err := s.db.Query(
			`MATCH (d:Document {id: $id})
			 SET d.content = $content, d.updatedAt = $updatedAt
			 RETURN `+docFields,
			map[string]any{
				"id": doc.ID, "content": content,
				"updatedAt": time.Now().UTC().Truncate(time.Microsecond),
			})
		if err != nil {
			return nil, err
		}
		if err := s.reindexDoc(doc.ID, content, changed); err != nil {
			return nil, err
		}
		rewritten = append(rewritten, docFromRow(upd[0]))
	}
	return rewritten, nil
}

// DeleteDocument removes a document, its blocks, and all their edges,
// returning the ids of documents whose reference sets changed (former
// sources and targets, plus any links its title freed up). ErrNotFound
// if the document is missing.
func (s *Store) DeleteDocument(id string) ([]string, error) {
	doc, err := s.GetDocument(id)
	if err != nil {
		return nil, err
	}
	changed := make(map[string]bool)
	if err := s.incomingSources(id, changed); err != nil {
		return nil, err
	}
	if err := s.outgoingTargets(id, changed); err != nil {
		return nil, err
	}
	err = s.db.Exec(
		`MATCH (:Document {id: $id})-[:HAS_BLOCK]->(b:Block) DETACH DELETE b`,
		map[string]any{"id": id})
	if err != nil {
		return nil, err
	}
	rows, err := s.db.Query(
		`MATCH (d:Document {id: $id}) DETACH DELETE d RETURN 1 AS deleted`,
		map[string]any{"id": id})
	if err != nil {
		return nil, err
	}
	if len(rows) == 0 {
		return nil, fmt.Errorf("document %q: %w", id, ErrNotFound)
	}
	// A duplicate title may have just become unambiguous.
	if err := s.reindexLinking(doc.Title, changed); err != nil {
		return nil, err
	}
	delete(changed, id)
	return sortedKeys(changed), nil
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
