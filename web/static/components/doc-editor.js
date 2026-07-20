// <doc-editor> — a document's content view: one CodeMirror 6 instance,
// tuned for prose writing (the primary use case), not code:
//   - Tab indents (inserts a tab stop, Word-style); Shift+Tab outdents
//     the current line(s)
//   - paragraph = a line; spacing and a comfortable serif reading
//     measure come from style.css
//   - browser spellcheck/autocapitalize are enabled on the content
// The document's title is edited in the hosting window's title bar
// (widget-window's editable-title mode), not here.
//
// Wiki links (docs/references.md): [[target]] markup renders as a
// clickable pill (syntax revealed while the cursor is inside), `[[`
// opens title autocomplete, Ctrl/Cmd-K wraps the selection as a link,
// and hovering a resolved link shows a preview. The JS side of the link
// grammar mirrors store/links.go — the contract is
// docs/link-grammar-fixtures.json; change both together.
//
// Methods: setContent(content), getContent(),
//          setDocs(docs)             — [{id, title}] for link resolution
//                                      and autocomplete
//          setRefTypes(types)        — relationship types for `type::`
//                                      completion
//          setPreviewProvider(fn)    — async (docId, blockId) => text;
//                                      injected by app.js so the widget
//                                      itself never calls the API
// Events:  'doc-change' — fired on user edits only, never on setContent().
//          'link-click' {target, block, type, display, docId} — docId is
//              the resolved target document, or null when unresolved.
//
// See doc-list.js for the widget contract.

import {
  EditorView, basicSetup, Decoration, WidgetType, ViewPlugin, MatchDecorator,
  StateEffect, autocompletion, startCompletion, hoverTooltip,
} from '/vendor/codemirror.js';

// The frozen link grammar (docs/references.md). Mirrors store/links.go.
const LINK_RE = /\[\[([^\[\]|\n]+)(?:\|([^\[\]|\n]*))?\]\]/g;
const PIN_RE = /[ \t]\^[0-9a-f]{4,32}/g; // + "must end its line", checked in decorate

// parseLink turns a LINK_RE match into {type, target, block, display},
// or null when the target trims to nothing.
function parseLink(m) {
  let inner = m[1];
  const display = (m[2] ?? '').trim();
  let type = 'links';
  const dc = inner.indexOf('::');
  if (dc >= 0) {
    const t = inner.slice(0, dc).trim().toLowerCase();
    if (t) type = t;
    inner = inner.slice(dc + 2);
  }
  let block = '';
  const h = inner.indexOf('#^');
  if (h >= 0) {
    block = inner.slice(h + 2).trim();
    inner = inner.slice(0, h);
  }
  const target = inner.trim();
  return target ? { type, target, block, display } : null;
}

// Signals the decorator that link resolution data changed (doc created/
// renamed/deleted), so pill colors refresh without a text edit.
const titlesChanged = StateEffect.define();

// The rendered form of a link when the cursor is elsewhere.
class LinkWidget extends WidgetType {
  constructor(link, status, host) {
    super();
    this.link = link;
    this.status = status; // '' resolved | 'unresolved' | 'ambiguous'
    this.host = host;
  }

  eq(other) {
    return other.status === this.status &&
      other.link.target === this.link.target &&
      other.link.display === this.link.display &&
      other.link.type === this.link.type &&
      other.link.block === this.link.block;
  }

  toDOM() {
    const a = document.createElement('a');
    a.className = 'io7-link' + (this.status ? ` io7-link--${this.status}` : '');
    a.dataset.reftype = this.link.type;
    a.textContent = this.link.display || this.link.target;
    a.href = '#';
    a.title = this.link.target + (this.link.block ? `#^${this.link.block}` : '');
    a.addEventListener('click', (ev) => {
      ev.preventDefault();
      this.host.emitLinkClick(this.link);
    });
    return a;
  }
}

class DocEditor extends HTMLElement {
  #view = null;
  #applying = false; // true while setContent() replaces content
  #titles = new Map(); // lower(title) -> doc id, '' when ambiguous
  #titleList = []; // display-case titles for autocomplete
  #refTypes = [];
  #preview = null; // async (docId, blockId) => text

  connectedCallback() {
    // Idempotent: re-parenting (e.g. widget-window moving us into its
    // body) re-fires this callback; the view must only be built once.
    if (this.#view) return;

    this.#view = new EditorView({
      extensions: [
        basicSetup,
        EditorView.lineWrapping,
        // Word-like niceties from the browser's own text machinery.
        EditorView.contentAttributes.of({
          spellcheck: 'true',
          autocapitalize: 'sentences',
          autocorrect: 'on',
        }),
        EditorView.updateListener.of((update) => {
          if (update.docChanged && !this.#applying) {
            this.dispatchEvent(new CustomEvent('doc-change', { bubbles: true }));
          }
        }),
        this.#linkDecorations(),
        this.#pinDecorations(),
        autocompletion({ override: [this.#completions], icons: false }),
        this.#hoverPreview(),
      ],
      parent: this,
    });

    // Tab/Shift+Tab indent handling. basicSetup deliberately leaves Tab
    // unbound, so a plain keydown listener does the job: CM ignores Tab,
    // the event bubbles here. Ctrl/Cmd-K ("link to…") rides the same
    // listener.
    this.addEventListener('keydown', (ev) => {
      if ((ev.ctrlKey || ev.metaKey) && ev.key === 'k') {
        ev.preventDefault();
        this.#linkSelection();
        return;
      }
      if (ev.key !== 'Tab') return;
      ev.preventDefault();
      if (ev.shiftKey) {
        this.#outdent();
      } else {
        this.#view.dispatch(this.#view.state.replaceSelection('\t'));
      }
    });
  }

  // --- wiki links ---

  #status(target) {
    const id = this.#titles.get(target.toLowerCase());
    if (id === undefined) return 'unresolved';
    if (id === '') return 'ambiguous';
    return '';
  }

  #resolve(target) {
    return this.#titles.get(target.toLowerCase()) || null;
  }

  emitLinkClick(link) {
    this.dispatchEvent(new CustomEvent('link-click', {
      bubbles: true,
      detail: { ...link, docId: this.#resolve(link.target) },
    }));
  }

  // Links render as pills except where the selection touches them — there
  // the raw syntax shows (marked) so it can be edited in place.
  #linkDecorations() {
    const host = this;
    const decorator = new MatchDecorator({
      regexp: LINK_RE,
      decorate: (add, from, to, m, view) => {
        const link = parseLink(m);
        if (!link) return;
        const sel = view.state.selection.main;
        if (sel.from <= to && sel.to >= from) {
          add(from, to, Decoration.mark({ class: 'io7-link-src' }));
        } else {
          add(from, to, Decoration.replace({
            widget: new LinkWidget(link, host.#status(link.target), host),
          }));
        }
      },
    });
    return ViewPlugin.fromClass(class {
      constructor(view) { this.deco = decorator.createDeco(view); }
      update(u) {
        if (u.docChanged || u.selectionSet || u.viewportChanged ||
            u.transactions.some((tr) => tr.effects.some((e) => e.is(titlesChanged)))) {
          this.deco = decorator.createDeco(u.view);
        }
      }
    }, { decorations: (v) => v.deco });
  }

  // Block-id pin markers (" ^a3f91c2b" at line end) are metadata for
  // block-targeted links — dim them rather than hide, so they survive
  // editing knowingly.
  #pinDecorations() {
    const decorator = new MatchDecorator({
      regexp: PIN_RE,
      decorate: (add, from, to, m, view) => {
        if (view.state.doc.lineAt(to).to !== to) return; // must end its line
        add(from, to, Decoration.mark({ class: 'io7-pin' }));
      },
    });
    return ViewPlugin.fromClass(class {
      constructor(view) { this.deco = decorator.createDeco(view); }
      update(u) {
        if (u.docChanged || u.viewportChanged) this.deco = decorator.createDeco(u.view);
      }
    }, { decorations: (v) => v.deco });
  }

  // Autocomplete inside an open [[…: document titles, plus `type::`
  // prefixes while no type is present yet.
  #completions = (ctx) => {
    const m = ctx.matchBefore(/\[\[[^\[\]|\n]*$/);
    if (!m) return null;
    let from = m.from + 2;
    let text = ctx.state.sliceDoc(from, ctx.pos);
    const dc = text.indexOf('::');
    const hasType = dc >= 0;
    if (hasType) from += dc + 2;
    const options = this.#titleList.map((title) => ({
      label: title,
      apply: applyTitle,
    }));
    if (!hasType) {
      for (const t of this.#refTypes) {
        options.push({ label: `${t}::`, boost: -1, apply: applyType });
      }
    }
    return { from, options, validFor: /^[^\[\]|\n]*$/ };
  };

  // Ctrl/Cmd-K: wrap the selection as [[|selection]] (a "linked word" —
  // ordinary grammar, no separate storage) and open the target picker.
  // With no selection, just start a link.
  #linkSelection() {
    const { state } = this.#view;
    const r = state.selection.main;
    const sel = state.sliceDoc(r.from, r.to);
    if (sel.includes('\n')) return; // links cannot span paragraphs
    const insert = sel ? `[[|${sel}]]` : '[[]]';
    this.#view.dispatch({
      changes: { from: r.from, to: r.to, insert },
      selection: { anchor: r.from + 2 },
    });
    this.#view.focus();
    startCompletion(this.#view);
  }

  #hoverPreview() {
    const host = this;
    return hoverTooltip((view, pos) => {
      if (!host.#preview) return null;
      const line = view.state.doc.lineAt(pos);
      for (const m of line.text.matchAll(LINK_RE)) {
        const from = line.from + m.index;
        const to = from + m[0].length;
        if (pos < from || pos > to) continue;
        const link = parseLink(m);
        const docId = link && host.#resolve(link.target);
        if (!docId) return null;
        return {
          pos: from,
          end: to,
          create: () => {
            const dom = document.createElement('div');
            dom.className = 'io7-link-preview';
            dom.textContent = '…';
            host.#preview(docId, link.block).then(
              (text) => { dom.textContent = text ?? ''; },
              () => { dom.textContent = ''; });
            return { dom };
          },
        };
      }
      return null;
    });
  }

  // --- methods (host -> widget) ---

  setDocs(docs) {
    this.#titles = new Map();
    this.#titleList = [];
    for (const doc of docs) {
      const key = doc.title.toLowerCase();
      if (this.#titles.has(key)) {
        this.#titles.set(key, ''); // ambiguous
      } else {
        this.#titles.set(key, doc.id);
        this.#titleList.push(doc.title);
      }
    }
    this.#view?.dispatch({ effects: titlesChanged.of(null) });
  }

  setRefTypes(types) {
    this.#refTypes = types;
  }

  setPreviewProvider(fn) {
    this.#preview = fn;
  }

  // #outdent removes one leading tab from every line touched by a
  // selection, mirroring Word's Shift+Tab on an indented paragraph.
  #outdent() {
    const { state } = this.#view;
    const seen = new Set();
    const changes = [];
    for (const range of state.selection.ranges) {
      for (let pos = range.from; pos <= range.to;) {
        const line = state.doc.lineAt(pos);
        if (!seen.has(line.from)) {
          seen.add(line.from);
          if (line.text.startsWith('\t')) {
            changes.push({ from: line.from, to: line.from + 1 });
          }
        }
        pos = line.to + 1;
      }
    }
    if (changes.length) this.#view.dispatch({ changes });
  }

  setContent(content) {
    this.#applying = true;
    this.#view.dispatch({
      changes: { from: 0, to: this.#view.state.doc.length, insert: content },
    });
    this.#applying = false;
  }

  getContent() {
    return this.#view.state.doc.toString();
  }
}

// applyTitle completes a title without duplicating closers the buffer
// already has (the Ctrl-K flow leaves the cursor before |alias]]).
function applyTitle(view, completion, from, to) {
  const after = view.state.sliceDoc(to, to + 1);
  const close = (after === '|' || after === ']') ? '' : ']]';
  const insert = completion.label + close;
  view.dispatch({
    changes: { from, to, insert },
    selection: { anchor: from + insert.length },
  });
}

// applyType inserts "type::" and reopens completion for the title.
function applyType(view, completion, from, to) {
  view.dispatch({
    changes: { from, to, insert: completion.label },
    selection: { anchor: from + completion.label.length },
  });
  startCompletion(view);
}

customElements.define('doc-editor', DocEditor);
