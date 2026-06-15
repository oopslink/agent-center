import { describe, expect, it } from 'vitest';
import { decideDrop } from './boardDrop';

describe('decideDrop (Work Board touch drop logic)', () => {
  it('Backlog task → plan = SELECT', () => {
    expect(decideDrop(null, { kind: 'plan', planId: 'P1' })).toEqual({ op: 'select', toPlanId: 'P1' });
  });
  it('Backlog task → pool = SELECT', () => {
    expect(decideDrop(null, { kind: 'pool', planId: 'POOL' })).toEqual({ op: 'select', toPlanId: 'POOL' });
  });
  it('Plan task → Backlog = REMOVE', () => {
    expect(decideDrop('P1', { kind: 'backlog' })).toEqual({ op: 'remove', fromPlanId: 'P1' });
  });
  it('Plan task → another plan = MOVE', () => {
    expect(decideDrop('P1', { kind: 'plan', planId: 'P2' })).toEqual({
      op: 'move',
      fromPlanId: 'P1',
      toPlanId: 'P2',
    });
  });
  it('Plan task dropped onto its own plan = no-op', () => {
    expect(decideDrop('P1', { kind: 'plan', planId: 'P1' })).toEqual({ op: 'noop' });
  });
  it('Backlog task → Backlog = no-op', () => {
    expect(decideDrop(null, { kind: 'backlog' })).toEqual({ op: 'noop' });
  });

  // T121 — the Assignment Pool is a full drag participant (its task-set is freely
  // editable, like the Backlog/draft plans). A pool task carries its pool id as
  // fromPlanId, so the same select/remove/move logic applies.
  it('Pool task → Backlog = REMOVE', () => {
    expect(decideDrop('POOL', { kind: 'backlog' })).toEqual({ op: 'remove', fromPlanId: 'POOL' });
  });
  it('Pool task → draft plan = MOVE', () => {
    expect(decideDrop('POOL', { kind: 'plan', planId: 'P1' })).toEqual({
      op: 'move',
      fromPlanId: 'POOL',
      toPlanId: 'P1',
    });
  });
  it('Draft-plan task → Pool = MOVE', () => {
    expect(decideDrop('P1', { kind: 'pool', planId: 'POOL' })).toEqual({
      op: 'move',
      fromPlanId: 'P1',
      toPlanId: 'POOL',
    });
  });
  it('Pool task dropped onto the Pool = no-op', () => {
    expect(decideDrop('POOL', { kind: 'pool', planId: 'POOL' })).toEqual({ op: 'noop' });
  });
});
