// <doc-editor> — a document's content view: one CodeMirror 6 instance.
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
        EditorView.updateListener.of((update) => {
          if (update.docChanged && !this.#applying) {
            this.dispatchEvent(new CustomEvent('doc-change', { bubbles: true }));
          }
        }),
      ],
      parent: this,
    });
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
