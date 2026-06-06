import { afterEach, describe, expect, it } from 'vitest';
import { renderHook, waitFor, cleanup } from '@testing-library/react';
import { http, HttpResponse } from 'msw';
import { makeWrapper } from '../test/renderWith';
import { server } from '../test/mswServer';
import { useArchiveAgent } from './agents';

// v2.8 #270 / #272: soft-archive is the only user-facing delete path. The hook
// POSTs the #272 endpoint and returns the refreshed (archived) agent.
describe('useArchiveAgent (#270/#272)', () => {
  afterEach(() => cleanup());

  it('POSTs /agents/:id/archive and returns the archived agent', async () => {
    let hitPath = '';
    server.use(
      http.post('/api/agents/:id/archive', ({ params }) => {
        hitPath = `archive:${params.id}`;
        return HttpResponse.json({ id: 'A1', lifecycle: 'archived' });
      }),
    );
    const { result } = renderHook(() => useArchiveAgent('A1'), { wrapper: makeWrapper() });
    result.current.mutate();
    await waitFor(() => expect(result.current.isSuccess).toBe(true));
    expect(hitPath).toBe('archive:A1');
    expect(result.current.data?.lifecycle).toBe('archived');
  });
});
