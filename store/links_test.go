package store

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

// TestLinkGrammarFixtures runs the parser against the shared fixture
// file — the grammar contract with the frontend (docs/references.md).
func TestLinkGrammarFixtures(t *testing.T) {
	data, err := os.ReadFile(filepath.Join("..", "docs", "link-grammar-fixtures.json"))
	if err != nil {
		t.Fatalf("reading fixtures: %v", err)
	}
	var fixtures []struct {
		Name  string `json:"name"`
		Input string `json:"input"`
		Links []struct {
			Raw     string `json:"raw"`
			Type    string `json:"type"`
			Target  string `json:"target"`
			Block   string `json:"block"`
			Display string `json:"display"`
		} `json:"links"`
	}
	if err := json.Unmarshal(data, &fixtures); err != nil {
		t.Fatalf("parsing fixtures: %v", err)
	}
	if len(fixtures) == 0 {
		t.Fatal("no fixtures")
	}

	for _, fx := range fixtures {
		t.Run(fx.Name, func(t *testing.T) {
			got := parseLinks(fx.Input)
			if len(got) != len(fx.Links) {
				t.Fatalf("got %d links, want %d: %#v", len(got), len(fx.Links), got)
			}
			for i, want := range fx.Links {
				l := got[i]
				raw := fx.Input[l.start:l.end]
				if raw != want.Raw || l.typ != want.Type || l.target != want.Target ||
					l.block != want.Block || l.display != want.Display {
					t.Errorf("link %d = raw %q type %q target %q block %q display %q,\nwant raw %q type %q target %q block %q display %q",
						i, raw, l.typ, l.target, l.block, l.display,
						want.Raw, want.Type, want.Target, want.Block, want.Display)
				}
			}
		})
	}
}

func TestRewriteTargets(t *testing.T) {
	cases := []struct {
		name, in, want string
		changed        bool
	}{
		{"plain", "see [[Old]] here", "see [[New]] here", true},
		{"case-insensitive", "see [[old]] here", "see [[New]] here", true},
		{"keeps type alias block", "[[supports::Old#^ab12|shown]]", "[[supports::New#^ab12|shown]]", true},
		{"other targets untouched", "[[Other]] and [[Old]]", "[[Other]] and [[New]]", true},
		{"no match", "no links to speak of", "no links to speak of", false},
		{"not a rewrite of plain text", "Old ideas [[Other]]", "Old ideas [[Other]]", false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got, changed := rewriteTargets(c.in, "Old", "New")
			if got != c.want || changed != c.changed {
				t.Errorf("rewriteTargets(%q) = %q, %v; want %q, %v", c.in, got, changed, c.want, c.changed)
			}
		})
	}
}
