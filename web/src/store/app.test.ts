import { beforeEach, describe, expect, it } from 'vitest';
import { useAppStore } from './app';

describe('useAppStore', () => {
  beforeEach(() => {
    useAppStore.setState({
      currentUserId: 'user:hayang',
      sseStatus: 'idle',
      sseLastEventId: null,
    });
  });

  it('seeds with the default loopback user identity', () => {
    expect(useAppStore.getState().currentUserId).toBe('user:hayang');
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
