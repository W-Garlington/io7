// <doc-editor> — edits one Document: a title field plus a CodeMirror 6
// content view. Each instance is bound to one document by the host.
//
// Methods: setDoc({title, content}), getTitle(), getContent(), focusTitle()
// Events:  'doc-change' — fired on user edits (title or content) only,
//          never on setDoc().
//
// See doc-list.js for the widget contract.

import { EditorView, basicSetup } from '/vendor/codemirror.js';

class DocEditor extends HTMLElement {
  #view = null;
  #titleInput = null;
  #applying = false; // true while setDoc() replaces state programmatically

  connectedCallback() {
    // Idempotent: re-parenting (e.g. widget-window moving us into its
    // body) re-fires this callback; the view must only be built once.
    if (this.#view) return;

    this.#titleInput = document.createElement('input');
    this.#titleInput.className = 'doc-editor-title';
    this.#titleInput.placeholder = 'Untitled';
    this.#titleInput.addEventListener('input', () => this.#changed());
    this.append(this.#titleInput);

    this.#view = new EditorView({
      extensions: [
        basicSetup,
        EditorView.lineWrapping,
        EditorView.updateListener.of((update) => {
          if (update.docChanged) this.#changed();
        }),
      ],
      parent: this,
    });
  }

  #changed() {
    if (!this.#applying) {
      this.dispatchEvent(new CustomEvent('doc-change', { bubbles: true }));
    }
  }

  setDoc({ title, content }) {
    this.#applying = true;
    this.#titleInput.value = title;
    this.#view.dispatch({
      changes: { from: 0, to: this.#view.state.doc.length, insert: content },
    });
    this.#applying = false;
  }

  getTitle() {
    return this.#titleInput.value;
  }

  getContent() {
    return this.#view.state.doc.toString();
  }

  focusTitle() {
    this.#titleInput.focus();
    this.#titleInput.select();
  }
}

customElements.define('doc-editor', DocEditor);
