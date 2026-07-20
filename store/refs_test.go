package store

import (
	"strings"
	"testing"
)

func mustCreate(t *testing.T, s *Store, title, content string) Document {
	t.Helper()
	doc, _, err := s.CreateDocument(title, content)
	if err != nil {
		t.Fatalf("CreateDocument(%q): %v", title, err)
	}
	return doc
}

func mustRefs(t *testing.T, s *Store, id string) DocReferences {
	t.Helper()
	refs, err := s.References(id)
	if err != nil {
		t.Fatalf("References(%q): %v", id, err)
	}
	return refs
}

func mustBlocks(t *testing.T, s *Store, id string) []Block {
	t.Helper()
	blocks, err := s.blocks(id)
	if err != nil {
		t.Fatalf("blocks(%q): %v", id, err)
	}
	return blocks
}

func TestLinkResolutionAndLateBinding(t *testing.T) {
	s := openTestStore(t)

	a := mustCreate(t, s, "Alpha", "points at [[Beta]] early")
	if refs := mustRefs(t, s, a.ID); len(refs.Outgoing) != 0 {
		t.Fatalf("unresolved link produced edges: %#v", refs.Outgoing)
	}

	// Creating Beta must resolve Alpha's existing link (late resolution).
	b, changed, err := s.CreateDocument("Beta", "the target")
	if err != nil {
		t.Fatalf("CreateDocument: %v", err)
	}
	found := false
	for _, id := range changed {
		if id == a.ID {
			found = true
		}
	}
	if !found {
		t.Errorf("refsChanged %v does not include Alpha", changed)
	}

	refs := mustRefs(t, s, a.ID)
	if len(refs.Outgoing) != 1 || refs.Outgoing[0].DocID != b.ID || refs.Outgoing[0].Type != "links" {
		t.Fatalf("unexpected outgoing refs: %#v", refs.Outgoing)
	}
	back := mustRefs(t, s, b.ID)
	if len(back.Incoming) != 1 || back.Incoming[0].DocID != a.ID ||
		back.Incoming[0].BlockText != "points at [[Beta]] early" {
		t.Fatalf("unexpected incoming refs: %#v", back.Incoming)
	}
}

func TestReindexIsIdempotent(t *testing.T) {
	s := openTestStore(t)

	mustCreate(t, s, "Target", "t")
	doc := mustCreate(t, s, "Source", "one [[Target]]\n\ntwo\nthree")

	before := mustBlocks(t, s, doc.ID)
	if len(before) != 3 {
		t.Fatalf("got %d blocks, want 3: %#v", len(before), before)
	}

	// Saving identical content twice must not change ids or edges.
	for i := 0; i < 2; i++ {
		if _, err := s.UpdateDocument(doc.ID, doc.Title, doc.Content); err != nil {
			t.Fatalf("UpdateDocument: %v", err)
		}
	}
	after := mustBlocks(t, s, doc.ID)
	for i := range before {
		if after[i] != before[i] {
			t.Errorf("block %d changed: %#v -> %#v", i, before[i], after[i])
		}
	}
	if refs := mustRefs(t, s, doc.ID); len(refs.Outgoing) != 1 {
		t.Errorf("got %d outgoing refs, want 1: %#v", len(refs.Outgoing), refs.Outgoing)
	}
}

func TestBlockIdentitySurvivesEdits(t *testing.T) {
	s := openTestStore(t)
	doc := mustCreate(t, s, "Doc", "first paragraph\nsecond paragraph\nthird paragraph")
	before := mustBlocks(t, s, doc.ID)

	// Edit the middle paragraph, leave the others untouched.
	if _, err := s.UpdateDocument(doc.ID, "Doc",
		"first paragraph\nsecond paragraph, edited\nthird paragraph"); err != nil {
		t.Fatalf("UpdateDocument: %v", err)
	}
	after := mustBlocks(t, s, doc.ID)
	if len(after) != 3 {
		t.Fatalf("got %d blocks, want 3", len(after))
	}
	for i := range before {
		if after[i].ID != before[i].ID {
			t.Errorf("block %d lost its id: %q -> %q", i, before[i].ID, after[i].ID)
		}
	}

	// Inserting a paragraph keeps identity for exact-match paragraphs.
	if _, err := s.UpdateDocument(doc.ID, "Doc",
		"first paragraph\ninserted\nsecond paragraph, edited\nthird paragraph"); err != nil {
		t.Fatalf("UpdateDocument: %v", err)
	}
	final := mustBlocks(t, s, doc.ID)
	if len(final) != 4 {
		t.Fatalf("got %d blocks, want 4", len(final))
	}
	if final[0].ID != before[0].ID || final[3].ID != before[2].ID {
		t.Errorf("exact-match paragraphs lost ids: %#v", final)
	}
}

func TestPinnedMarkersAndBlockTargets(t *testing.T) {
	s := openTestStore(t)

	target := mustCreate(t, s, "Target", "intro\nthe key claim ^ab12cd34\noutro")
	blocks := mustBlocks(t, s, target.ID)
	if blocks[1].ID != "ab12cd34" {
		t.Fatalf("pinned marker not honored: %#v", blocks)
	}

	src := mustCreate(t, s, "Source", "see [[Target#^ab12cd34]] for the claim")
	refs := mustRefs(t, s, src.ID)
	if len(refs.Outgoing) != 1 || refs.Outgoing[0].TargetBlockID != "ab12cd34" ||
		refs.Outgoing[0].DocID != target.ID {
		t.Fatalf("unexpected block-target ref: %#v", refs.Outgoing)
	}
	back := mustRefs(t, s, target.ID)
	if len(back.Incoming) != 1 || back.Incoming[0].DocID != src.ID ||
		back.Incoming[0].TargetBlockID != "ab12cd34" {
		t.Fatalf("unexpected incoming block ref: %#v", back.Incoming)
	}

	// Heavy edits around the pinned paragraph must not break the link.
	if _, err := s.UpdateDocument(target.ID, "Target",
		"totally new intro\nmore\nthe key claim, reworded ^ab12cd34\nnew outro"); err != nil {
		t.Fatalf("UpdateDocument: %v", err)
	}
	back = mustRefs(t, s, target.ID)
	if len(back.Incoming) != 1 || back.Incoming[0].TargetBlockID != "ab12cd34" {
		t.Fatalf("pinned block lost incoming ref after edits: %#v", back.Incoming)
	}
}

func TestTypedLinksAndRefTypes(t *testing.T) {
	s := openTestStore(t)

	b := mustCreate(t, s, "Beta", "t")
	a := mustCreate(t, s, "Alpha", "[[supports::Beta|the evidence]]\n[[eclipses::Beta]]")

	refs := mustRefs(t, s, a.ID)
	if len(refs.Outgoing) != 2 {
		t.Fatalf("got %d outgoing, want 2: %#v", len(refs.Outgoing), refs.Outgoing)
	}
	types := map[string]bool{}
	for _, r := range refs.Outgoing {
		types[r.Type] = true
		if r.DocID != b.ID {
			t.Errorf("wrong target: %#v", r)
		}
	}
	if !types["supports"] || !types["eclipses"] {
		t.Errorf("unexpected types: %#v", refs.Outgoing)
	}

	// RefTypes = seeds plus types in use (the user-defined "eclipses").
	all, err := s.RefTypes()
	if err != nil {
		t.Fatalf("RefTypes: %v", err)
	}
	got := strings.Join(all, ",")
	for _, want := range append([]string{"eclipses"}, seedRefTypes...) {
		if !strings.Contains(","+got+",", ","+want+",") {
			t.Errorf("RefTypes %v missing %q", all, want)
		}
	}
}

func TestRenamePropagation(t *testing.T) {
	s := openTestStore(t)

	b := mustCreate(t, s, "Beta", "t")
	a := mustCreate(t, s, "Alpha", "see [[supports::Beta|shown]] and [[Beta]]")

	res, err := s.UpdateDocument(b.ID, "Gamma", "t")
	if err != nil {
		t.Fatalf("UpdateDocument: %v", err)
	}
	if len(res.Rewritten) != 1 || res.Rewritten[0].ID != a.ID {
		t.Fatalf("unexpected rewritten docs: %#v", res.Rewritten)
	}
	want := "see [[supports::Gamma|shown]] and [[Gamma]]"
	if res.Rewritten[0].Content != want {
		t.Errorf("rewritten content = %q, want %q", res.Rewritten[0].Content, want)
	}
	// Edges survive the rename.
	refs := mustRefs(t, s, a.ID)
	if len(refs.Outgoing) != 2 {
		t.Errorf("refs lost on rename: %#v", refs.Outgoing)
	}
}

func TestAmbiguousTitles(t *testing.T) {
	s := openTestStore(t)

	first := mustCreate(t, s, "Note", "t")
	a := mustCreate(t, s, "Alpha", "see [[Note]]")
	if refs := mustRefs(t, s, a.ID); len(refs.Outgoing) != 1 {
		t.Fatalf("expected 1 ref while unambiguous: %#v", refs.Outgoing)
	}

	// A second "Note" makes the title ambiguous: edges must drop.
	dup := mustCreate(t, s, "Note", "t2")
	if refs := mustRefs(t, s, a.ID); len(refs.Outgoing) != 0 {
		t.Fatalf("ambiguous title still has edges: %#v", refs.Outgoing)
	}

	// Deleting the duplicate resolves it again.
	if _, err := s.DeleteDocument(dup.ID); err != nil {
		t.Fatalf("DeleteDocument: %v", err)
	}
	refs := mustRefs(t, s, a.ID)
	if len(refs.Outgoing) != 1 || refs.Outgoing[0].DocID != first.ID {
		t.Fatalf("edge did not return after ambiguity cleared: %#v", refs.Outgoing)
	}
}

func TestDeleteClearsReferences(t *testing.T) {
	s := openTestStore(t)

	b := mustCreate(t, s, "Beta", "t")
	a := mustCreate(t, s, "Alpha", "see [[Beta]]")

	changed, err := s.DeleteDocument(b.ID)
	if err != nil {
		t.Fatalf("DeleteDocument: %v", err)
	}
	found := false
	for _, id := range changed {
		if id == a.ID {
			found = true
		}
	}
	if !found {
		t.Errorf("refsChanged %v does not include the referring doc", changed)
	}
	if refs := mustRefs(t, s, a.ID); len(refs.Outgoing) != 0 {
		t.Errorf("refs to deleted doc remain: %#v", refs.Outgoing)
	}
}
