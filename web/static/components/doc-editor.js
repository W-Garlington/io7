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
// Methods: setContent(content), getContent()
// Events:  'doc-change' — fired on user edits only, never on setContent().
//
// See doc-list.js for the widget contract.

import { EditorView, basicSetup } from '/vendor/codemirror.js';

class DocEditor extends HTMLElement {
  #view = null;
  #applying = false; // true while setContent() replaces content

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
      ],
      parent: this,
    });

    // Tab/Shift+Tab indent handling. basicSetup deliberately leaves Tab
    // unbound (and the vendored bundle doesn't export keymap helpers),
    // so a plain keydown listener does the job: CM ignores Tab, the
    // event bubbles here.
    this.addEventListener('keydown', (ev) => {
      if (ev.key !== 'Tab') return;
      ev.preventDefault();
      if (ev.shiftKey) {
        this.#outdent();
      } else {
        this.#view.dispatch(this.#view.state.replaceSelection('\t'));
      }
    });
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

customElements.define('doc-editor', DocEditor);
