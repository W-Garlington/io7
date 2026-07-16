// <nav-menu> — topbar dropdown listing spawnable widgets.
//
// Methods: setItems(items) with items = [{name, title}]
// Events:  'widget-spawn' {name}
//
// See doc-list.js for the widget contract.

class NavMenu extends HTMLElement {
  #details = null;
  #list = null;

  connectedCallback() {
    if (this.#details) return;

    const summary = document.createElement('summary');
    summary.textContent = 'Widgets';
    this.#list = document.createElement('ul');
    this.#details = document.createElement('details');
    this.#details.append(summary, this.#list);
    this.append(this.#details);

    // Close the dropdown when clicking anywhere outside it.
    document.addEventListener('pointerdown', (ev) => {
      if (!this.contains(ev.target)) this.#details.open = false;
    });
  }

  setItems(items) {
    this.#list.replaceChildren(...items.map(({ name, title }) => {
      const item = document.createElement('li');
      item.textContent = title;
      item.onclick = () => {
        this.#details.open = false;
        this.dispatchEvent(new CustomEvent('widget-spawn', { detail: { name }, bubbles: true }));
      };
      return item;
    }));
  }
}

customElements.define('nav-menu', NavMenu);
