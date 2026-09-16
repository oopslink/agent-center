import { MarkerType } from '@xyflow/react';
import { describe, expect, it } from 'vitest';
import {
  stageDisplayMeta,
  withStageTopologyEdges,
} from './PlanDetail';
import {
  layoutGraphFlow,
  layoutLegacyFlow,
  refreshGraphFlowNodes,
  refreshLegacyFlowNodes,
  PLAN_DAG_NODE_H,
  PLAN_DAG_NODE_W,
  PLAN_DAG_STAGE_HEADER_H,
} from './planDagFlow';
import type { PlanGraphNode, PlanGraphEdge, PlanStage } from '@/api/plans';

function biz(id: string): PlanGraphNode {
  return { id, category: 'business', title: id, status: 'open', task_id: `task-${id}` };
}
function ctrl(id: string, kind: 'start' | 'end' | 'condition'): PlanGraphNode {
  return { id, category: 'control', control_kind: kind, title: kind, status: 'open' };
}
const seq = (from: string, to: string): PlanGraphEdge => ({ from, to, kind: 'seq' });

function stage(over: Partial<PlanStage> & { id: string }): PlanStage {
  return {
    name: over.id,
    status: 'open',
    rounds: 0,
    max_rounds: 3,
    depends_on_stages: [],
    gate_node_id: '',
    gate_task_id: '',
    gate_spec: {
      evaluator_kind: 'human',
      pass_route: '',
      reject_route: '',
      exhausted_route: '',
    },
    members: [],
    ...over,
  };
}
function member(taskId: string): PlanStage['members'][number] {
  return { task_id: taskId, title: taskId, task_status: 'open' };
}

describe('withStageTopologyEdges — staged visual topology', () => {
  const hasEdge = (edges: PlanGraphEdge[], from: string, to: string) =>
    edges.some((edge) => edge.from === from && edge.to === to);

  it('rebuilds Start/End anchor edges from stage dependencies instead of keeping stale terminal edges', () => {
    const nodes = [
      ctrl('start', 'start'),
      biz('A'),
      ctrl('gate1', 'condition'),
      biz('B'),
      ctrl('gate2', 'condition'),
      biz('R'),
      ctrl('gateR', 'condition'),
      ctrl('end', 'end'),
    ];
    const stages = [
      stage({ id: 's1', members: [member('task-A')], gate_node_id: 'gate1' }),
      stage({ id: 's2', members: [member('task-B')], gate_node_id: 'gate2', depends_on_stages: ['s1'] }),
      stage({ id: 'remediate-s1', members: [member('task-R')], gate_node_id: 'gateR', depends_on_stages: ['s1'] }),
    ];
    const edges = [
      seq('start', 'A'),
      seq('A', 'gate1'),
      seq('gate1', 'end'), // stale: s1 is no longer terminal after remediation stages exist.
    ];

    const topology = withStageTopologyEdges(nodes, edges, stages);

    expect(hasEdge(topology, 'gate1', 'end')).toBe(false);
    expect(hasEdge(topology, 'start', 'A')).toBe(true);
    expect(hasEdge(topology, 'gate1', 'B')).toBe(true);
    expect(hasEdge(topology, 'gate1', 'R')).toBe(true);
    expect(hasEdge(topology, 'gate2', 'end')).toBe(true);
    expect(hasEdge(topology, 'gateR', 'end')).toBe(true);
  });

  it('inherits stage topology for generation tasks that only carry follows_task_id lineage', async () => {
    const nodes: PlanGraphNode[] = [
      ctrl('start', 'start'),
      { ...biz('A'), task_id: 'task-A', stage_id: 's1' },
      { ...biz('G1'), task_id: 'task-G1', stage_id: 's1' },
      ctrl('gate1', 'condition'),
      { ...biz('R1'), task_id: 'task-R1', follows_task_id: 'task-G1' },
      { ...biz('R2'), task_id: 'task-R2', follows_task_id: 'task-G1' },
      { ...biz('B'), task_id: 'task-B', stage_id: 's2' },
      { ...biz('G2'), task_id: 'task-G2', stage_id: 's2' },
      ctrl('gate2', 'condition'),
      ctrl('end', 'end'),
    ];
    const stages = [
      stage({
        id: 's1',
        members: [member('task-A'), member('task-G1')],
        gate_node_id: 'gate1',
        gate_task_id: 'task-G1',
      }),
      stage({
        id: 's2',
        members: [member('task-B'), member('task-G2')],
        gate_node_id: 'gate2',
        gate_task_id: 'task-G2',
        depends_on_stages: ['s1'],
      }),
    ];
    const edges = [
      seq('start', 'A'),
      seq('A', 'G1'),
      seq('G1', 'gate1'),
      seq('gate1', 'end'), // stale terminal anchor from before the generation repair tasks existed.
      seq('R1', 'R2'),
    ];

    const topology = withStageTopologyEdges(nodes, edges, stages);
    const layout = await layoutGraphFlow(nodes, topology, stages);
    const r1 = layout.nodes.find((node) => node.id === 'R1')!;
    const r2 = layout.nodes.find((node) => node.id === 'R2')!;

    expect(hasEdge(topology, 'gate1', 'end')).toBe(false);
    expect(hasEdge(topology, 'gate1', 'B')).toBe(false);
    expect(hasEdge(topology, 'G1', 'R1')).toBe(true);
    expect(hasEdge(topology, 'G1', 'R2')).toBe(false);
    expect(hasEdge(topology, 'R1', 'R2')).toBe(true);
    expect(hasEdge(topology, 'R2', 'B')).toBe(true);
    expect(hasEdge(topology, 'R2', 'G1')).toBe(false);
    expect(hasEdge(topology, 'start', 'R1')).toBe(false);
    expect(r1.data.kind).toBe('business');
    expect(r2.data.kind).toBe('business');
  });
});

describe('planDagFlow — React Flow + ELK adapter', () => {
  it('lays out staged business nodes inside ELK compound stage bounds and keeps cross-stage edges', async () => {
    const nodes: PlanGraphNode[] = [
      { ...biz('A'), task_id: 'task-A' },
      { ...biz('B'), task_id: 'task-B' },
      { ...biz('C'), task_id: 'task-C' },
    ];
    const stages = [
      stage({ id: 's1', members: [member('task-A'), member('task-B')] }),
      stage({ id: 's2', members: [member('task-C')], depends_on_stages: ['s1'] }),
    ];
    const layout = await layoutGraphFlow(nodes, [seq('A', 'B'), seq('B', 'C')], stages);
    const stage1 = layout.nodes.find((node) => node.id === 'stage:s1')!;
    const stage2 = layout.nodes.find((node) => node.id === 'stage:s2')!;
    const a = layout.nodes.find((node) => node.id === 'A')!;
    const b = layout.nodes.find((node) => node.id === 'B')!;
    const c = layout.nodes.find((node) => node.id === 'C')!;

    for (const node of [a, b]) {
      expect(node.parentId).toBe('stage:s1');
      expect(node.position.x).toBeGreaterThanOrEqual(0);
      expect(node.position.x + PLAN_DAG_NODE_W).toBeLessThanOrEqual((stage1.style?.width as number) + 1);
      expect(node.position.y).toBeGreaterThanOrEqual(PLAN_DAG_STAGE_HEADER_H);
      expect(node.position.y + PLAN_DAG_NODE_H).toBeLessThanOrEqual((stage1.style?.height as number) + 1);
    }
    expect(c.parentId).toBe('stage:s2');
    const overlapX = stage1.position.x < stage2.position.x + (stage2.style?.width as number)
      && stage2.position.x < stage1.position.x + (stage1.style?.width as number);
    const overlapY = stage1.position.y < stage2.position.y + (stage2.style?.height as number)
      && stage2.position.y < stage1.position.y + (stage1.style?.height as number);
    expect(overlapX && overlapY).toBe(false);
    expect(layout.edges.some((edge) => edge.source === 'B' && edge.target === 'C')).toBe(true);
    for (const edge of layout.edges) {
      expect(edge.sourceHandle).toBe('bottom');
      expect(edge.targetHandle).toBe('top');
      expect(edge.markerEnd).toEqual({ type: MarkerType.ArrowClosed });
    }
  });

  it('keeps branch and join dependencies without overlapping task cards', async () => {
    const nodes = [biz('A'), biz('B'), biz('C'), biz('D')];
    const layout = await layoutGraphFlow(nodes, [seq('A', 'B'), seq('A', 'C'), seq('B', 'D'), seq('C', 'D')]);
    const boxes = layout.nodes.filter((node) => node.data.kind === 'business');
    for (let i = 0; i < boxes.length; i += 1) {
      for (let j = i + 1; j < boxes.length; j += 1) {
        const left = boxes[i];
        const right = boxes[j];
        const overlapX = left.position.x < right.position.x + PLAN_DAG_NODE_W && right.position.x < left.position.x + PLAN_DAG_NODE_W;
        const overlapY = left.position.y < right.position.y + PLAN_DAG_NODE_H && right.position.y < left.position.y + PLAN_DAG_NODE_H;
        expect(overlapX && overlapY).toBe(false);
      }
    }
    expect(layout.edges.map((edge) => `${edge.source}->${edge.target}`).sort()).toEqual(['A->B', 'A->C', 'B->D', 'C->D']);
    expect(layout.edges.every((edge) => edge.sourceHandle === 'bottom' && edge.targetHandle === 'top')).toBe(true);
  });

  it('converts legacy depends_on into real dependency edges plus synthetic Start/End anchors', async () => {
    const layout = await layoutLegacyFlow([
      { task_id: 'A', title: 'A', assignee_ref: 'agent:a', task_status: 'open', node_status: 'ready', depends_on: [] },
      { task_id: 'B', title: 'B', assignee_ref: 'agent:b', task_status: 'open', node_status: 'blocked', depends_on: ['A'] },
    ]);
    expect(layout.nodes.some((node) => node.id === '__legacy_start__' && node.data.kind === 'control')).toBe(true);
    expect(layout.nodes.some((node) => node.id === '__legacy_end__' && node.data.kind === 'control')).toBe(true);
    expect(layout.edges.some((edge) => edge.source === 'A' && edge.target === 'B' && edge.data?.fromTaskId === 'B' && edge.data?.toTaskId === 'A')).toBe(true);
    expect(layout.edges.every((edge) => edge.sourceHandle === 'bottom' && edge.targetHandle === 'top')).toBe(true);
    expect(layout.edges.filter((edge) => edge.data?.kind === 'synthetic').map((edge) => `${edge.source}->${edge.target}`).sort()).toEqual([
      'B->__legacy_end__',
      '__legacy_start__->A',
    ]);
  });

  it('refreshes node and stage data without changing ELK positions', async () => {
    const graphNodes = [{ ...biz('A'), title: 'old graph title' }];
    const stages = [stage({ id: 's1', status: 'open', members: [member('task-A')] })];
    const graphLayout = await layoutGraphFlow(graphNodes, [], stages);
    const graphPosition = graphLayout.nodes.find((node) => node.id === 'A')!.position;
    const refreshedGraph = refreshGraphFlowNodes(
      graphLayout.nodes,
      [{ ...graphNodes[0], title: 'new graph title', assignee_ref: 'agent:new' }],
      [{ ...stages[0], status: 'running' }],
    );
    const refreshedBusiness = refreshedGraph.find((node) => node.id === 'A')!;
    expect(refreshedBusiness.position).toEqual(graphPosition);
    expect(refreshedBusiness.data.kind).toBe('business');
    if (refreshedBusiness.data.kind === 'business') {
      expect(refreshedBusiness.data.node.title).toBe('new graph title');
      expect(refreshedBusiness.data.node.assignee_ref).toBe('agent:new');
    }
    const refreshedStage = refreshedGraph.find((node) => node.id === 'stage:s1')!;
    expect(refreshedStage.data.kind === 'stage' && refreshedStage.data.stage.status).toBe('running');

    const legacyLayout = await layoutLegacyFlow([
      { task_id: 'A', title: 'old legacy title', assignee_ref: 'agent:a', task_status: 'open', node_status: 'ready', depends_on: [] },
    ]);
    const legacyPosition = legacyLayout.nodes.find((node) => node.id === 'A')!.position;
    const refreshedLegacy = refreshLegacyFlowNodes(legacyLayout.nodes, [
      { task_id: 'A', title: 'new legacy title', assignee_ref: 'agent:b', task_status: 'running', node_status: 'running', depends_on: [] },
    ], []);
    const refreshedLegacyNode = refreshedLegacy.find((node) => node.id === 'A')!;
    expect(refreshedLegacyNode.position).toEqual(legacyPosition);
    expect(refreshedLegacyNode.data.kind === 'legacy' && refreshedLegacyNode.data.node.node_status).toBe('running');
  });
});

describe('stageDisplayMeta — mockup stage refs', () => {
  it('uses compact plan-local refs, removes duplicate name prefixes, and labels gates', () => {
    const stages = [
      stage({ id: 'stage-opaque-a', name: 'S1 Data layer', gate_node_id: 'gate-a' }),
      stage({ id: 'stage-opaque-b', name: 'S2 · Git storage', gate_node_id: 'gate-b' }),
      stage({ id: 'stage-opaque-c', name: 'Release' }),
    ];

    const display = stageDisplayMeta(stages);

    expect(display.byStageId.get('stage-opaque-a')).toEqual({ ref: 'S1', name: 'Data layer' });
    expect(display.byStageId.get('stage-opaque-b')).toEqual({ ref: 'S2', name: 'Git storage' });
    expect(display.byStageId.get('stage-opaque-c')).toEqual({ ref: 'S3', name: 'Release' });
    expect(display.byGateNodeId.get('gate-a')).toBe('S1');
    expect(display.byGateNodeId.get('gate-b')).toBe('S2');
  });

  it('keeps the no-stage display metadata empty', () => {
    const display = stageDisplayMeta([]);
    expect(display.byStageId.size).toBe(0);
    expect(display.byGateNodeId.size).toBe(0);
  });
});
