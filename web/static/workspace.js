// Workspace window manager: a registry of spawnable widgets and the
// floating windows that host them. app.js registers widgets at boot and
// calls spawn(); the nav-menu dropdown lists whatever is registered.
// See docs/widgets.md "Spawning windows".

import '/components/widget-window.js';

const registry = new Map(); // name -> {title, tag, width, height, singleton, editableTitle}
let cascade = 0;

// register declares a spawnable widget. tag is the custom element to
// create inside the window. singleton widgets get at most one window —
// spawning again focuses the existing one. editableTitle windows let the
// user edit the title bar text (they emit 'title-change').
export function register(name, { title, tag, width = 520, height = 360, singleton = false, editableTitle = false }) {
  registry.set(name, { title, tag, width, height, singleton, editableTitle });
}

// widgets lists registered widgets for menus: [{name, title}].
export function widgets() {
  return [...registry].map(([name, { title }]) => ({ name, title }));
}

// spawn opens a window hosting the widget `name` and returns
// {win, el, created}. New windows cascade so they don't stack exactly.
// Every spawned window is closable; closing removes it from the DOM
// (widgets needing teardown beyond DOM removal handle 'window-close'
// themselves — it bubbles).
export function spawn(name) {
  const spec = registry.get(name);
  if (!spec) throw new Error(`unknown widget: ${name}`);
  const workspace = document.getElementById('workspace');

  if (spec.singleton) {
    const win = workspace.querySelector(`widget-window[data-widget="${name}"]`);
    if (win) {
      win.bringToFront();
      return { win, el: win.querySelector(spec.tag), created: false };
    }
  }

  const el = document.createElement(spec.tag);
  const win = document.createElement('widget-window');
  win.dataset.widget = name;
  win.setAttribute('window-title', spec.title);
  win.setAttribute('closable', '');
  if (spec.editableTitle) win.setAttribute('editable-title', '');
  const step = cascade++ % 8;
  win.setAttribute('x', String(32 + step * 32));
  win.setAttribute('y', String(24 + step * 26));
  win.setAttribute('width', String(spec.width));
  win.setAttribute('height', String(spec.height));
  win.append(el);
  win.addEventListener('window-close', () => win.remove());
  workspace.append(win);
  return { win, el, created: true };
}
