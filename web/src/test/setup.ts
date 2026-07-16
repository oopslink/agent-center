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

// jsdom does not implement PointerEvent (only MouseEvent), so
// fireEvent.pointerDown/Move/Up/Cancel from @testing-library/dom silently
// fall back to a bare `Event` that drops properties like clientY/pointerId
// (see @testing-library/dom's createEvent: it uses `window[EventType] ||
// window.Event`). Components that implement drag gestures via onPointerDown
// etc. (e.g. BottomSheet's drag-to-close handle) need those properties to be
// testable — polyfill PointerEvent as a thin MouseEvent subclass so the
// standard fireEvent.pointerX helpers work like they would in a real browser.
if (
  typeof MouseEvent !== 'undefined' &&
  typeof (globalThis as { PointerEvent?: unknown }).PointerEvent === 'undefined'
) {
  class PointerEventPolyfill extends MouseEvent {
    pointerId: number;
    constructor(type: string, params: PointerEventInit = {}) {
      super(type, params);
      this.pointerId = params.pointerId ?? 0;
    }
  }
  (globalThis as unknown as { PointerEvent: typeof PointerEventPolyfill }).PointerEvent =
    PointerEventPolyfill;
  (window as unknown as { PointerEvent: typeof PointerEventPolyfill }).PointerEvent =
    PointerEventPolyfill;
}

// Per F4 oversight #4 + #6: MSW Node server intercepts fetch during
// vitest runs. Each test starts with the canonical handler set; tests
// that need to override a path can use server.use(...) which resets
// after each test.
beforeAll(() => server.listen({ onUnhandledRequest: 'error' }));
afterEach(() => server.resetHandlers());
afterAll(() => server.close());
