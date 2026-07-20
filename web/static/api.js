// Thin wrappers over the backend REST API. Every function returns parsed
// JSON or throws an Error with the server's message.

async function request(method, path, body) {
  const opts = { method };
  if (body !== undefined) {
    opts.headers = { 'Content-Type': 'application/json' };
    opts.body = JSON.stringify(body);
  }
  const res = await fetch(path, opts);
  if (!res.ok) {
    throw new Error(`${method} ${path}: ${res.status} ${await res.text()}`);
  }
  return res.status === 204 ? null : res.json();
}

export const listDocs = () => request('GET', '/api/docs');
export const getDoc = (id) => request('GET', `/api/docs/${id}`);
export const createDoc = (title, content) => request('POST', '/api/docs', { title, content });
export const updateDoc = (id, title, content) => request('PUT', `/api/docs/${id}`, { title, content });
export const deleteDoc = (id) => request('DELETE', `/api/docs/${id}`);
export const refTypes = () => request('GET', '/api/reftypes');
