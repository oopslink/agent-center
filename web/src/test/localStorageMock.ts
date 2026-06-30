// installMemoryLocalStorage — the Node test runner exposes an experimental
// `localStorage` global with NO methods (hence the `--localstorage-file` warning),
// so the app's persistence effects (guarded by typeof checks) become no-ops in
// tests. Suites that assert on persisted state install an in-memory localStorage.
// Call inside beforeAll. Mirrors the inline polyfill in AppLayout.sidebar.test.
export function installMemoryLocalStorage(): void {
  const store: Record<string, string> = {};
  Object.defineProperty(globalThis, 'localStorage', {
    value: {
      getItem: (k: string) => (k in store ? store[k] : null),
      setItem: (k: string, v: string) => {
        store[k] = String(v);
      },
      removeItem: (k: string) => {
        delete store[k];
      },
      clear: () => {
        for (const k of Object.keys(store)) delete store[k];
      },
    },
    configurable: true,
  });
}
