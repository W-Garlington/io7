// <doc-list> — the document list widget.
//
// Widget contract (all widgets follow this shape):
//   downward: the host calls methods/properties on the element.
//   upward:   the element dispatches bubbling CustomEvents; it never
//             talks to the API or other widgets directly.
//   connectedCallback must be idempotent (windows re-parent children).
//
// Methods: setDocs(docs)
// Events:  'doc-create', 'doc-select' {id}, 'doc-delete' {id}
//
// Renders into light DOM on purpose — one global stylesheet, no per-widget
// shadow roots to theme separately.

class DocList extends HTMLElement {
  #docs = [];

  connectedCallback() {
    this.#render();
  }

  setDocs(docs) {
    this.#docs = docs;
    this.#render();
  }

  #emit(type, detail) {
    this.dispatchEvent(new CustomEvent(type, { detail, bubbles: true }));
  }

  #render() {
    this.replaceChildren();

    const newBtn = document.createElement('button');
    newBtn.className = 'doc-new';
    newBtn.textContent = '+ New document';
    newBtn.onclick = () => this.#emit('doc-create');
    this.append(newBtn);

    const list = document.createElement('ul');
    for (const doc of this.#docs) {
      const item = document.createElement('li');

      const title = document.createElement('span');
      title.className = 'doc-title';
      title.textContent = doc.title || 'Untitled';
      item.onclick = () => this.#emit('doc-select', { id: doc.id });

      const del = document.createElement('button');
      del.className = 'doc-delete';
      del.textContent = '%'; // placeholder delete glyph for now
      del.title = 'Delete';
      del.onclick = (e) => {
        e.stopPropagation();
        if (confirm(`Delete "${doc.title || 'Untitled'}"?`)) {
          this.#emit('doc-delete', { id: doc.id });
        }
      };

      item.append(title, del);
      list.append(item);
    }
    this.append(list);
  }
}

customElements.define('doc-list', DocList);
