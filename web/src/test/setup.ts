import '@testing-library/jest-dom/vitest';
import { afterAll, afterEach, beforeAll } from 'vitest';
import { server } from './mswServer';
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
