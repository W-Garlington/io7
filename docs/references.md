# References system — requirements

Requirements for cross-document references: wiki-style links, linked
words/phrases, and typed relationships between documents, stored and
queried through the graph DB. This document directs implementation;
architecture rationale for the rest of the system is in `IOX_PLAN.md`,
frontend mechanics in `docs/widgets.md`.

Decisions here were made deliberately (anchoring model, relationship
typing, v1 scope) — don't reopen them mid-implementation without noting
why in this file.

## The core problem, and the chosen anchoring model

A reference needs to know *where* in a document it sits. Storing
character offsets as metadata alongside a mutable text is the classic
failure mode: every edit shifts every offset after it, and repair
heuristics never fully win. This system **bypasses positions instead of
tracking them**, with two complementary mechanisms:

1. **Links live in the text as markup.** A reference is written into the
   content itself (`[[Target]]`). Its position is wherever the markup
   sits; it moves with edits, survives cut/paste, and needs no stored
   offset. The graph edges are a *derived index* rebuilt from the text
   on save — never the other way around.
2. **Documents decompose into block nodes.** On save, the backend splits
   content into paragraph-level `Block` nodes in the graph. Reference
   edges are anchored at the source *block*, giving backlinks a context
   snippet and sub-document addressability, again without offsets.

`Document.content` remains the single source of truth for text; blocks
are a reconciled index over it. (The alternative — blocks as the source
of truth, content assembled from them — was rejected: it complicates the
editor round-trip and CodeMirror integration for no gain while
paragraph = line remains the prose model.)

**Invariant: no character offsets are ever persisted — not in the graph,
not in sidecar metadata.** If a feature seems to need one, the design is
wrong; anchor it in markup or on a block instead.

## Link grammar

The concrete syntax, frozen here because both Go (indexer) and JS
(editor decoration) must implement it identically:

```
[[target]]                       plain link
[[target|display text]]          link with display alias ("linked words")
[[type::target]]                 typed link
[[type::target|display text]]    typed link with alias
target  = document title, optionally followed by #^<block-id>
type    = user-defined relationship name (see below)
```

- Examples: `[[Neural Nets]]`, `[[supports::Neural Nets|as shown here]]`,
  `[[Neural Nets#^a3f9]]`.
- Titles match case-insensitively, exact string. Titles containing `]]`,
  `|`, `::`, or `#^` are not linkable — reject/escape at the resolution
  layer, don't complicate the grammar.
- If multiple documents share a title, the link is **ambiguous** and
  treated like an unresolved link (visibly flagged, no edge indexed).
- A shared fixture file (`docs/link-grammar-fixtures.json`: input →
  expected parse) is the contract between the Go and JS parsers. The Go
  test suite must consume it; the JS side uses it at minimum as the
  manual-check reference.

## Data model

Schema changes in `store/store.go`:

```
CREATE NODE TABLE Block(id STRING PRIMARY KEY, text STRING, ord INT64)
CREATE REL TABLE HAS_BLOCK(FROM Document TO Block)
CREATE REL TABLE REFERENCES(FROM Block TO Document, FROM Block TO Block,
                            type STRING, display STRING)
```

- The existing, unused `LINKS_TO` table is **replaced** by `REFERENCES`
  (delete it; nothing writes to it today).
- A block is a non-empty line of content (paragraph = line, matching the
  prose editor model). `ord` is its position in the document.
- Relationship **types are free-form strings** on the edge — user-
  definable, no schema change to add one. The untyped `[[target]]` form
  indexes as type `"links"`. The UI's type picker offers the distinct
  types already in use plus a small seed set (`links`, `related`,
  `supports`, `contradicts`, `defines`, `source`); typing a new name
  creates it implicitly.

### Block identity across edits

Block IDs must be stable enough that block-targeted links don't rot:

- On save, the indexer reconciles the new paragraph list against the
  document's existing blocks: match by exact text first, then by
  order/proximity for edited paragraphs; create/delete the remainder.
  Unchanged and lightly-edited paragraphs keep their IDs.
- A block that is a **link target** (has incoming `REFERENCES`) gets its
  identity pinned in the text: the first time a block is targeted, a
  marker (` ^<block-id>`) is appended to that paragraph, and the
  reconciler treats the marker as authoritative. Untargeted blocks may
  tolerate heuristic reconciliation; targeted blocks must not.
- Marker rendering: the editor dims/hides `^id` markers the same way it
  decorates link syntax.

### Indexing pipeline (on every document save)

1. Parse content with the link grammar; split into paragraphs.
2. Reconcile `Block` nodes (above).
3. Delete all `REFERENCES` edges originating from this document's
   blocks; recreate from the parse. The reindex is **idempotent** —
   saving twice with no text change produces an identical graph.
4. Broadcast `refs.updated` (see API) for the saved doc and every doc
   whose incoming references changed.

Title-based linking makes two backend behaviors **mandatory**, or links
silently rot:

- **Rename propagation:** renaming a document rewrites `[[OldTitle...`
  occurrences to the new title in every referring document's content
  (then reindexes and broadcasts `doc.updated` for each). A rename must
  never break a resolved link.
- **Late resolution:** creating (or renaming) a document whose title
  matches existing unresolved links reindexes the documents containing
  them, so phantom links become live automatically. Unresolved links are
  *not* stored as edges (there is no target node); a content scan on
  create/rename is acceptable at this scale.

## API surface

- `GET /api/docs/{id}/references` →
  `{ "outgoing": [...], "incoming": [...] }`, each item:
  `{docId, docTitle, blockId, blockText, type, targetBlockId?}`.
- `GET /api/reftypes` → distinct `type` values in use (plus seeds), for
  the type picker.
- WS event `refs.updated {id}` on the existing multiplexed socket
  (`server/ws.go` `event` struct — new event *type*, not a new socket).
- Link autocomplete reuses `GET /api/docs` (titles are already listed).

## Editor requirements (v1 scope)

All four interactions below are in scope for the first implementation.
Widget-contract rules from `docs/widgets.md` apply: widgets emit events
up, `app.js` talks to the API and calls methods down.

**Enabling task — re-vendor the CodeMirror bundle.** The current
`web/static/vendor/codemirror.js` exports only `EditorView`,
`basicSetup`, `minimalSetup`. Link decoration, autocomplete, and hover
previews need more (`Decoration`, `ViewPlugin`, `WidgetType`,
`MatchDecorator`, `StateField`, `EditorState`, `keymap`,
`autocompletion`, `hoverTooltip`). Regenerate the same single-file
esm.sh bundle with those exports — the modules are already compiled in
via `basicSetup`, they just aren't exported. Still one vendored file, no
npm in the dev loop.

1. **Typed `[[wiki-links]]` with autocomplete.** Typing `[[` opens a
   title autocomplete (fed via a method by `app.js`, per the contract);
   `type::` prefix and `|alias` complete manually. Rendered links show
   the display text with syntax dimmed/hidden while the cursor is
   outside the link; resolved, unresolved, and ambiguous links are
   visually distinct.
2. **Link any selected word/phrase.** With a non-empty selection, a
   "link to…" action (keyboard shortcut + UI affordance) opens a target
   picker with an optional type; confirming wraps the selection as
   `[[type::Target|selected text]]`. Linked words are ordinary grammar
   links — no separate storage path.
3. **Click-to-navigate + hover preview.** Clicking a resolved link opens
   the target document's window, or focuses it if already open (the
   existing open-or-focus model in `app.js`); a `#^block` target scrolls
   to that block. Hovering shows a preview excerpt (targeted block, or
   the first blocks of the doc). Clicking an unresolved link offers to
   create the document with that title, pre-linked.
4. **Backlinks widget** (`<back-links>`, registered in `workspace.js`,
   so it appears in the Widgets menu automatically). Shows incoming
   references for the last-focused editor window's document, grouped by
   relationship type, each entry showing the source document title and
   the source block's text as context. Entries navigate on click. Live:
   updates on `refs.updated`, and retargets when editor-window focus
   changes.

## Non-functional requirements

- **Dependency budget unchanged:** zero new Go modules; no npm at
  runtime or in the dev loop; the CM re-vendor replaces the existing
  single file.
- **Save-path cost:** parsing + reconciliation + reindex runs on the
  (already debounced) autosave and must stay imperceptible for
  documents up to ~1 MB.
- **Recovery:** a full reindex-all routine (iterate documents, run the
  pipeline) must exist for schema migration and repair; wire it to a
  flag or run it on schema creation.
- **Multi-tab coherence:** all reference state changes flow through WS
  events so multiple browser tabs stay consistent (same rule the doc
  CRUD already follows).

## Phasing

Each phase builds, passes `go test ./...`, and is smoke-tested before
the next begins.

1. **Backend: storage + indexing.** Schema migration (Block/HAS_BLOCK/
   REFERENCES, drop LINKS_TO), Go link parser against the fixture file,
   block reconciliation, save-path reindex, rename propagation, late
   resolution, references + reftypes endpoints, `refs.updated` event.
   → verify: grammar fixtures pass; reconciliation keeps IDs across
   simulated edits; reindex idempotence test; rename rewrites referring
   docs; zero new `go.mod` entries.
2. **Editor: authoring + navigation.** Re-vendored CM bundle, link
   decorations (incl. `^id` markers), `[[` autocomplete, selection →
   link flow, click-to-navigate, hover preview, create-from-unresolved.
   → verify: manual smoke of each interaction; `doc-editor` still makes
   no API calls (contract intact).
3. **Backlinks widget.** `<back-links>` element, focus tracking, typed
   grouping, live updates.
   → verify: two-tab smoke test — editing a link in tab A updates the
   backlinks widget in tab B.
4. **Block targeting polish.** `#^` block picker in autocomplete,
   scroll-to-block on navigate, marker pinning UX.
   → verify: block link survives heavy editing of the target document.

## Explicitly out of scope (for now)

- Rendering/preview mode for markdown beyond link decoration.
- Transclusion/embedding (`![[...]]`) — natural later extension of the
  same grammar and block model; do not design against it.
- A visual graph-view widget (separate TODO item; this system provides
  the edges it will query).
- Reference metadata beyond `type` and `display` (weights, notes on
  edges) — the single-edge-table design leaves room; don't build it yet.
