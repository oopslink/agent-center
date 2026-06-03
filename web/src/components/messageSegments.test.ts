import { describe, expect, it } from 'vitest';
import type { Message } from '@/api/types';
import { groupMessagesByWorkItem } from './messageSegments';

function msg(id: string, workItemRef?: string): Message {
  return {
    id,
    conversation_id: 'conv-1',
    sender_identity_id: 'agent:builder',
    content_kind: 'text',
    content: `body ${id}`,
    direction: 'internal',
    posted_at: `2026-05-30T0${id}:00:00Z`,
    ...(workItemRef
      ? { context_refs: { work_item_ref: workItemRef, task_ref: '', agent_ref: '' } }
      : {}),
  };
}

describe('groupMessagesByWorkItem (#137 work-item segments)', () => {
  it('returns no segments for an empty message list', () => {
    expect(groupMessagesByWorkItem([])).toEqual([]);
  });

  it('groups consecutive messages sharing a work_item_ref into one segment', () => {
    const segs = groupMessagesByWorkItem([msg('1', 'wi-A'), msg('2', 'wi-A'), msg('3', 'wi-A')]);
    expect(segs).toHaveLength(1);
    expect(segs[0].workItemRef).toBe('wi-A');
    expect(segs[0].label).toBe('Work item wi-A');
    expect(segs[0].messages.map((m) => m.id)).toEqual(['1', '2', '3']);
  });

  it('starts a NEW segment when work_item_ref changes (re-dispatch is its own segment, no mis-merge)', () => {
    // A → B → A: the second run of wi-A must NOT merge back into the first.
    const segs = groupMessagesByWorkItem([
      msg('1', 'wi-A'),
      msg('2', 'wi-B'),
      msg('3', 'wi-A'),
    ]);
    expect(segs.map((s) => s.workItemRef)).toEqual(['wi-A', 'wi-B', 'wi-A']);
    expect(segs.map((s) => s.messages.map((m) => m.id))).toEqual([['1'], ['2'], ['3']]);
    // Keys are unique even though wi-A repeats, so React won't collapse them.
    expect(new Set(segs.map((s) => s.key)).size).toBe(3);
  });

  it('places messages with no work_item_ref into a labeled "Unassociated work item" segment, ordered chronologically before the first WI segment', () => {
    const segs = groupMessagesByWorkItem([msg('1'), msg('2'), msg('3', 'wi-A')]);
    expect(segs.map((s) => s.workItemRef)).toEqual(['', 'wi-A']);
    expect(segs[0].label).toBe('Unassociated work item');
    expect(segs[0].messages.map((m) => m.id)).toEqual(['1', '2']);
    expect(segs[1].messages.map((m) => m.id)).toEqual(['3']);
  });

  it('keeps grouping stable: every message lands in exactly one segment, order preserved (no silent drop)', () => {
    const input = [msg('1'), msg('2', 'wi-A'), msg('3', 'wi-A'), msg('4'), msg('5', 'wi-B')];
    const segs = groupMessagesByWorkItem(input);
    const flattened = segs.flatMap((s) => s.messages.map((m) => m.id));
    expect(flattened).toEqual(['1', '2', '3', '4', '5']);
  });

  it('keeps a no-ref message in its chronological position (does NOT hoist it before WI segments)', () => {
    // PD §-1 ruling: a conversation is a time-ordered stream. An unassociated
    // message sandwiched between two work-item runs stays in time order — its
    // "Unassociated work item" segment sits BETWEEN the WI segments, not pulled to the top.
    const segs = groupMessagesByWorkItem([msg('1', 'wi-A'), msg('2'), msg('3', 'wi-B')]);
    expect(segs.map((s) => s.workItemRef)).toEqual(['wi-A', '', 'wi-B']);
    expect(segs[1].label).toBe('Unassociated work item');
    expect(segs[1].messages.map((m) => m.id)).toEqual(['2']);
  });
});
