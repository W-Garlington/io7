# TODO

Task backlog for implementing `IOX_PLAN.md`. Read that file first for the architecture and rationale — this file is just the ordered work list. Phases 1–3 and the editor pane / basic file management of phase 4 are implemented (see checkboxes); a prior spike was done and then fully reverted (see `IOX_PLAN.md`'s "Graph DB binding" section for what was learned from it).

Suggested order: graph DB binding → backend skeleton → frontend skeleton → widgets. Each phase should build and be manually smoke-tested before moving to the next.

## 1. Graph DB binding (`graphdb/` package)

- [x] Add `go:generate` directive to fetch the native lib:
  `curl -fsSL https://raw.githubusercontent.com/LadybugDB/ladybug/refs/heads/main/scripts/download-liblbug.sh | LBUG_TARGET_DIR=$(pwd)/lib-ladybug bash`
  into `graphdb/lib-ladybug/` (gitignore this directory — it's a downloaded binary + headers, not source).
- [x] Write a minimal cgo wrapper directly against `lbug.h` (the C ABI, NOT the `go-ladybug` Go module — see `IOX_PLAN.md` for why). Needed surface: open/create database, run a Cypher query, iterate/read query results, close connection, error handling.
- [x] cgo directives go in the wrapper file itself (`${SRCDIR}`-relative CFLAGS + LDFLAGS) — see the "Lessons learned" section in `IOX_PLAN.md` before reinventing this.
- [x] Verify: `go build ./...` succeeds, `go.mod` gains **zero** new non-stdlib dependencies from this package.
- [x] Define the property graph schema from `IOX_PLAN.md` (schema lives in `store/`; graphdb tests cover create/link/query): `Document`/`Folder`/`Tag` nodes, `CONTAINS`/`LINKS_TO`/`TAGGED` edges. Write a smoke test: create a doc node, link it, query it back.

## 2. Backend skeleton

- [x] WS library: `github.com/coder/websocket` (the maintained successor of `nhooyr.io/websocket`, same minimal API). `fsnotify` deferred — no on-disk content to watch yet (see storage decision below).
- [x] `net/http` server: REST CRUD for documents backed by `store/` (`graphdb` is not touched by handlers directly). Folders/tags: schema exists, endpoints not yet.
- [x] Single WebSocket endpoint multiplexing event types (doc changed, graph updated, fs event) — not one socket per feature.
- [ ] File watcher goroutine via `fsnotify`, pushes WS events on external changes. (Deferred with the storage decision — nothing on disk to watch yet.)
- [x] Decided (for now): content bodies live as a graph property — documents are small plain text. Revisit when attachments/large files arrive; those should be disk files with a path reference in the graph.
- [x] Launch/stop UX: bind `127.0.0.1:<port>` only, auto-open default browser (`xdg-open`/`open`/`start` shellout), graceful shutdown on Ctrl-C via `context`, plus a localhost-only `/shutdown` endpoint.

## 3. Frontend skeleton

- [x] Vendor a prebuilt CodeMirror 6 ESM bundle — lives under `web/static/vendor/` (`web/` is its own embed package; `frontend/` stays pure Fyne until decommission) — one-time download, committed, no npm/`node_modules` at runtime or in the dev loop.
- [x] Static HTML/JS shell served via `go:embed`, vanilla JS + Web Components (no React/Vue/build step).
- [ ] Hand-rolled floating/docking panel layer (`position: fixed` + z-index + a small drag/resize controller). Don't reach for a docking library (dockview etc.) unless hand-rolling proves genuinely insufficient — see `IOX_PLAN.md` non-goals.
- [x] Wire the WS connection + basic REST calls from the frontend shell to the backend.

## 4. Widgets

- [x] Editor pane (CodeMirror 6 instance bound to a `Document`), with debounced autosave + Ctrl-S.
- [x] Flat document list widget (create/open/delete, backed by REST CRUD). Tree view (folders via `CONTAINS`) still to do.
- [ ] Graph view widget — this is the payoff of having a real graph DB; a Cypher query for backlinks/tags feeding some minimal force-directed or list-based visualization. Keep the first version simple (e.g. a backlinks list) before attempting a visual graph render.
- [ ] Search widget.

## 5. Cleanup / decommission

- [ ] Once the web frontend reaches parity with the current Fyne UI: remove `frontend/app.go`, `frontend/layout.go`, and the Fyne-related `go.mod` entries (`fyne.io/*`, `github.com/go-gl/*`, `github.com/fyne-io/*`, etc.). Don't remove before parity — `main.go` now starts the web server, but the Fyne packages stay in-tree (and in `go.mod`) until then.
- [ ] Re-run `govulncheck ./...` once the new deps (cgo binding, WS lib, fsnotify) are in place. One known, currently-unreached vuln to revisit: `github.com/yuin/goldmark` XSS (GO-2026-5320, fixed in v1.7.17) — only matters if the frontend ends up rendering markdown through it.

## Notes for whoever picks this up

- Go toolchain, gcc, and Fyne's X11/GL dev libs are already installed on this machine (done earlier this session) — a plain `go build ./...` on the current `main` works today, before any of the above.
- Don't re-add `github.com/LadybugDB/go-ladybug` as a dependency — it unconditionally imports Apache Arrow in its main package (confirmed by reading its source), which drags in Thrift/Brotli/lz4/gonum and violates the small-dependency-footprint priority. This was tried, measured, and reverted earlier — see git history / this file's history if curious.
- Cayley and EliasDB (pure-Go graph DB alternatives) were evaluated and rejected for being unmaintained (last real commits July 2024 and August 2022 respectively, verified via GitHub API commit history, not star/issue counts).
