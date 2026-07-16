// Single WebSocket connection for all live events (see IOX_PLAN.md "Live
// updates"). Events arrive as {type, ...} objects; handlers subscribe by
// type. Reconnects automatically with backoff.

const handlers = new Map(); // type -> Set of callbacks

export function on(type, callback) {
  if (!handlers.has(type)) handlers.set(type, new Set());
  handlers.get(type).add(callback);
}

export function connect() {
  let delay = 500;
  const open = () => {
    const ws = new WebSocket(`ws://${location.host}/ws`);
    ws.onmessage = (msg) => {
      const ev = JSON.parse(msg.data);
      for (const cb of handlers.get(ev.type) ?? []) cb(ev);
    };
    ws.onopen = () => { delay = 500; };
    ws.onclose = () => {
      setTimeout(open, delay);
      delay = Math.min(delay * 2, 10000);
    };
  };
  open();
}
