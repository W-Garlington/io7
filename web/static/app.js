// App orchestrator: owns "which document is open", wires widget events to
// the REST API, and applies live events from the WebSocket. Widgets never
// call the API themselves — they emit events handled here.

import * as api from '/api.js';
import * as live from '/live.js';
import '/components/doc-list.js';
import '/components/doc-editor.js';
import '/components/widget-window.js';

const docList = document.querySelector('doc-list');
const editor = document.querySelector('doc-editor');
const editorWindow = document.getElementById('editor-window');
const titleInput = document.getElementById('doc-title');
const saveStatus = document.getElementById('save-status');

let current = null; // the open document as last saved, or null
let saveTimer = null;
const SAVE_DEBOUNCE_MS = 600;

function setStatus(text) {
  saveStatus.textContent = text;
}

function isDirty() {
  return current !== null &&
    (editor.getContent() !== current.content || titleInput.value !== current.title);
}

async function refreshList() {
  docList.setDocs(await api.listDocs());
  docList.setActive(current?.id ?? null);
}

async function openDoc(id) {
  current = await api.getDoc(id);
  titleInput.value = current.title;
  titleInput.disabled = false;
  editor.open(current.content);
  editorWindow.setTitle(current.title || 'Untitled');
  docList.setActive(id);
  setStatus('saved');
}

function closeDoc() {
  current = null;
  titleInput.value = '';
  titleInput.disabled = true;
  editor.close();
  editorWindow.setTitle('Editor');
  docList.setActive(null);
  setStatus('');
}

async function save() {
  if (!current || !isDirty()) return;
  setStatus('saving…');
  try {
    current = await api.updateDoc(current.id, titleInput.value, editor.getContent());
    editorWindow.setTitle(current.title || 'Untitled');
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

// --- widget events -> API ---

docList.addEventListener('doc-create', async () => {
  const doc = await api.createDoc('Untitled', '');
  await openDoc(doc.id);
  refreshList();
  titleInput.select();
});

docList.addEventListener('doc-select', (ev) => {
  if (current?.id !== ev.detail.id) openDoc(ev.detail.id);
});

docList.addEventListener('doc-delete', async (ev) => {
  await api.deleteDoc(ev.detail.id);
  if (current?.id === ev.detail.id) closeDoc();
  refreshList();
});

editor.addEventListener('doc-change', scheduleSave);
titleInput.addEventListener('input', scheduleSave);

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
    editorWindow.setTitle(ev.doc.title || 'Untitled');
    editor.open(ev.doc.content);
  }
});

// --- boot ---

live.connect();
refreshList();
closeDoc();
