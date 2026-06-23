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
    expect(qk.agent('A-1')).toEqual([...P, 'agent', 'A-1']);
    expect(qk.agentTasks('A-1')).toEqual([...P, 'agentTasks', 'A-1']);
    expect(qk.agentActivity('A-1')).toEqual([...P, 'agentActivity', 'A-1']);
    expect(qk.secrets()).toEqual([...P, 'secrets']);
    expect(qk.fleet()).toEqual([...P, 'fleet']);
    expect(qk.unread('C1')).toEqual([...P, 'unread', 'C1']);
    expect(qk.projects()).toEqual([...P, 'projects']);
    expect(qk.project('p-1')).toEqual([...P, 'project', 'p-1']);
    // v2.7 ProjectManager BC per-project Issue/Task keys
    expect(qk.issues()).toEqual([...P, 'issues']);
    expect(qk.issuesByProject('p-1')).toEqual([...P, 'issuesByProject', 'p-1']);
    expect(qk.issue('IS-1')).toEqual([...P, 'issue', 'IS-1']);
    expect(qk.tasksList()).toEqual([...P, 'tasksList']);
    expect(qk.tasksByProject('p-1')).toEqual([...P, 'tasksByProject', 'p-1']);
    expect(qk.task('TS-1')).toEqual([...P, 'task', 'TS-1']);
    expect(qk.codeReposByProject('p-1')).toEqual([...P, 'codeReposByProject', 'p-1']);
  });
});
