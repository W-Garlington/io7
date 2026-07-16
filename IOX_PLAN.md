# IOX Architecture Plan

Status: draft — architecture direction, not yet implemented.

## Goals (from discussion)

- Tight, responsive editing experience — not "web app" laggy.
- Graph-backed storage via **LadybugDB** (embedded, in-process, Cypher) — chosen over hosted graph DBs because it's free and embeddable, no server to run. Bound via a **custom minimal cgo binding against LadybugDB's C API**, not the official `go-ladybug` Go module — see "Graph DB binding" below for why.
- Frontend: advanced text editing, file management, multiple floating/overlay "widgets" (extensible, tool-window style).
- Priorities, in order: small dependency footprint > extendability > ease of use (single command to launch/stop/access).

## High-level shape

One static Go binary. No separate DB server, no Node toolchain, no Electron/webview runtime.

```
┌─────────────────────────────────────────────┐
│  io7 binary (single process)                 │
│                                               │
│  ┌─────────────┐   ┌─────────────────────┐   │
│  │ LadybugDB   │   │ net/http server      │   │
│  │ (embedded,  │◄─►│  - REST (CRUD)       │   │
│  │  in-process,│   │  - WebSocket (live)  │   │
│  │  via cgo)   │   │  - static assets     │   │
│  └─────────────┘   │    (go:embed)        │   │
│                     └─────────┬───────────┘   │
└───────────────────────────────┼───────────────┘
                                │ localhost:PORT
                                ▼
                     ┌────────────────────┐
                     │ Browser tab/window  │
                     │  - CodeMirror 6     │
                     │  - Web Components   │
                     │    (panels/widgets) │
                     │  - zero npm runtime │
                     └────────────────────┘
```

Browser is the render target, but this is **not** a hosted web app — it binds to `127.0.0.1` only, starts/stops with the process, and the frontend ships pre-built and vendored (no build step, no `node_modules`, no supply-chain surface at runtime).

## Backend (Go)

- **Language/runtime:** stdlib `net/http`, `embed`, `encoding/json`. No web framework — routes are few enough not to need one.
- **Graph storage:** LadybugDB, accessed through a **hand-written cgo binding** (see "Graph DB binding" below), not `github.com/LadybugDB/go-ladybug`. Property graph model:
  - `Document` nodes (id, title, content ref, timestamps)
  - `Folder`/`Tag` nodes
  - Edges: `CONTAINS`, `LINKS_TO` (wiki-style backlinks), `TAGGED`
  - Cypher queries power backlinks, search-by-tag, and a future graph-view widget for free — this is the main payoff of picking a graph DB over flat files/SQLite.

### Graph DB binding — why not `go-ladybug`

The official Go module (`github.com/LadybugDB/go-ladybug`) unconditionally imports `github.com/apache/arrow-go/v18` in its main package (`arrow.go`, no build tag), which transitively pulls in Thrift, Brotli, lz4, and gonum — a large dependency tree for Arrow/Parquet interop this project doesn't need, and it violates the "small dependency footprint" priority.

LadybugDB's core repo ships a genuine, stable **C ABI** (`src/include/c_api/lbug.h`, ~178 `extern "C"` functions, pure C — no C++ required to consume). Plan: write our own thin cgo package (`graphdb/`) directly against that header — open/create DB, run a Cypher query, iterate results, close — roughly 200-300 lines. This gets:
- Zero extra Go dependencies (just cgo + the vendored native `liblbug.so`).
- No reliance on any third-party Go module's maintenance status for the DB layer.
- A native library that, unlike the Go-ecosystem alternatives evaluated (see below), is under genuinely active development.

**Alternatives evaluated and rejected** (checked via GitHub API commit history, not repo star/issue counts, which can be misleading):
- `github.com/LadybugDB/go-ladybug` — rejected for the Arrow-bloat reason above.
- **Cayley** (`cayleygraph/cayley`) — pure Go, Apache-2.0, embeddable — but last real commit is **July 2024**. Effectively unmaintained.
- **EliasDB** (`krotik/eliasdb`) — pure Go, MPL-2.0, minimal deps — but last commit is **August 2022**. More stale still.
- LadybugDB's core C++ engine, by contrast, had commits as recent as the day this plan was updated, and is the de facto center of gravity in the embedded-graph-DB space since KuzuDB was archived (Oct 2025) and LadybugDB forked from it.

**Native lib bootstrap** (verified working during a spike):
```bash
curl -fsSL https://raw.githubusercontent.com/LadybugDB/ladybug/refs/heads/main/scripts/download-liblbug.sh | LBUG_TARGET_DIR=<dir> bash
```
Downloads `liblbug.so` (or platform equivalent) + `lbug.h` + `lbug.hpp` for the current platform. Vendor this into `graphdb/lib-ladybug/` via a `go:generate` directive, gitignored (it's a compiled binary, re-downloaded per machine/CI run).
- **File management:** documents are graph nodes; content bodies can live either as a graph property or as plain files on disk with the graph as the index — decide this once we scope file sizes (large binary attachments should NOT go through cgo/graph, keep those as files on disk, graph just holds a path reference).
- **Live updates:** a single WebSocket endpoint multiplexing event types (doc changed, graph updated, file watcher event) rather than one socket per feature — keeps the frontend's connection management trivial and keeps multiple browser tabs in sync with each other.
- **File watching:** `fsnotify` (only non-stdlib, non-DB dependency needed on the backend) to detect external edits and push WS events.
- **Concurrency:** goroutines for the watcher and any background indexing; the DB itself is the source of truth so most handlers stay simple request/response.

### Backend dependency budget
| Dependency | Why | Alternative considered |
|---|---|---|
| none (custom cgo binding, stdlib `cgo` only) | direct binding to LadybugDB's C ABI, avoids `go-ladybug`'s Arrow baggage | `go-ladybug` (rejected — Arrow/Thrift/Brotli/lz4/gonum baggage), Cayley/EliasDB (rejected — unmaintained) |
| `fsnotify` | external file change detection | polling (rejected — wasteful, laggy) |
| a WS library (`nhooyr.io/websocket` or `gorilla/websocket`) | stdlib has no WS | roll our own frames (rejected — not worth it) |
| everything else | stdlib | — |

That's the entire backend dependency list: 2 non-stdlib Go modules (`fsnotify`, a WS library), plus the vendored native LadybugDB shared library consumed via a custom cgo binding (no Go module dependency for the DB layer at all).

## Frontend

- **Editor:** **CodeMirror 6**, not Monaco. CM6 is modular, ships as prebuilt ESM bundles (no bundler needed — vendor the built `.js` once, commit it, serve via `go:embed`), and is an order of magnitude smaller than Monaco. Monaco is the better choice only if we later want VS Code-grade IntelliSense/LSP UI out of the box — noted as a swap-in upgrade path, not the default.
- **UI shell:** vanilla JS + native **Web Components** for panels/widgets. No React/Vue — avoids an npm build chain entirely and keeps the "vendor one file, no `node_modules`" property intact. Each widget (file tree, graph view, search, floating tool window) is a self-contained custom element with a documented mount contract, so new widgets can be dropped in without touching core.
- **Overlays/floating widgets:** hand-rolled minimal docking/float layer using `position: fixed` + z-index stacking and a small drag/resize controller (a few hundred lines, no dependency). This is deliberately not a vendored docking library (e.g. dockview) up front — added later only if hand-rolling proves insufficient, since it's the single biggest optional-dependency temptation in the whole plan.
- **Multi-window:** for true OS-level separate windows (vs. in-page overlays), rely on the browser's own `window.open()` against the same localhost origin rather than building window management — every window shares the same backend/DB via WebSocket.
- **Build step:** none at runtime. A one-time vendoring step downloads/builds the CM6 bundle and commits it under `frontend/web/vendor/`. Regular development is edit-HTML/JS-refresh-browser, no compile step, no npm install in the normal dev loop.

### Frontend dependency budget
| Dependency | Why | Alternative considered |
|---|---|---|
| CodeMirror 6 (vendored build, not npm-managed at runtime) | best available text-editing engine (virtualized rendering, IME/unicode, language modes) without rebuilding one from scratch | Monaco (heavier), hand-rolled editor (rejected — reinventing a solved, hard problem) |
| none else | Web Components + hand-rolled docking cover the rest | React/Vue (rejected — pulls in a build chain and dependency tree for layout we can do in CSS) |

## Launch / stop / access

- `io7 serve` (or just running the binary) starts the HTTP+WS server bound to `127.0.0.1:<port>` and opens the default browser to it automatically (cross-platform `open`/`xdg-open`/`start` shellout — small, no dependency).
- Stop: Ctrl-C (graceful shutdown via `context`), or a localhost-only `/shutdown` endpoint for scripting.
- Optional: a systray icon (`fyne.io/systray`, already present transitively from the current Fyne build) for "open/stop" without a terminal — decide later whether that's worth keeping a Fyne-adjacent dependency around once the native Fyne UI is retired.
- No install step beyond running the binary — DB and frontend assets are embedded in it.
- Security: server never binds beyond loopback by default; add a token/flag explicitly if remote access is ever wanted.

## Open decisions

1. **Content storage split** — how much lives in the graph vs. on disk (matters once attachment/file sizes are known).
2. **WS library pick** — `nhooyr.io/websocket` (simpler, context-based) vs `gorilla/websocket` (more established, more examples). Leaning `nhooyr.io/websocket` for the smaller API surface.
3. **Fate of the current Fyne frontend** — this plan replaces it; existing `frontend/` package would be removed once the web frontend reaches parity, not before.
4. **Docking library** — hand-rolled vs. vendoring dockview/golden-layout, revisit once overlay requirements are concrete.

~~Graph DB binding approach~~ — resolved, see "Graph DB binding" above (custom cgo against LadybugDB's C API).

## Lessons learned (cgo linking, for whoever implements `graphdb/`)

When binding directly against `lbug.h` in our own package (not through `go-ladybug`), declare both CFLAGS and LDFLAGS in that package's own cgo comment, `${SRCDIR}`-relative:
```c
#cgo CFLAGS: -I${SRCDIR}/lib-ladybug
#cgo linux LDFLAGS: -L${SRCDIR}/lib-ladybug -Wl,-rpath,${SRCDIR}/lib-ladybug
#cgo darwin LDFLAGS: -L${SRCDIR}/lib-ladybug -Wl,-rpath,${SRCDIR}/lib-ladybug
#cgo windows LDFLAGS: -L${SRCDIR}/lib-ladybug
```
This works cleanly here because we own the package doing `#include "lbug.h"` directly — unlike `go-ladybug`'s `system_ladybug` build tag variant, which declares LDFLAGS but not CFLAGS in its own package, so the header path has to come from a global `CGO_CFLAGS` env var instead. Since we're not using their package, this doesn't apply to us, but don't rediscover it the hard way.

## Non-goals (for now)

- No Electron/Tauri/webview embedding — plain browser + localhost server.
- No npm/webpack build chain in the repo.
- No hosted/remote multi-user access — single local user, single machine.
