package store

// The [[...]] wiki-link grammar parsed out of document content. The
// grammar is frozen in docs/references.md, and the fixture file
// docs/link-grammar-fixtures.json is the contract shared with the
// frontend's link decorator — changes here must update both.

import (
	"regexp"
	"strings"
)

// defaultRefType is the relationship type of an untyped [[link]].
const defaultRefType = "links"

// link is one wiki-link occurrence. Offsets are transient parse state
// (used for rename rewriting within one call); they are never persisted —
// see the invariant in docs/references.md.
type link struct {
	start, end int    // byte offsets of the whole [[...]] span
	typ        string // relationship type, defaultRefType when untyped
	target     string // target document title
	block      string // block id after #^, "" when targeting the whole doc
	display    string // display alias after |, "" when absent
}

var linkRE = regexp.MustCompile(`\[\[([^\[\]|\n]+)(?:\|([^\[\]|\n]*))?\]\]`)

// parseLinks extracts every wiki-link from s. Targets, types, and
// displays are whitespace-trimmed; types are lowercased so "Supports"
// and "supports" are one relationship type.
func parseLinks(s string) []link {
	var out []link
	for _, m := range linkRE.FindAllStringSubmatchIndex(s, -1) {
		inner := s[m[2]:m[3]]
		l := link{start: m[0], end: m[1], typ: defaultRefType}
		if m[4] >= 0 {
			l.display = strings.TrimSpace(s[m[4]:m[5]])
		}
		if i := strings.Index(inner, "::"); i >= 0 {
			if t := strings.ToLower(strings.TrimSpace(inner[:i])); t != "" {
				l.typ = t
			}
			inner = inner[i+2:]
		}
		if i := strings.Index(inner, "#^"); i >= 0 {
			l.block = strings.TrimSpace(inner[i+2:])
			inner = inner[:i]
		}
		l.target = strings.TrimSpace(inner)
		if l.target == "" {
			continue
		}
		out = append(out, l)
	}
	return out
}

// canonical renders l back to grammar syntax. Used by rename rewriting;
// normalizes whitespace and omits an explicit default type.
func (l link) canonical() string {
	var b strings.Builder
	b.WriteString("[[")
	if l.typ != defaultRefType {
		b.WriteString(l.typ)
		b.WriteString("::")
	}
	b.WriteString(l.target)
	if l.block != "" {
		b.WriteString("#^")
		b.WriteString(l.block)
	}
	if l.display != "" {
		b.WriteString("|")
		b.WriteString(l.display)
	}
	b.WriteString("]]")
	return b.String()
}

// rewriteTargets re-points every link targeting oldTitle (title matching
// is case-insensitive, like resolution) at newTitle, reporting whether
// anything changed.
func rewriteTargets(s, oldTitle, newTitle string) (string, bool) {
	links := parseLinks(s)
	var b strings.Builder
	last := 0
	for _, l := range links {
		if !strings.EqualFold(l.target, oldTitle) {
			continue
		}
		b.WriteString(s[last:l.start])
		l.target = newTitle
		b.WriteString(l.canonical())
		last = l.end
	}
	if last == 0 {
		return s, false
	}
	b.WriteString(s[last:])
	return b.String(), true
}

// linksTo reports whether content contains a link targeting title.
func linksTo(content, title string) bool {
	for _, l := range parseLinks(content) {
		if strings.EqualFold(l.target, title) {
			return true
		}
	}
	return false
}
