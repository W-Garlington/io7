// Package store is io7's data layer: the property-graph schema from
// IOX_PLAN.md and CRUD operations on top of graphdb. HTTP handlers should
// talk to this package, never to graphdb directly.
package store

import (
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"

	"github.com/W-Garlington/io7/graphdb"
)

// ErrNotFound is returned when a requested node does not exist.
var ErrNotFound = errors.New("not found")

// schema is the full property-graph shape. Document/Block/REFERENCES carry
// the references system (docs/references.md); Folder/Tag and their edges
// are defined now so later features (folders, tagging) extend data, not
// schema.
var schema = []string{
	`CREATE NODE TABLE IF NOT EXISTS Document(
		id STRING PRIMARY KEY, title STRING, content STRING,
		createdAt TIMESTAMP, updatedAt TIMESTAMP)`,
	`CREATE NODE TABLE IF NOT EXISTS Block(id STRING PRIMARY KEY, text STRING, ord INT64)`,
	`CREATE NODE TABLE IF NOT EXISTS Folder(id STRING PRIMARY KEY, name STRING)`,
	`CREATE NODE TABLE IF NOT EXISTS Tag(id STRING PRIMARY KEY, name STRING)`,
	`CREATE REL TABLE IF NOT EXISTS HAS_BLOCK(FROM Document TO Block)`,
	`CREATE REL TABLE IF NOT EXISTS CONTAINS(FROM Folder TO Document, FROM Folder TO Folder)`,
	`CREATE REL TABLE IF NOT EXISTS REFERENCES(FROM Block TO Document, FROM Block TO Block,
		type STRING, display STRING)`,
	`CREATE REL TABLE IF NOT EXISTS TAGGED(FROM Document TO Tag)`,
	// LINKS_TO predates the references system and was never written to;
	// REFERENCES (block-anchored, typed) replaces it.
	`DROP TABLE IF EXISTS LINKS_TO`,
}

// Store wraps an open graph database with io7's schema applied.
type Store struct {
	db *graphdb.DB
}

// Open opens (or creates) the database at path and applies the schema.
func Open(path string) (*Store, error) {
	db, err := graphdb.Open(path)
	if err != nil {
		return nil, err
	}
	for _, stmt := range schema {
		if err := db.Exec(stmt, nil); err != nil {
			db.Close()
			return nil, fmt.Errorf("store: applying schema: %w", err)
		}
	}
	s := &Store{db: db}
	// The reference index (blocks + edges) is derived from content, so a
	// full rebuild on open keeps it self-healing (docs/references.md).
	if err := s.ReindexAll(); err != nil {
		db.Close()
		return nil, fmt.Errorf("store: rebuilding reference index: %w", err)
	}
	return s, nil
}

// Close closes the underlying database.
func (s *Store) Close() {
	s.db.Close()
}

// newID returns a random 128-bit hex identifier.
func newID() string {
	var b [16]byte
	rand.Read(b[:])
	return hex.EncodeToString(b[:])
}
