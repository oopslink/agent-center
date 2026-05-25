import { describe, expect, it } from 'vitest';
import { qk } from './queryKeys';

describe('query key factory', () => {
  it('emits stable shapes', () => {
    expect(qk.conversations()).toEqual(['conversations']);
    expect(qk.conversations('channel')).toEqual(['conversations', { kind: 'channel' }]);
    expect(qk.conversation('C1')).toEqual(['conversation', 'C1']);
    expect(qk.messages('C1')).toEqual(['messages', 'C1']);
    expect(qk.agents()).toEqual(['agents']);
    expect(qk.agent('aa')).toEqual(['agent', 'aa']);
    expect(qk.secrets()).toEqual(['secrets']);
    expect(qk.inputRequests()).toEqual(['inputRequests']);
    expect(qk.fleet()).toEqual(['fleet']);
    expect(qk.taskTrace('T1')).toEqual(['taskTrace', 'T1']);
    expect(qk.unread('C1')).toEqual(['unread', 'C1']);
    expect(qk.projects()).toEqual(['projects']);
    expect(qk.project('p-1')).toEqual(['project', 'p-1']);
    // v2.3-5b BC-native Issue/Task keys
    expect(qk.issues()).toEqual(['issues']);
    expect(qk.issues({})).toEqual(['issues']);
    expect(qk.issues({ projectId: 'p-1' })).toEqual([
      'issues',
      { projectId: 'p-1' },
    ]);
    expect(qk.issues({ projectId: 'p-1', status: 'open' })).toEqual([
      'issues',
      { projectId: 'p-1', status: 'open' },
    ]);
    expect(qk.issue('IS-1')).toEqual(['issue', 'IS-1']);
    expect(qk.tasksList()).toEqual(['tasksList']);
    expect(qk.tasksList({ projectId: 'p-1' })).toEqual([
      'tasksList',
      { projectId: 'p-1' },
    ]);
    expect(qk.task('TS-1')).toEqual(['task', 'TS-1']);
  });
});
