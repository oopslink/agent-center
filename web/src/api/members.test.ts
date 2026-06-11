import type React from 'react';
import { afterEach, describe, expect, it, vi } from 'vitest';
import { renderHook, waitFor, cleanup } from '@testing-library/react';
import { createElement } from 'react';
import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import { http, HttpResponse } from 'msw';
import { server } from '../test/mswServer';
import { qk } from './queryKeys';
import {
  displayNameFallback,
  isResolvedName,
  normalizeIdentityRef,
  useAddAgentMember,
} from './members';

// v2.7 #160: members carry BARE ids ("user-x"); message sender_identity_id and
// conversation participants carry PREFIXED refs ("user:user-x" / "agent:y").
// normalizeIdentityRef strips the prefix so both key to the same value for the
// display-name lookup.
describe('normalizeIdentityRef (#160)', () => {
  it('strips user:/agent: prefixes to the bare id', () => {
    expect(normalizeIdentityRef('user:user-ab12')).toBe('user-ab12');
    expect(normalizeIdentityRef('agent:agent-cd34')).toBe('agent-cd34');
  });
  it('leaves bare ids unchanged', () => {
    expect(normalizeIdentityRef('user-ab12')).toBe('user-ab12');
    expect(normalizeIdentityRef('agent-cd34')).toBe('agent-cd34');
  });
  it('only strips the leading scheme prefix', () => {
    // a single strip — does not recurse / mangle ids that contain colons later.
    expect(normalizeIdentityRef('user:weird:id')).toBe('weird:id');
  });
});

// F1 (v2.8.1 #192): an UNRESOLVED ref (e.g. a force-deleted agent) must never
// surface the raw `agent:agent-xxx` prefixed form. The resolver's fallback +
// the isResolvedName predicate gate that.
describe('displayNameFallback (#192 F1)', () => {
  it('strips the scheme prefix → a clean tail handle (NOT the raw ref)', () => {
    const raw = 'agent:agent-8d1126f6';
    const fallback = displayNameFallback(raw);
    expect(fallback).toBe('agent-8d1126f6');
    // the load-bearing guarantee: never the raw prefixed form.
    expect(fallback).not.toBe(raw);
    expect(fallback.startsWith('agent:')).toBe(false);
    expect(fallback).not.toContain('agent:agent-');
  });
  it('strips a user: prefix too', () => {
    expect(displayNameFallback('user:user-ab12')).toBe('user-ab12');
  });
});

describe('isResolvedName (#192 F1)', () => {
  it('treats a name equal to the RAW ref as UNRESOLVED (deleted)', () => {
    // the resolver returns the raw ref unchanged on a miss (the #192/#215
    // sentinel) — that equality means "unresolved".
    const ref = 'agent:agent-8d1126f6';
    expect(isResolvedName(ref, ref)).toBe(false);
  });
  it('treats a real member display name as RESOLVED', () => {
    expect(isResolvedName('agent:agent-8d1126f6', 'builder-bot')).toBe(true);
  });
  it('an empty ref is unresolved', () => {
    expect(isResolvedName('', '')).toBe(false);
  });
});

// v2.9 #300 (@oopslink finding): after creating an agent through the unified
// POST /api/members/agent (AgentCreateModal → useAddAgentMember), the agents
// list (Agents / Home / MembersAgents / WorkerManagement / BoundAgents /
// Environment — all read useAgents → qk.agents()) must auto-refresh. The
// create therefore has to invalidate qk.agents() — not just the members list —
// otherwise the new agent only appears after a manual reload.
describe('useAddAgentMember (#300 auto-refresh)', () => {
  afterEach(() => cleanup());

  function makeWrapperWithClient(): {
    wrapper: ({ children }: { children: React.ReactNode }) => React.ReactElement;
    qc: QueryClient;
  } {
    const qc = new QueryClient({
      defaultOptions: { queries: { retry: false }, mutations: { retry: false } },
    });
    const wrapper = ({ children }: { children: React.ReactNode }) =>
      createElement(QueryClientProvider, { client: qc }, children);
    return { wrapper, qc };
  }

  it('invalidates qk.agents() (and the members list) on a successful create', async () => {
    server.use(
      http.post('/api/members/agent', () =>
        HttpResponse.json({
          id: 'M1',
          organization_id: 'org-1',
          identity_id: 'agent-new1',
          kind: 'agent',
          role: 'member',
          status: 'joined',
          joined_at: '2026-06-12T00:00:00Z',
          agent_id: 'agent-new1',
        }),
      ),
    );

    const { wrapper, qc } = makeWrapperWithClient();
    const invalidateSpy = vi.spyOn(qc, 'invalidateQueries');

    const { result } = renderHook(() => useAddAgentMember(), { wrapper });
    result.current.mutate({ display_name: 'builder-bot', worker_id: 'w-1' });

    await waitFor(() => expect(result.current.isSuccess).toBe(true));

    const invalidatedKeys = invalidateSpy.mock.calls.map((c) => c[0]?.queryKey);
    // the load-bearing assertion: the agents list query was invalidated so the
    // new agent shows up without a manual reload.
    expect(invalidatedKeys).toContainEqual(qk.agents());
    // and the members list is still invalidated (pre-existing behaviour kept).
    expect(
      invalidatedKeys.some(
        (k) => Array.isArray(k) && k[0] === 'org' && k[k.length - 1] === 'members',
      ),
    ).toBe(true);
  });
});
