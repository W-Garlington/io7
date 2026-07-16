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

Current widgets:

| Element | File | Methods (down) | Events (up) |
|---|---|---|---|
| `<doc-list>` | `components/doc-list.js` | `setDocs(docs)`, `setActive(id)` | `doc-create`, `doc-select {id}`, `doc-delete {id}` |
| `<doc-editor>` | `components/doc-editor.js` | `open(content)`, `close()`, `getContent()` | `doc-change` |
| `<widget-window>` | `components/widget-window.js` | `setTitle(text)`, `bringToFront()` | `window-close` |

## Floating windows: `<widget-window>`

`<widget-window>` is the foundation for floating widgets. It is a generic
container — it knows nothing about what's inside it. Wrap any widget in
one and it becomes a movable, resizable window inside `#workspace`:

```html
<main id="workspace">
  <widget-window window-title="Editor" x="32" y="24" width="760" height="520">
    <doc-editor></doc-editor>
  </widget-window>
</main>
```

Or create one dynamically:

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

### API

- **Attributes** (read once on connect): `window-title`; `x`, `y`,
  `width`, `height` in px (defaults 24, 24, 520, 360); `closable`
  (presence adds an × button).
- **Methods**: `setTitle(text)`; `bringToFront()`.
- **Events**: `window-close` — fired when the × is clicked. The window
  does **not** remove itself; the host decides (hide, remove, confirm…).

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
- **Move clamping** keeps at least 48px of the title bar inside the
  workspace, so a window can never be dragged out of reach. Resizing
  enforces 280×160 minimums.
- **Z-order**: a module-level counter; any `pointerdown` inside a window
  raises it above all others. No bookkeeping, monotonically increasing.
- Styling lives in `style.css` under "widget-window" — components carry
  no inline styles beyond geometry (`left/top/width/height/z-index`).

## Adding a new floating widget — checklist

1. Create `web/static/components/<name>.js` defining the custom element.
   Follow the contract: methods down, bubbling `CustomEvent`s up, no API
   calls inside the widget.
2. Add its styles to `style.css` (namespace selectors under the element
   name, e.g. `search-panel .result { … }`).
3. Import it in `app.js` (`import '/components/<name>.js';`) and wire its
   events to `api.js`/`live.js` there.
4. Mount it: either statically in `index.html` wrapped in a
   `<widget-window>`, or dynamically as in the snippet above.
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
