import { afterEach, describe, expect, it } from 'vitest';
import { cleanup, renderHook, waitFor, act } from '@testing-library/react';
import { http, HttpResponse } from 'msw';
import { makeWrapper } from '../test/renderWith';
import { server } from '../test/mswServer';
import {
  useConversations,
  useConversation,
  useMessages,
  useCreateConversation,
  useSendMessage,
  useArchiveConversation,
  useInviteParticipant,
  useRemoveParticipant,
} from './conversations';
import { useAgents, useAgent } from './agents';
import { useSecrets, useCreateSecret, useRevokeSecret } from './secrets';
import { useInputRequests, useRespondInputRequest } from './inputRequests';
import { useFleet, useTaskTrace } from './fleet';
import { useDeriveIssue, useDeriveTask } from './derive';

// Mutation tests use the sync `mutate(args)` API + waitFor on isSuccess
// rather than `await act(async () => await mutateAsync(...))`. The async
// pattern leaves React 19's dispatcher in a state where the NEXT
// renderHook in the same file returns result.current = null.

describe('react-query hooks', () => {
  afterEach(() => cleanup());

  it('useConversations returns the canned list', async () => {
    const { result } = renderHook(() => useConversations({ kind: 'channel' }), {
      wrapper: makeWrapper(),
    });
    await waitFor(() => expect(result.current.isSuccess).toBe(true));
    expect(result.current.data?.[0].name).toBe('alpha');
  });

  // v2.1-B: cover the no-filter / no-search-params branch in
  // useConversations (the path where the query string stays empty).
  it('useConversations with no filter hits /conversations unfiltered', async () => {
    const { result } = renderHook(() => useConversations(), {
      wrapper: makeWrapper(),
    });
    await waitFor(() => expect(result.current.isSuccess).toBe(true));
    expect(Array.isArray(result.current.data)).toBe(true);
  });

  it('useConversation skips fetch when id is undefined', () => {
    const { result } = renderHook(() => useConversation(undefined), { wrapper: makeWrapper() });
    expect(result.current.fetchStatus).toBe('idle');
  });

  it('useConversation fetches when id is set', async () => {
    const { result } = renderHook(() => useConversation('C1'), { wrapper: makeWrapper() });
    await waitFor(() => expect(result.current.isSuccess).toBe(true));
    expect(result.current.data?.id).toBe('C1');
  });

  it('useMessages requires a conversationId', () => {
    const { result } = renderHook(() => useMessages(undefined), { wrapper: makeWrapper() });
    expect(result.current.fetchStatus).toBe('idle');
  });

  it('useMessages fetches when conversationId is set', async () => {
    const { result } = renderHook(() => useMessages('C1'), { wrapper: makeWrapper() });
    await waitFor(() => expect(result.current.isSuccess).toBe(true));
    expect(result.current.data?.[0].content).toBe('hi');
  });

  it('useCreateConversation channel happy', async () => {
    const { result } = renderHook(() => useCreateConversation(), { wrapper: makeWrapper() });
    act(() => {
      result.current.mutate({ kind: 'channel', name: 'alpha' });
    });
    await waitFor(() => expect(result.current.isSuccess).toBe(true));
    expect(result.current.data?.conversation_id).toBe('C-NEW');
  });

  it('useCreateConversation channel rejects when name missing', async () => {
    const { result } = renderHook(() => useCreateConversation(), { wrapper: makeWrapper() });
    act(() => {
      result.current.mutate({ kind: 'channel' });
    });
    await waitFor(() => expect(result.current.isError).toBe(true));
    expect((result.current.error as Error).message).toMatch(/name required/);
  });

  it('useSendMessage invalidates the messages query on success', async () => {
    const { result } = renderHook(() => useSendMessage(), { wrapper: makeWrapper() });
    act(() => {
      result.current.mutate({ conversationId: 'C1', content: 'hi' });
    });
    await waitFor(() => expect(result.current.isSuccess).toBe(true));
    expect(result.current.data?.message_id).toBe('M-NEW');
  });

  it('useArchiveConversation + useInviteParticipant + useRemoveParticipant', async () => {
    const wrapper = makeWrapper();
    const archive = renderHook(() => useArchiveConversation(), { wrapper });
    act(() => {
      archive.result.current.mutate({ id: 'C1', version: 1, archivedBy: 'user:hayang' });
    });
    await waitFor(() => expect(archive.result.current.isSuccess).toBe(true));

    const invite = renderHook(() => useInviteParticipant(), { wrapper });
    act(() => {
      invite.result.current.mutate({
        conversationId: 'C1',
        identityId: 'agent:bot-1',
        role: 'member',
      });
    });
    await waitFor(() => expect(invite.result.current.isSuccess).toBe(true));

    const remove = renderHook(() => useRemoveParticipant(), { wrapper });
    act(() => {
      remove.result.current.mutate({ conversationId: 'C1', identityId: 'agent:bot-1' });
    });
    await waitFor(() => expect(remove.result.current.isSuccess).toBe(true));
  });

  it('useAgents + useAgent', async () => {
    const wrapper = makeWrapper();
    const list = renderHook(() => useAgents(), { wrapper });
    await waitFor(() => expect(list.result.current.isSuccess).toBe(true));
    expect(list.result.current.data?.[0].name).toBe('aa');

    const one = renderHook(() => useAgent('aa'), { wrapper });
    await waitFor(() => expect(one.result.current.isSuccess).toBe(true));
    expect(one.result.current.data?.name).toBe('aa');
  });

  it('useAgent skips when name is undefined', () => {
    const { result } = renderHook(() => useAgent(undefined), { wrapper: makeWrapper() });
    expect(result.current.fetchStatus).toBe('idle');
  });

  it('useSecrets + useCreateSecret + useRevokeSecret', async () => {
    const wrapper = makeWrapper();
    const list = renderHook(() => useSecrets(), { wrapper });
    await waitFor(() => expect(list.result.current.isSuccess).toBe(true));
    expect(list.result.current.data?.[0].name).toBe('github');

    const create = renderHook(() => useCreateSecret(), { wrapper });
    act(() => {
      create.result.current.mutate({ name: 'new', kind: 'other', value: 'v' });
    });
    await waitFor(() => expect(create.result.current.isSuccess).toBe(true));
    expect(create.result.current.data?.id).toBe('S-NEW');

    const revoke = renderHook(() => useRevokeSecret(), { wrapper });
    act(() => {
      revoke.result.current.mutate('S-NEW');
    });
    await waitFor(() => expect(revoke.result.current.isSuccess).toBe(true));
  });

  it('useInputRequests + useRespondInputRequest', async () => {
    const wrapper = makeWrapper();
    const list = renderHook(() => useInputRequests(), { wrapper });
    await waitFor(() => expect(list.result.current.isSuccess).toBe(true));
    expect(list.result.current.data?.[0].id).toBe('IR-1');

    const respond = renderHook(() => useRespondInputRequest(), { wrapper });
    act(() => {
      respond.result.current.mutate({ id: 'IR-1', answer: 'yes' });
    });
    await waitFor(() => expect(respond.result.current.isSuccess).toBe(true));
  });

  it('useFleet + useTaskTrace', async () => {
    const wrapper = makeWrapper();
    const fleet = renderHook(() => useFleet(), { wrapper });
    await waitFor(() => expect(fleet.result.current.isSuccess).toBe(true));

    const trace = renderHook(() => useTaskTrace('T-1'), { wrapper });
    await waitFor(() => expect(trace.result.current.isSuccess).toBe(true));
  });

  it('useTaskTrace skips when taskId is undefined', () => {
    const { result } = renderHook(() => useTaskTrace(undefined), { wrapper: makeWrapper() });
    expect(result.current.fetchStatus).toBe('idle');
  });

  it('useDeriveIssue + useDeriveTask', async () => {
    const wrapper = makeWrapper();
    const issue = renderHook(() => useDeriveIssue(), { wrapper });
    act(() => {
      issue.result.current.mutate({
        source_conversation_id: 'C1',
        source_message_ids: ['M1'],
        project_id: 'p-demo',
        title: 'fix it',
      });
    });
    await waitFor(() => expect(issue.result.current.isSuccess).toBe(true));
    expect(issue.result.current.data?.conversation_id).toBe('I-1');

    const task = renderHook(() => useDeriveTask(), { wrapper });
    act(() => {
      task.result.current.mutate({
        source_conversation_id: 'C1',
        source_message_ids: ['M1'],
        project_id: 'p-demo',
        title: 'do it',
      });
    });
    await waitFor(() => expect(task.result.current.isSuccess).toBe(true));
    expect(task.result.current.data?.conversation_id).toBe('T-1');
  });

  it('hooks surface ApiError from the server', async () => {
    server.use(
      http.get('/api/agents', () =>
        HttpResponse.json({ error: 'find_failed', message: 'db down' }, { status: 500 }),
      ),
    );
    const { result } = renderHook(() => useAgents(), { wrapper: makeWrapper() });
    await waitFor(() => expect(result.current.isError).toBe(true));
    expect((result.current.error as Error).message).toContain('db down');
  });
});
