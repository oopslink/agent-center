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
  });
});
