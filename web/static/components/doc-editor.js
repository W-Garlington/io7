// <doc-editor> — the text editing widget, wrapping a CodeMirror 6 view.
//
// Methods: open(content), close(), getContent()
// Events:  'doc-change' — fired on user edits only, never on open().
//
// See doc-list.js for the widget contract.

import { EditorView, basicSetup } from '/vendor/codemirror.js';

class DocEditor extends HTMLElement {
  #view = null;
  #applying = false; // true while open() replaces content programmatically

  connectedCallback() {
    // Idempotent: re-parenting (e.g. widget-window moving us into its
    // body) re-fires this callback; the view must only be built once.
    if (this.#view) return;

    this.#empty = document.createElement('div');
    this.#empty.className = 'editor-empty';
    this.#empty.textContent = 'Select or create a document';
    this.append(this.#empty);

    this.#view = new EditorView({
      extensions: [
        basicSetup,
        EditorView.lineWrapping,
        EditorView.updateListener.of((update) => {
          if (update.docChanged && !this.#applying) {
            this.dispatchEvent(new CustomEvent('doc-change', { bubbles: true }));
          }
        }),
      ],
      parent: this,
    });
    this.close();
  }

  #empty = null;

  open(content) {
    this.#applying = true;
    this.#view.dispatch({
      changes: { from: 0, to: this.#view.state.doc.length, insert: content },
    });
    this.#applying = false;
    this.#empty.hidden = true;
    this.#view.dom.hidden = false;
    this.#view.focus();
  }

  close() {
    this.#applying = true;
    this.#view.dispatch({
      changes: { from: 0, to: this.#view.state.doc.length, insert: '' },
    });
    this.#applying = false;
    this.#view.dom.hidden = true;
    this.#empty.hidden = false;
  }

  getContent() {
    return this.#view.state.doc.toString();
  }
}

customElements.define('doc-editor', DocEditor);
