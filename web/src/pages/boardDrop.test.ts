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
});
