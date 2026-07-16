# Frontend widgets and floating windows

How io7's frontend widgets work, how the floating-window layer is built,
and how to add a new widget. Architecture rationale lives in `IOX_PLAN.md`
("Frontend"); this documents the implemented mechanics.

## Ground rules

- **No build step.** Everything under `web/static/` is served as-is via
  `go:embed`. Plain ES modules, no npm, no bundler. The only vendored
  artifact is `web/static/vendor/codemirror.js` (a self-contained
  CodeMirror 6 ESM bundle, fetched once from esm.sh and committed).
- **Native Web Components, light DOM.** Widgets are custom elements that
  render into their own light DOM — no shadow roots, so one global
  stylesheet (`style.css`) themes everything.
- **No framework.** State lives in `app.js`; widgets are dumb views.

## The widget contract

Every widget is a self-contained custom element in
`web/static/components/`, and communicates in exactly two directions:

- **Downward** — the host calls methods on the element
  (e.g. `docList.setDocs(docs)`, `editor.open(content)`).
- **Upward** — the element dispatches **bubbling `CustomEvent`s**
  (e.g. `doc-select`, `doc-change`). Widgets never call the REST API,
  never import other widgets, and never reach into each other's DOM.

`app.js` is the single orchestrator: it listens for widget events, talks
to the backend (`api.js` for REST, `live.js` for WebSocket events), and
pushes results back down into widgets. Adding a feature usually means
"add an event or method to one widget, handle it in `app.js`".

Two rules that follow from windows spawning and closing at runtime:

- **`connectedCallback` must be idempotent.** `<widget-window>` moves its
  children into its body container on connect, which disconnects and
  reconnects them — firing their `connectedCallback` again. Guard with a
  private field (`if (this.#built) return;` style), or you'll build your
  widget twice (this was a real bug: two CodeMirror editors in one
  window).
- **Don't hold widget references; delegate.** `app.js` listens on
  `document` (widget events bubble) and looks elements up per use
  (`document.querySelector('doc-editor')`), so handlers keep working for
  widgets created after boot, any number of instances, and windows the
  user has closed. State pushes iterate all instances
  (`querySelectorAll('doc-list')`).

Current widgets:

| Element | File | Methods (down) | Events (up) |
|---|---|---|---|
| `<doc-list>` | `components/doc-list.js` | `setDocs(docs)` | `doc-create`, `doc-select {id}`, `doc-delete {id}` |
| `<doc-editor>` | `components/doc-editor.js` | `setContent(content)`, `getContent()` | `doc-change` |

`<doc-editor>` is tuned for prose, not code: Tab inserts an indent
(Shift+Tab outdents the touched lines), a hard line is a paragraph
(spacing via `.cm-line` padding), the content column has a serif face and
a centered ~72ch reading measure, and browser spellcheck/autocapitalize
are on. Tab handling is a DOM `keydown` listener because the vendored
CodeMirror bundle exports no keymap helpers — basicSetup leaves Tab
unbound, so the event bubbles out of CM untouched.
| `<nav-menu>` | `components/nav-menu.js` | `setItems([{name, title}])` | `widget-spawn {name}` |
| `<widget-window>` | `components/widget-window.js` | `setTitle(text)`, `getTitle()`, `setDirty(on)`, `focusTitle()`, `bringToFront()` | `window-close`, `title-change {title}` |

## Document windows

The workspace is windows-only (no fixed panes): documents each open in
their own floating editor window, tracked by `data-doc-id` on the window.
`app.js` implements the model:

- **Selecting** a doc in a `<doc-list>` opens its window, or focuses the
  already-open one (never two windows for the same doc — they would
  fight over saves).
- **Creating** a doc ("+ New document" in a list, or "New document" in
  the Widgets menu) creates it via the API and opens a fresh window with
  the title field focused.
- **Deleting** (the per-row `%` button — placeholder glyph) removes the
  doc and closes its window everywhere, including other browser tabs
  (via the `doc.deleted` WebSocket event).
- **Renaming**: the window title bar *is* the doc's title field — editor
  windows use widget-window's `editable-title` mode (set via the
  registry's `editableTitle` flag), which swaps the title span for an
  input that emits `title-change`. `<doc-editor>` itself is content-only.
- **Saving** is per window: title (window) + content (editor) autosave
  (debounced) together; per-editor state lives in a `WeakMap` in
  `app.js`. Unsaved state shows as a dot next to the title
  (`setDirty(on)`). Ctrl-S flushes every dirty editor; closing a window
  flushes its editor first.

## Floating windows: `<widget-window>`

`<widget-window>` is the foundation for floating widgets. It is a generic
container — it knows nothing about what's inside it. Wrap any widget in
one and it becomes a movable, resizable window inside `#workspace`.
Windows are normally created through the registry (next section), but the
element also works standalone:

```js
const win = document.createElement('widget-window');
win.setAttribute('window-title', 'Search');
win.setAttribute('closable', '');
win.setAttribute('x', '120');
win.setAttribute('y', '80');
win.append(document.createElement('search-panel'));
win.addEventListener('window-close', () => win.remove());
document.getElementById('workspace').append(win);
```

## Spawning windows: the workspace registry

`workspace.js` keeps a registry of spawnable widgets and manages their
windows. `app.js` registers each widget once at boot:

```js
workspace.register('documents', {
  title: 'Documents',   // window title + nav-menu label
  tag: 'doc-list',      // custom element created inside the window
  width: 260, height: 420,
  // singleton: true    // would make spawn() focus the existing window
});
```

- `workspace.spawn(name)` opens the window (cascaded position, closable,
  removed from the DOM on close) and returns `{win, el, created}`.
- `workspace.widgets()` lists registrations for menus — the topbar
  `<nav-menu>` is populated from it, so **registering a widget is all it
  takes to appear in the "Widgets" dropdown**. The dropdown emits
  `widget-spawn {name}`; `app.js` spawns and then *hydrates* the new
  instance with current state (a new `doc-list` gets `setDocs`). The
  `editor` registration is special-cased in `app.js`: spawning it
  creates a new document first (an editor window always edits a doc).
- Every spawned window has a close (×) button. Closing only removes the
  window; app-level consequences are handled in `app.js` — e.g. closing
  an editor window flushes any pending save first (a capture-phase
  `window-close` listener reads the editor before the window's own
  listener removes it).

### API

- **Attributes** (read once on connect): `window-title`; `x`, `y`,
  `width`, `height` in px (defaults 24, 24, 520, 360; clamped to fit the
  workspace); `closable` (presence adds an × button); `editable-title`
  (title bar becomes an input).
- **Methods**: `setTitle(text)`, `getTitle()`, `setDirty(on)` (shows a
  dot next to the title), `focusTitle()` (editable titles only),
  `bringToFront()`.
- **Events**: `window-close` — fired when the × is clicked; the window
  does **not** remove itself; the host decides (workspace.js removes
  spawned windows; capture-phase listeners can act first).
  `title-change {title}` — user edited an editable title.

### How it works (~120 lines, no dependencies)

- On `connectedCallback` the element moves its original children into a
  `.window-body` div and prepends a `.window-header` title bar and a
  `.window-resize` corner grip.
- Windows are `position: absolute` inside `#workspace`
  (`position: relative; overflow: hidden`) — the deliberate hand-rolled
  approach from `IOX_PLAN.md`, chosen over vendoring a docking library.
- **Dragging** the header and **resizing** the grip share one helper
  (`#draggable`): on `pointerdown` it captures the pointer
  (`setPointerCapture`, so fast drags outside the element keep working),
  records the starting geometry, and applies pointer deltas on
  `pointermove`. CSS `touch-action: none` on both handles keeps touch
  input from scrolling instead.
- **Windows are fully contained**: moving and resizing are clamped to the
  workspace edges (and initial geometry is clamped on connect), so a
  window can never end up out of reach. Resizing enforces 280×160
  minimums.
- **Z-order**: a module-level counter; any `pointerdown` inside a window
  raises it above all others. No bookkeeping, monotonically increasing.
- Styling lives in `style.css` under "widget-window" — components carry
  no inline styles beyond geometry (`left/top/width/height/z-index`).

## Adding a new floating widget — checklist

1. Create `web/static/components/<name>.js` defining the custom element.
   Follow the contract: methods down, bubbling `CustomEvent`s up, no API
   calls inside the widget — and an idempotent `connectedCallback`.
2. Add its styles to `style.css` (namespace selectors under the element
   name, e.g. `search-panel .result { … }`).
3. In `app.js`: import it, `workspace.register('<name>', {title, tag, …})`
   it, and handle its events with `document.addEventListener` (delegated).
   Registration alone puts it in the "Widgets" dropdown.
4. If spawned instances need current state, hydrate them in the
   `widget-spawn` handler in `app.js`.
5. If it needs live updates, subscribe via `live.on('<event.type>', cb)` —
   one WebSocket serves all widgets; new event *types* are added on the
   backend (`server/ws.go` `event` struct), never new sockets.

## Known limitations / future work

- **No geometry persistence** — window positions reset on reload.
  Natural next step: save `{id: {x,y,w,h}}` to `localStorage` on drag/
  resize end, restore on connect.
- **No snapping/docking/maximize** — plan says hand-roll first, vendor a
  docking library only if this proves insufficient.
- **Workspace resize** — windows are clamped while dragging, but shrinking
  the browser window can leave a window's title bar out of view until
  it's dragged again (re-clamp on `ResizeObserver` would fix this).
- **Keyboard accessibility** — moving/resizing is pointer-only.
- **True OS windows** — per `IOX_PLAN.md`, separate OS-level windows
  should use `window.open()` against the same origin, not this layer.
