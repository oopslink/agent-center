import { describe, expect, it } from 'vitest';
import { qk } from './queryKeys';

// v2.6: every org-scoped key is prefixed with ['org', <slug>]. In the test
// environment (no /organizations/{slug} URL) the slug resolves to 'no-org'.
const P = ['org', 'no-org'];

describe('query key factory', () => {
  it('emits stable org-scoped shapes', () => {
    expect(qk.conversations()).toEqual([...P, 'conversations']);
    expect(qk.conversations('channel')).toEqual([...P, 'conversations', { kind: 'channel' }]);
    expect(qk.conversation('C1')).toEqual([...P, 'conversation', 'C1']);
    expect(qk.messages('C1')).toEqual([...P, 'messages', 'C1']);
    expect(qk.agents()).toEqual([...P, 'agents']);
    expect(qk.agent('aa')).toEqual([...P, 'agent', 'aa']);
    expect(qk.secrets()).toEqual([...P, 'secrets']);
    expect(qk.inputRequests()).toEqual([...P, 'inputRequests']);
    expect(qk.fleet()).toEqual([...P, 'fleet']);
    expect(qk.taskTrace('T1')).toEqual([...P, 'taskTrace', 'T1']);
    expect(qk.unread('C1')).toEqual([...P, 'unread', 'C1']);
    expect(qk.projects()).toEqual([...P, 'projects']);
    expect(qk.project('p-1')).toEqual([...P, 'project', 'p-1']);
    // v2.3-5b BC-native Issue/Task keys
    expect(qk.issues()).toEqual([...P, 'issues']);
    expect(qk.issues({})).toEqual([...P, 'issues']);
    expect(qk.issues({ projectId: 'p-1' })).toEqual([...P, 'issues', { projectId: 'p-1' }]);
    expect(qk.issues({ projectId: 'p-1', status: 'open' })).toEqual([
      ...P,
      'issues',
      { projectId: 'p-1', status: 'open' },
    ]);
    expect(qk.issue('IS-1')).toEqual([...P, 'issue', 'IS-1']);
    expect(qk.tasksList()).toEqual([...P, 'tasksList']);
    expect(qk.tasksList({ projectId: 'p-1' })).toEqual([...P, 'tasksList', { projectId: 'p-1' }]);
    expect(qk.task('TS-1')).toEqual([...P, 'task', 'TS-1']);
  });
});
