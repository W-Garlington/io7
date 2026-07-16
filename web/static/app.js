// App orchestrator: binds documents to editor windows, wires widget
// events to the REST API, and applies live events from the WebSocket.
// Widgets never call the API themselves — they emit events handled here.
//
// Model: one window per open document. Selecting a doc opens (or
// focuses) its window; creating a doc opens a new window; each editor
// autosaves independently. Listeners live on `document` (widget events
// bubble) since windows spawn and close at runtime.

import * as api from '/api.js';
import * as live from '/live.js';
import * as workspace from '/workspace.js';
import '/components/doc-list.js';
import '/components/doc-editor.js';
import '/components/nav-menu.js';

workspace.register('editor', { title: 'New document', tag: 'doc-editor', width: 640, height: 480, editableTitle: true });
workspace.register('documents', { title: 'Documents', tag: 'doc-list', width: 260, height: 420 });

// Per-editor state: doc-editor element -> {doc: last saved doc, timer}.
const states = new WeakMap();
const SAVE_DEBOUNCE_MS = 600;

const winFor = (docId) => document.querySelector(`widget-window[data-doc-id="${docId}"]`);
const winOf = (el) => el.closest('widget-window');

function bindDoc(win, el, doc) {
  win.dataset.docId = doc.id;
  states.set(el, { doc, timer: null });
  el.setContent(doc.content);
  win.setTitle(doc.title);
  win.setDirty(false);
}

// The doc's title is edited in the window's title bar, its content in
// the doc-editor; dirty compares both against the last saved doc.
function isDirty(el) {
  const st = states.get(el);
  const win = winOf(el);
  return Boolean(st && win &&
    (win.getTitle() !== st.doc.title || el.getContent() !== st.doc.content));
}

function updateDirty(el) {
  winOf(el)?.setDirty(isDirty(el));
}

async function refreshLists() {
  const docs = await api.listDocs();
  for (const list of document.querySelectorAll('doc-list')) list.setDocs(docs);
}

async function openDoc(id) {
  const existing = winFor(id);
  if (existing) {
    existing.bringToFront();
    return;
  }
  const doc = await api.getDoc(id);
  const { win, el } = workspace.spawn('editor');
  bindDoc(win, el, doc);
}

async function createDoc() {
  const doc = await api.createDoc('Untitled', '');
  const { win, el } = workspace.spawn('editor');
  bindDoc(win, el, doc);
  win.focusTitle();
  refreshLists();
}

async function save(el) {
  const st = states.get(el);
  if (!st || !isDirty(el)) return;
  // Read both synchronously up front — the window may be removed (close
  // flush) before the request resolves.
  const title = winOf(el).getTitle();
  const content = el.getContent();
  try {
    st.doc = await api.updateDoc(st.doc.id, title, content);
    updateDirty(el);
    refreshLists();
  } catch (err) {
    console.error(err); // keeps the • so unsaved state stays visible
  }
}

function scheduleSave(el) {
  const st = states.get(el);
  if (!st) return;
  updateDirty(el);
  clearTimeout(st.timer);
  st.timer = setTimeout(() => save(el), SAVE_DEBOUNCE_MS);
}

// --- widget events -> API (delegated: any widget instance, any window) ---

document.addEventListener('doc-create', createDoc);

document.addEventListener('doc-select', (ev) => openDoc(ev.detail.id));

document.addEventListener('doc-delete', async (ev) => {
  await api.deleteDoc(ev.detail.id);
  winFor(ev.detail.id)?.remove(); // doc is gone — no save flush
  refreshLists();
});

document.addEventListener('doc-change', (ev) => scheduleSave(ev.target));

// Title edits in an editor window's title bar save like content edits.
document.addEventListener('title-change', (ev) => {
  const el = ev.target.querySelector('doc-editor');
  if (el) scheduleSave(el);
});

document.addEventListener('widget-spawn', (ev) => {
  if (ev.detail.name === 'editor') {
    createDoc(); // an editor window always edits a document — a fresh one
    return;
  }
  const { el, created } = workspace.spawn(ev.detail.name);
  if (created && ev.detail.name === 'documents') refreshLists();
});

// Flush pending edits before an editor window is removed. Capture phase
// runs before workspace.js's removal listener on the window itself.
document.addEventListener('window-close', (ev) => {
  const el = ev.target.querySelector('doc-editor');
  if (el && states.get(el)) {
    clearTimeout(states.get(el).timer);
    save(el); // reads the editor synchronously, before removal
  }
}, true);

document.addEventListener('keydown', (ev) => {
  if ((ev.ctrlKey || ev.metaKey) && ev.key === 's') {
    ev.preventDefault();
    for (const el of document.querySelectorAll('doc-editor')) {
      const st = states.get(el);
      if (st) clearTimeout(st.timer);
      save(el);
    }
  }
});

// --- live events (other tabs / future backend watchers) -> UI ---

live.on('doc.created', refreshLists);

live.on('doc.deleted', (ev) => {
  winFor(ev.id)?.remove();
  refreshLists();
});

live.on('doc.updated', (ev) => {
  refreshLists();
  const win = winFor(ev.doc.id);
  const el = win?.querySelector('doc-editor');
  const st = el && states.get(el);
  // Apply remote content only if this window has no unsaved local edits —
  // never clobber in-progress typing. Our own saves arrive here too, but
  // updatedAt already matches, so they're skipped.
  if (st && ev.doc.updatedAt !== st.doc.updatedAt && !isDirty(el)) {
    st.doc = ev.doc;
    el.setContent(ev.doc.content);
    win.setTitle(ev.doc.title);
    win.setDirty(false);
  }
});

// --- boot ---

document.querySelector('nav-menu').setItems(workspace.widgets());
live.connect();
workspace.spawn('documents');
refreshLists();
