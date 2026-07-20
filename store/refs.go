package store

// Reference indexing (docs/references.md): documents decompose into Block
// nodes, and REFERENCES edges are derived from the link markup in their
// content. Content is the source of truth — everything here is a
// rebuildable index, and no character offsets are ever persisted.

import (
	"crypto/rand"
	"encoding/hex"
	"regexp"
	"sort"
	"strings"
)

// Block is one paragraph of a document, indexed as a graph node.
type Block struct {
	ID   string
	Text string
	Ord  int64
}

// Reference is one edge of a document's reference set, as served by the
// API. For outgoing references DocID/DocTitle name the target document;
// for incoming ones, the source. BlockID/BlockText are always the source
// block — the anchor whose text contains the link.
type Reference struct {
	DocID         string `json:"docId"`
	DocTitle      string `json:"docTitle"`
	BlockID       string `json:"blockId"`
	BlockText     string `json:"blockText"`
	Type          string `json:"type"`
	TargetBlockID string `json:"targetBlockId,omitempty"`
}

// DocReferences is both directions of a document's reference set.
type DocReferences struct {
	Outgoing []Reference `json:"outgoing"`
	Incoming []Reference `json:"incoming"`
}

// seedRefTypes are always offered by RefTypes, so the type picker is
// never empty; users create further types just by using them.
var seedRefTypes = []string{"contradicts", "defines", "links", "related", "source", "supports"}

// markerRE matches a trailing block-id pin (" ^a3f91c2b"). The marker
// must follow text — a line that is only a marker is ordinary text.
var markerRE = regexp.MustCompile(`[ \t]\^([0-9a-f]{4,32})$`)

// blockLine is one paragraph of content before reconciliation.
type blockLine struct {
	text string // full line text, marker included
	pin  string // block id pinned by a trailing ^id marker, "" if none
}

// newBlockID returns a short random hex id — short because pinned ids
// appear as ^id markers in document text.
func newBlockID() string {
	var b [4]byte
	rand.Read(b[:])
	return hex.EncodeToString(b[:])
}

// splitBlocks derives the paragraph list from content: every line that is
// not blank is a block (paragraph = line, the prose editor's model).
func splitBlocks(content string) []blockLine {
	var lines []blockLine
	for _, line := range strings.Split(content, "\n") {
		if strings.TrimSpace(line) == "" {
			continue
		}
		bl := blockLine{text: line}
		if m := markerRE.FindStringSubmatch(line); m != nil {
			bl.pin = m[1]
		}
		lines = append(lines, bl)
	}
	return lines
}

// reconcile assigns block IDs to the new paragraph list, preserving old
// identities: an explicit ^id marker is authoritative, then exact text
// match, then in-order pairing of the leftovers (so a lightly edited
// paragraph keeps its id). See docs/references.md "Block identity".
func (s *Store) reconcile(old []Block, lines []blockLine) ([]Block, error) {
	desired := make([]Block, len(lines))
	oldExists := make(map[string]bool, len(old))
	for _, b := range old {
		oldExists[b.ID] = true
	}
	used := make(map[string]bool)

	// Pinned markers first. A duplicate pin (paragraph copied with its
	// marker) is honored once; later occurrences fall through and get a
	// fresh id. A pin owned by a different document (marker pasted across
	// docs) must not steal that document's block node.
	for i, ln := range lines {
		if ln.pin == "" || used[ln.pin] {
			continue
		}
		if !oldExists[ln.pin] {
			taken, err := s.blockExists(ln.pin)
			if err != nil {
				return nil, err
			}
			if taken {
				continue
			}
		}
		desired[i].ID = ln.pin
		used[ln.pin] = true
	}

	// Exact text match against unclaimed old blocks.
	byText := make(map[string][]string)
	for _, b := range old {
		if !used[b.ID] {
			byText[b.Text] = append(byText[b.Text], b.ID)
		}
	}
	for i, ln := range lines {
		if desired[i].ID != "" {
			continue
		}
		if q := byText[ln.text]; len(q) > 0 {
			desired[i].ID = q[0]
			used[q[0]] = true
			byText[ln.text] = q[1:]
		}
	}

	// In-order pairing of leftover new lines with leftover old blocks.
	var free []string
	for _, b := range old {
		if !used[b.ID] {
			free = append(free, b.ID)
		}
	}
	next := 0
	for i := range lines {
		if desired[i].ID == "" && next < len(free) {
			desired[i].ID = free[next]
			used[free[next]] = true
			next++
		}
	}

	for i := range lines {
		if desired[i].ID == "" {
			desired[i].ID = newBlockID()
		}
		desired[i].Text = lines[i].text
		desired[i].Ord = int64(i)
	}
	return desired, nil
}

func (s *Store) blockExists(id string) (bool, error) {
	rows, err := s.db.Query(`MATCH (b:Block {id: $id}) RETURN 1 AS one`,
		map[string]any{"id": id})
	return len(rows) > 0, err
}

// blocks returns a document's current blocks in order.
func (s *Store) blocks(docID string) ([]Block, error) {
	rows, err := s.db.Query(
		`MATCH (:Document {id: $id})-[:HAS_BLOCK]->(b:Block)
		 RETURN b.id AS id, b.text AS text, b.ord AS ord ORDER BY b.ord`,
		map[string]any{"id": docID})
	if err != nil {
		return nil, err
	}
	blocks := make([]Block, 0, len(rows))
	for _, row := range rows {
		b := Block{}
		b.ID, _ = row["id"].(string)
		b.Text, _ = row["text"].(string)
		b.Ord, _ = row["ord"].(int64)
		blocks = append(blocks, b)
	}
	return blocks, nil
}

// reindexDoc rebuilds a document's blocks and outgoing REFERENCES edges
// from content. Idempotent: the same content yields the same graph. The
// ids of documents whose reference sets may have changed — the document
// itself plus old and new targets — are added to changed.
func (s *Store) reindexDoc(id, content string, changed map[string]bool) error {
	changed[id] = true
	if err := s.outgoingTargets(id, changed); err != nil {
		return err
	}

	old, err := s.blocks(id)
	if err != nil {
		return err
	}
	desired, err := s.reconcile(old, splitBlocks(content))
	if err != nil {
		return err
	}

	// Apply the block diff: delete, create, update only what changed.
	oldByID := make(map[string]Block, len(old))
	for _, b := range old {
		oldByID[b.ID] = b
	}
	keep := make(map[string]bool, len(desired))
	for _, b := range desired {
		keep[b.ID] = true
	}
	for _, b := range old {
		if !keep[b.ID] {
			err := s.db.Exec(`MATCH (b:Block {id: $id}) DETACH DELETE b`,
				map[string]any{"id": b.ID})
			if err != nil {
				return err
			}
		}
	}
	for _, b := range desired {
		ob, existed := oldByID[b.ID]
		switch {
		case !existed:
			err = s.db.Exec(
				`MATCH (d:Document {id: $doc})
				 CREATE (d)-[:HAS_BLOCK]->(:Block {id: $id, text: $text, ord: $ord})`,
				map[string]any{"doc": id, "id": b.ID, "text": b.Text, "ord": b.Ord})
		case ob.Text != b.Text || ob.Ord != b.Ord:
			err = s.db.Exec(`MATCH (b:Block {id: $id}) SET b.text = $text, b.ord = $ord`,
				map[string]any{"id": b.ID, "text": b.Text, "ord": b.Ord})
		}
		if err != nil {
			return err
		}
	}

	// Rebuild outgoing edges from the links in the new blocks.
	err = s.db.Exec(
		`MATCH (:Document {id: $id})-[:HAS_BLOCK]->(:Block)-[r:REFERENCES]->() DELETE r`,
		map[string]any{"id": id})
	if err != nil {
		return err
	}
	titles, err := s.titleIndex()
	if err != nil {
		return err
	}
	made := make(map[string]bool) // dedupe identical links in one block
	for _, b := range desired {
		for _, l := range parseLinks(b.Text) {
			targetDoc := titles[strings.ToLower(l.target)]
			if targetDoc == "" { // unresolved or ambiguous: no edge
				continue
			}
			targetBlock := ""
			if l.block != "" {
				ok, err := s.docHasBlock(targetDoc, l.block)
				if err != nil {
					return err
				}
				if ok {
					targetBlock = l.block
				}
			}
			key := b.ID + "\x00" + targetDoc + "\x00" + targetBlock + "\x00" + l.typ
			if made[key] {
				continue
			}
			made[key] = true
			params := map[string]any{
				"src": b.ID, "dst": targetDoc, "type": l.typ, "display": l.display,
			}
			if targetBlock != "" {
				params["dst"] = targetBlock
				err = s.db.Exec(
					`MATCH (b:Block {id: $src}), (t:Block {id: $dst})
					 CREATE (b)-[:REFERENCES {type: $type, display: $display}]->(t)`, params)
			} else {
				err = s.db.Exec(
					`MATCH (b:Block {id: $src}), (t:Document {id: $dst})
					 CREATE (b)-[:REFERENCES {type: $type, display: $display}]->(t)`, params)
			}
			if err != nil {
				return err
			}
			changed[targetDoc] = true
		}
	}
	return nil
}

// outgoingTargets adds the ids of documents currently referenced from
// docID into set.
func (s *Store) outgoingTargets(docID string, set map[string]bool) error {
	for _, q := range []string{
		`MATCH (:Document {id: $id})-[:HAS_BLOCK]->(:Block)-[:REFERENCES]->(t:Document)
		 RETURN DISTINCT t.id AS id`,
		`MATCH (:Document {id: $id})-[:HAS_BLOCK]->(:Block)-[:REFERENCES]->(:Block)<-[:HAS_BLOCK]-(t:Document)
		 RETURN DISTINCT t.id AS id`,
	} {
		rows, err := s.db.Query(q, map[string]any{"id": docID})
		if err != nil {
			return err
		}
		for _, row := range rows {
			if id, _ := row["id"].(string); id != "" {
				set[id] = true
			}
		}
	}
	return nil
}

// incomingSources adds the ids of documents referencing docID into set.
func (s *Store) incomingSources(docID string, set map[string]bool) error {
	for _, q := range []string{
		`MATCH (sd:Document)-[:HAS_BLOCK]->(:Block)-[:REFERENCES]->(:Document {id: $id})
		 RETURN DISTINCT sd.id AS id`,
		`MATCH (sd:Document)-[:HAS_BLOCK]->(:Block)-[:REFERENCES]->(:Block)<-[:HAS_BLOCK]-(:Document {id: $id})
		 RETURN DISTINCT sd.id AS id`,
	} {
		rows, err := s.db.Query(q, map[string]any{"id": docID})
		if err != nil {
			return err
		}
		for _, row := range rows {
			if id, _ := row["id"].(string); id != "" {
				set[id] = true
			}
		}
	}
	return nil
}

// titleIndex maps lowercased titles to document ids; ambiguous titles
// (shared by several documents) map to "" so no edge is indexed for them.
func (s *Store) titleIndex() (map[string]string, error) {
	rows, err := s.db.Query(`MATCH (d:Document) RETURN d.id AS id, d.title AS title`, nil)
	if err != nil {
		return nil, err
	}
	titles := make(map[string]string, len(rows))
	for _, row := range rows {
		id, _ := row["id"].(string)
		title, _ := row["title"].(string)
		key := strings.ToLower(title)
		if _, dup := titles[key]; dup {
			titles[key] = ""
		} else {
			titles[key] = id
		}
	}
	return titles, nil
}

func (s *Store) docHasBlock(docID, blockID string) (bool, error) {
	rows, err := s.db.Query(
		`MATCH (:Document {id: $doc})-[:HAS_BLOCK]->(:Block {id: $block}) RETURN 1 AS one`,
		map[string]any{"doc": docID, "block": blockID})
	return len(rows) > 0, err
}

// reindexLinking reindexes every document whose content links the given
// title — late resolution when a document appears (or a title becomes
// unambiguous), and edge removal when it becomes ambiguous.
func (s *Store) reindexLinking(title string, changed map[string]bool) error {
	rows, err := s.db.Query(`MATCH (d:Document) RETURN d.id AS id, d.content AS content`, nil)
	if err != nil {
		return err
	}
	for _, row := range rows {
		id, _ := row["id"].(string)
		content, _ := row["content"].(string)
		if !linksTo(content, title) {
			continue
		}
		if err := s.reindexDoc(id, content, changed); err != nil {
			return err
		}
	}
	return nil
}

// ReindexAll rebuilds blocks and reference edges for every document. Run
// on Open so the index heals itself after schema changes or repair.
func (s *Store) ReindexAll() error {
	rows, err := s.db.Query(`MATCH (d:Document) RETURN d.id AS id, d.content AS content`, nil)
	if err != nil {
		return err
	}
	changed := make(map[string]bool)
	for _, row := range rows {
		id, _ := row["id"].(string)
		content, _ := row["content"].(string)
		if err := s.reindexDoc(id, content, changed); err != nil {
			return err
		}
	}
	return nil
}

// References returns both directions of a document's reference set, or
// ErrNotFound.
func (s *Store) References(id string) (DocReferences, error) {
	if _, err := s.GetDocument(id); err != nil {
		return DocReferences{}, err
	}
	refs := DocReferences{Outgoing: []Reference{}, Incoming: []Reference{}}

	outgoing := []string{
		`MATCH (:Document {id: $id})-[:HAS_BLOCK]->(b:Block)-[r:REFERENCES]->(t:Document)
		 RETURN t.id AS docId, t.title AS docTitle, b.id AS blockId, b.text AS blockText,
		        r.type AS refType, '' AS targetBlockId
		 ORDER BY b.ord`,
		`MATCH (:Document {id: $id})-[:HAS_BLOCK]->(b:Block)-[r:REFERENCES]->(tb:Block)<-[:HAS_BLOCK]-(t:Document)
		 RETURN t.id AS docId, t.title AS docTitle, b.id AS blockId, b.text AS blockText,
		        r.type AS refType, tb.id AS targetBlockId
		 ORDER BY b.ord`,
	}
	incoming := []string{
		`MATCH (sd:Document)-[:HAS_BLOCK]->(b:Block)-[r:REFERENCES]->(:Document {id: $id})
		 RETURN sd.id AS docId, sd.title AS docTitle, b.id AS blockId, b.text AS blockText,
		        r.type AS refType, '' AS targetBlockId
		 ORDER BY sd.title, b.ord`,
		`MATCH (sd:Document)-[:HAS_BLOCK]->(b:Block)-[r:REFERENCES]->(tb:Block)<-[:HAS_BLOCK]-(:Document {id: $id})
		 RETURN sd.id AS docId, sd.title AS docTitle, b.id AS blockId, b.text AS blockText,
		        r.type AS refType, tb.id AS targetBlockId
		 ORDER BY sd.title, b.ord`,
	}
	for _, q := range outgoing {
		if err := s.appendRefs(q, id, &refs.Outgoing); err != nil {
			return DocReferences{}, err
		}
	}
	for _, q := range incoming {
		if err := s.appendRefs(q, id, &refs.Incoming); err != nil {
			return DocReferences{}, err
		}
	}
	return refs, nil
}

func (s *Store) appendRefs(query, id string, dst *[]Reference) error {
	rows, err := s.db.Query(query, map[string]any{"id": id})
	if err != nil {
		return err
	}
	for _, row := range rows {
		ref := Reference{}
		ref.DocID, _ = row["docId"].(string)
		ref.DocTitle, _ = row["docTitle"].(string)
		ref.BlockID, _ = row["blockId"].(string)
		ref.BlockText, _ = row["blockText"].(string)
		ref.Type, _ = row["refType"].(string)
		ref.TargetBlockID, _ = row["targetBlockId"].(string)
		*dst = append(*dst, ref)
	}
	return nil
}

// RefTypes returns every relationship type in use plus the seed set,
// sorted, for the type picker.
func (s *Store) RefTypes() ([]string, error) {
	rows, err := s.db.Query(`MATCH ()-[r:REFERENCES]->() RETURN DISTINCT r.type AS t`, nil)
	if err != nil {
		return nil, err
	}
	set := make(map[string]bool, len(rows)+len(seedRefTypes))
	for _, t := range seedRefTypes {
		set[t] = true
	}
	for _, row := range rows {
		if t, _ := row["t"].(string); t != "" {
			set[t] = true
		}
	}
	return sortedKeys(set), nil
}

func sortedKeys(set map[string]bool) []string {
	keys := make([]string, 0, len(set))
	for k := range set {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}
