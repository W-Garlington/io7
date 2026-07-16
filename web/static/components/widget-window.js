// <widget-window> — floating container that makes any widget movable and
// resizable inside #workspace. This is the foundation for all floating
// widgets; see docs/widgets.md for the full guide.
//
// Usage: wrap a widget in the markup (or create one dynamically):
//
//   <widget-window window-title="Editor" x="24" y="16" width="760" height="520">
//     <doc-editor></doc-editor>
//   </widget-window>
//
// Attributes: window-title, x, y, width, height (px), closable,
//             editable-title (title bar becomes an input the user can edit)
// Methods:    setTitle(text), getTitle(), setDirty(on), focusTitle(),
//             bringToFront()
// Events:     'window-close'  — fired when the × of a `closable` window is
//                               clicked; the host decides what to do.
//             'title-change' {title} — user edited an editable title.
//
// Follows the widget contract (see doc-list.js): light DOM, methods down,
// bubbling CustomEvents up. Dragging clamps so the title bar always stays
// reachable inside the workspace. Pointer capture keeps drag/resize
// working when the cursor outruns the element.

let topZ = 10; // shared z-order counter; last-focused window wins

const MIN_WIDTH = 280;
const MIN_HEIGHT = 160;

class WidgetWindow extends HTMLElement {
  #titleEl = null;
  #dirtyDot = null;

  connectedCallback() {
    // Idempotent: only build the chrome on first connect.
    if (this.#titleEl) return;

    const content = [...this.childNodes];

    if (this.hasAttribute('editable-title')) {
      this.#titleEl = document.createElement('input');
      this.#titleEl.className = 'window-title-input';
      this.#titleEl.placeholder = 'Untitled';
      this.#titleEl.value = this.getAttribute('window-title') ?? '';
      // 'input' only fires on user edits, never on setTitle().
      this.#titleEl.addEventListener('input', () => this.dispatchEvent(
        new CustomEvent('title-change', { detail: { title: this.#titleEl.value }, bubbles: true })));
    } else {
      this.#titleEl = document.createElement('span');
      this.#titleEl.className = 'window-title';
      this.#titleEl.textContent = this.getAttribute('window-title') ?? '';
    }

    this.#dirtyDot = document.createElement('span');
    this.#dirtyDot.className = 'window-dirty';
    this.#dirtyDot.textContent = '•';
    this.#dirtyDot.title = 'Unsaved changes';
    this.#dirtyDot.hidden = true;

    const header = document.createElement('div');
    header.className = 'window-header';
    header.append(this.#titleEl, this.#dirtyDot);

    if (this.hasAttribute('closable')) {
      const close = document.createElement('button');
      close.className = 'window-close';
      close.textContent = '×';
      close.title = 'Close';
      close.onclick = () =>
        this.dispatchEvent(new CustomEvent('window-close', { bubbles: true }));
      header.append(close);
    }

    const body = document.createElement('div');
    body.className = 'window-body';
    body.append(...content);

    this.replaceChildren(header, body);

    this.style.left = `${Number(this.getAttribute('x') ?? 24)}px`;
    this.style.top = `${Number(this.getAttribute('y') ?? 24)}px`;
    // Clamp the requested geometry so the window starts inside the
    // workspace even if the attributes ask for more than fits.
    this.#resizeTo(Number(this.getAttribute('width') ?? 520),
      Number(this.getAttribute('height') ?? 360));
    this.#moveTo(this.offsetLeft, this.offsetTop);

    const grip = document.createElement('div');
    grip.className = 'window-resize';
    this.append(grip);

    this.addEventListener('pointerdown', () => this.bringToFront(), true);
    this.#draggable(header, (dx, dy, start) => this.#moveTo(start.x + dx, start.y + dy));
    this.#draggable(grip, (dx, dy, start) => this.#resizeTo(start.w + dx, start.h + dy));
    this.bringToFront();
  }

  setTitle(text) {
    if (this.#titleEl instanceof HTMLInputElement) {
      this.#titleEl.value = text;
    } else {
      this.#titleEl.textContent = text;
    }
  }

  getTitle() {
    return this.#titleEl instanceof HTMLInputElement
      ? this.#titleEl.value
      : this.#titleEl.textContent;
  }

  setDirty(on) {
    this.#dirtyDot.hidden = !on;
  }

  focusTitle() {
    if (this.#titleEl instanceof HTMLInputElement) {
      this.#titleEl.focus();
      this.#titleEl.select();
    }
  }

  bringToFront() {
    this.style.zIndex = ++topZ;
  }

  // #draggable wires pointer drag on handle, reporting deltas from the
  // pointer-down position plus the geometry captured at drag start.
  #draggable(handle, apply) {
    handle.addEventListener('pointerdown', (ev) => {
      // Interactive header controls (title input, close button) must not
      // start a drag.
      if (ev.button !== 0 || ev.target.closest('input, button')) return;
      ev.preventDefault();
      const from = { x: ev.clientX, y: ev.clientY };
      const start = {
        x: this.offsetLeft, y: this.offsetTop,
        w: this.offsetWidth, h: this.offsetHeight,
      };
      const move = (e) => apply(e.clientX - from.x, e.clientY - from.y, start);
      const stop = () => {
        handle.removeEventListener('pointermove', move);
        handle.removeEventListener('pointerup', stop);
      };
      handle.setPointerCapture(ev.pointerId);
      handle.addEventListener('pointermove', move);
      handle.addEventListener('pointerup', stop);
    });
  }

  // Windows are fully contained: they can never move or grow past the
  // workspace edges.
  #moveTo(x, y) {
    const bounds = this.parentElement;
    this.style.left = `${clamp(x, 0, Math.max(0, bounds.clientWidth - this.offsetWidth))}px`;
    this.style.top = `${clamp(y, 0, Math.max(0, bounds.clientHeight - this.offsetHeight))}px`;
  }

  #resizeTo(w, h) {
    const bounds = this.parentElement;
    this.style.width = `${clamp(w, MIN_WIDTH, Math.max(MIN_WIDTH, bounds.clientWidth - this.offsetLeft))}px`;
    this.style.height = `${clamp(h, MIN_HEIGHT, Math.max(MIN_HEIGHT, bounds.clientHeight - this.offsetTop))}px`;
  }
}

const clamp = (v, lo, hi) => Math.min(Math.max(v, lo), hi);

customElements.define('widget-window', WidgetWindow);
