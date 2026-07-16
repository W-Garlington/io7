// App orchestrator: owns "which document is open", wires widget events to
// the REST API, and applies live events from the WebSocket. Widgets never
// call the API themselves — they emit events handled here.
//
// Widgets spawn and close at runtime, so listeners live on `document`
// (widget events bubble) and elements are looked up per use, never held
// in long-lived references.

import * as api from '/api.js';
import * as live from '/live.js';
import * as workspace from '/workspace.js';
import '/components/doc-list.js';
import '/components/doc-editor.js';
import '/components/nav-menu.js';

workspace.register('editor', { title: 'Editor', tag: 'doc-editor', width: 760, height: 520, singleton: true });
workspace.register('documents', { title: 'Documents', tag: 'doc-list', width: 260, height: 420 });

const titleInput = document.getElementById('doc-title');
const saveStatus = document.getElementById('save-status');

const editorEl = () => document.querySelector('doc-editor');
const editorWin = () => document.querySelector('widget-window[data-widget="editor"]');

let current = null; // the open document as last saved, or null
let saveTimer = null;
const SAVE_DEBOUNCE_MS = 600;

function setStatus(text) {
  saveStatus.textContent = text;
}

// editedContent is the content as currently edited — falling back to the
// last saved content when the editor window is closed.
function editedContent() {
  return editorEl()?.getContent() ?? current?.content;
}

function isDirty() {
  return current !== null &&
    (editedContent() !== current.content || titleInput.value !== current.title);
}

async function refreshList() {
  const docs = await api.listDocs();
  for (const list of document.querySelectorAll('doc-list')) {
    list.setDocs(docs);
    list.setActive(current?.id ?? null);
  }
}

function setActive(id) {
  for (const list of document.querySelectorAll('doc-list')) list.setActive(id);
}

// ensureEditor spawns (or focuses) the editor window and returns the
// doc-editor element inside it.
function ensureEditor() {
  return workspace.spawn('editor').el;
}

async function openDoc(id) {
  current = await api.getDoc(id);
  titleInput.value = current.title;
  titleInput.disabled = false;
  ensureEditor().open(current.content);
  editorWin().setTitle(current.title || 'Untitled');
  setActive(id);
  setStatus('saved');
}

function closeDoc() {
  current = null;
  titleInput.value = '';
  titleInput.disabled = true;
  editorEl()?.close();
  editorWin()?.setTitle('Editor');
  setActive(null);
  setStatus('');
}

async function save() {
  if (!current || !isDirty()) return;
  setStatus('saving…');
  try {
    current = await api.updateDoc(current.id, titleInput.value, editedContent());
    editorWin()?.setTitle(current.title || 'Untitled');
    setStatus(isDirty() ? 'unsaved' : 'saved');
    refreshList();
  } catch (err) {
    console.error(err);
    setStatus('save failed');
  }
}

function scheduleSave() {
  setStatus('unsaved');
  clearTimeout(saveTimer);
  saveTimer = setTimeout(save, SAVE_DEBOUNCE_MS);
}

// --- widget events -> API (delegated: any widget instance, any window) ---

document.addEventListener('doc-create', async () => {
  const doc = await api.createDoc('Untitled', '');
  await openDoc(doc.id);
  refreshList();
  titleInput.select();
});

document.addEventListener('doc-select', (ev) => {
  if (current?.id !== ev.detail.id) {
    openDoc(ev.detail.id);
  } else {
    ensureEditor(); // already open — just surface the editor window
  }
});

document.addEventListener('doc-delete', async (ev) => {
  await api.deleteDoc(ev.detail.id);
  if (current?.id === ev.detail.id) closeDoc();
  refreshList();
});

document.addEventListener('doc-change', scheduleSave);
titleInput.addEventListener('input', scheduleSave);

document.addEventListener('widget-spawn', (ev) => {
  const { el, created } = workspace.spawn(ev.detail.name);
  if (!created) return;
  // Hydrate freshly spawned widgets with current state.
  if (ev.detail.name === 'documents') refreshList();
  if (ev.detail.name === 'editor' && current) {
    el.open(current.content);
    editorWin().setTitle(current.title || 'Untitled');
  }
});

// Flush pending edits before the editor window is removed. Capture phase
// runs before workspace.js's removal listener on the window itself.
document.addEventListener('window-close', (ev) => {
  if (ev.target === editorWin()) {
    clearTimeout(saveTimer);
    save(); // reads the editor synchronously, before removal
  }
}, true);

document.addEventListener('keydown', (ev) => {
  if ((ev.ctrlKey || ev.metaKey) && ev.key === 's') {
    ev.preventDefault();
    clearTimeout(saveTimer);
    save();
  }
});

// --- live events (other tabs / future backend watchers) -> UI ---

live.on('doc.created', refreshList);
live.on('doc.deleted', (ev) => {
  if (current?.id === ev.id) closeDoc();
  refreshList();
});
live.on('doc.updated', (ev) => {
  refreshList();
  // Apply remote content to the open doc only if there are no local
  // unsaved edits — never clobber in-progress typing.
  if (current?.id === ev.doc.id && !isDirty() && ev.doc.updatedAt !== current.updatedAt) {
    current = ev.doc;
    titleInput.value = ev.doc.title;
    editorWin()?.setTitle(ev.doc.title || 'Untitled');
    editorEl()?.open(ev.doc.content);
  }
});

// --- boot ---

document.querySelector('nav-menu').setItems(workspace.widgets());
live.connect();
workspace.spawn('editor');
closeDoc();
refreshList();
