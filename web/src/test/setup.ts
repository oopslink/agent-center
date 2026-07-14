import '@testing-library/jest-dom/vitest';
import { afterAll, afterEach, beforeAll } from 'vitest';
import { configure } from '@testing-library/react';
import { server } from './mswServer';

// Raise the DOM Testing Library async timeout from its 1000ms default. The
// whole-app render suite (App.test.tsx) mounts lazily-imported pages behind
// Suspense; under a fully-parallel `vitest run` (180+ files saturating the
// worker pool) a lazy page can take >1s to resolve + render, so a `findBy*`
// with the 1s default flakily reports "unable to find element" even though the
// route tree is correct (it passes deterministically in isolation). 5s is well
// under the per-test 20000ms budget the heavy tests already self-select, so a
// genuinely-missing element still fails fast enough — this only absorbs
// load-induced render latency, it does not mask real failures.
configure({ asyncUtilTimeout: 5000 });
// Initialise i18next for the whole test run so components using
// useTranslation() render real copy (default lang → English in jsdom, where
// navigator.language is en-US and no `ac.lang` is stored) rather than raw keys.
import '../i18n';

// jsdom does not implement the object-URL APIs the composer uses for image
// attachment previews (URL.createObjectURL / revokeObjectURL). Provide inert
// stubs so components that stage image files render in tests.
if (typeof URL.createObjectURL !== 'function') {
  URL.createObjectURL = () => 'blob:mock';
}
if (typeof URL.revokeObjectURL !== 'function') {
  URL.revokeObjectURL = () => {};
}

// Per F4 oversight #4 + #6: MSW Node server intercepts fetch during
// vitest runs. Each test starts with the canonical handler set; tests
// that need to override a path can use server.use(...) which resets
// after each test.
beforeAll(() => server.listen({ onUnhandledRequest: 'error' }));
afterEach(() => server.resetHandlers());
afterAll(() => server.close());
