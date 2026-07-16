package graphdb

import (
	"path/filepath"
	"testing"
)

func openTestDB(t *testing.T) *DB {
	t.Helper()
	db, err := Open(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	t.Cleanup(db.Close)
	return db
}

func TestQueryRoundTrip(t *testing.T) {
	db := openTestDB(t)

	mustExec := func(cypher string, params map[string]any) {
		t.Helper()
		if err := db.Exec(cypher, params); err != nil {
			t.Fatalf("Exec(%q): %v", cypher, err)
		}
	}

	mustExec("CREATE NODE TABLE Doc(id STRING PRIMARY KEY, title STRING, words INT64, pinned BOOLEAN)", nil)
	mustExec("CREATE (:Doc {id: $id, title: $title, words: $words, pinned: $pinned})",
		map[string]any{"id": "d1", "title": "hello", "words": int64(42), "pinned": true})

	rows, err := db.Query("MATCH (d:Doc {id: $id}) RETURN d.title AS title, d.words AS words, d.pinned AS pinned",
		map[string]any{"id": "d1"})
	if err != nil {
		t.Fatalf("Query: %v", err)
	}
	if len(rows) != 1 {
		t.Fatalf("got %d rows, want 1", len(rows))
	}
	row := rows[0]
	if row["title"] != "hello" || row["words"] != int64(42) || row["pinned"] != true {
		t.Errorf("unexpected row: %#v", row)
	}
}

func TestQueryLinkedNodes(t *testing.T) {
	db := openTestDB(t)

	statements := []string{
		"CREATE NODE TABLE Doc(id STRING PRIMARY KEY, title STRING)",
		"CREATE REL TABLE LINKS_TO(FROM Doc TO Doc)",
		"CREATE (:Doc {id: 'a', title: 'A'})",
		"CREATE (:Doc {id: 'b', title: 'B'})",
		"MATCH (a:Doc {id: 'a'}), (b:Doc {id: 'b'}) CREATE (a)-[:LINKS_TO]->(b)",
	}
	for _, stmt := range statements {
		if err := db.Exec(stmt, nil); err != nil {
			t.Fatalf("Exec(%q): %v", stmt, err)
		}
	}

	rows, err := db.Query("MATCH (a:Doc)-[:LINKS_TO]->(b:Doc) RETURN a.id AS src, b.id AS dst", nil)
	if err != nil {
		t.Fatalf("Query: %v", err)
	}
	if len(rows) != 1 || rows[0]["src"] != "a" || rows[0]["dst"] != "b" {
		t.Errorf("unexpected rows: %#v", rows)
	}
}

func TestQueryErrorReporting(t *testing.T) {
	db := openTestDB(t)
	if _, err := db.Query("MATCH (x:NoSuchTable) RETURN x", nil); err == nil {
		t.Fatal("expected error for query against missing table")
	}
}
