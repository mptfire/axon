/* eslint-env node, es2021 */
// Minimal window stub so the pure-logic modules import cleanly under the node environment,
// without pulling in jsdom. utils.js -> config.js does `const { config } = window;` at import
// time, and config.js falls back to window.location.origin when base_url is empty.
globalThis.window = {
  location: { origin: "https://ntfy.sh" },
  config: { base_url: "", disallowed_topics: ["app", "account", "settings"] },
  atob: globalThis.atob, // urlB64ToUint8Array uses window.atob; Node provides global atob
};

// utils.js -> Prefs.js -> db.js -> Session.username() reads localStorage at module load.
// Node has no localStorage; an in-memory stand-in is enough for the pure-logic tests.
const store = new Map();
globalThis.localStorage = {
  getItem: (key) => (store.has(key) ? store.get(key) : null),
  setItem: (key, value) => store.set(key, String(value)),
  removeItem: (key) => store.delete(key),
  clear: () => store.clear(),
};
