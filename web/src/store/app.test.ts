import { beforeEach, describe, expect, it } from 'vitest';
import { useAppStore } from './app';

describe('useAppStore', () => {
  beforeEach(() => {
    useAppStore.setState({
      currentUserId: '',
      sseStatus: 'idle',
      sseLastEventId: null,
    });
  });

  it('seeds currentUserId EMPTY (no hardcoded placeholder identity)', () => {
    // Assert the store INITIALIZER (not the beforeEach reset): it must not
    // carry a hardcoded user (e.g. the removed 'user:hayang'). AppLayout
    // seeds it from /api/auth/me at runtime; until then it is ''.
    expect(useAppStore.getInitialState().currentUserId).toBe('');
  });

  it('setCurrentUserId replaces the identity', () => {
    useAppStore.getState().setCurrentUserId('user:demo');
    expect(useAppStore.getState().currentUserId).toBe('user:demo');
  });

  it('SSE status + last event id round-trip', () => {
    useAppStore.getState().setSSEStatus('open');
    useAppStore.getState().setSSELastEventId('ev-42');
    expect(useAppStore.getState().sseStatus).toBe('open');
    expect(useAppStore.getState().sseLastEventId).toBe('ev-42');
  });

});
