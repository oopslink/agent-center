import { describe, expect, it } from 'vitest';
import type { PlanNode } from '@/api/plans';
import { dependencyEdgeError, validDropTargets } from './planDagEdit';

// minimal PlanNode factory (only task_id + depends_on matter to the validator)
function n(task_id: string, depends_on: string[] = []): PlanNode {
  return {
    task_id,
    title: task_id,
    assignee_ref: '',
    task_status: 'open',
    node_status: 'ready',
    depends_on,
  };
}

// Chain A ← B ← C  (B depends_on A; C depends_on B)
const chain = [n('A'), n('B', ['A']), n('C', ['B'])];

describe('dependencyEdgeError', () => {
  it('rejects a self-edge', () => {
    expect(dependencyEdgeError(chain, 'A', 'A')).toBe('self');
  });

  it('rejects an edge that already exists', () => {
    expect(dependencyEdgeError(chain, 'B', 'A')).toBe('exists');
  });

  it('rejects an edge that would close a cycle', () => {
    // A depends_on C would create A→C→B→A (C transitively depends on A)
    expect(dependencyEdgeError(chain, 'A', 'C')).toBe('cycle');
  });

  it('rejects a direct back-edge cycle', () => {
    // A depends_on B, but B already depends_on A → cycle
    expect(dependencyEdgeError(chain, 'A', 'B')).toBe('cycle');
  });

  it('allows a legal new edge', () => {
    // C depends_on A is fine (A is upstream of C already; no cycle, not existing)
    expect(dependencyEdgeError(chain, 'C', 'A')).toBeNull();
  });
});

describe('validDropTargets', () => {
  it('excludes self, existing deps, and cycle-forming targets', () => {
    // From A: A=self, B=cycle (B→A exists), C=cycle (C→B→A). Nothing legal.
    expect([...validDropTargets(chain, 'A')].sort()).toEqual([]);
  });

  it('includes only legal upstream targets', () => {
    // From C: C=self, B=exists. A is legal (C already depends on B which depends
    // on A, but a direct C→A edge is not a cycle and not pre-existing).
    expect([...validDropTargets(chain, 'C')].sort()).toEqual(['A']);
  });

  it('a standalone node can target everything except itself', () => {
    const flat = [n('X'), n('Y'), n('Z')];
    expect([...validDropTargets(flat, 'X')].sort()).toEqual(['Y', 'Z']);
  });
});
