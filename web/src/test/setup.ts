import '@testing-library/jest-dom/vitest';
import { afterAll, afterEach, beforeAll } from 'vitest';
import { server } from './mswServer';

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
