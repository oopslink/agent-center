import ELK from 'elkjs/lib/elk.bundled.js';
import { MarkerType } from '@xyflow/react';
import type { Edge as FlowEdge, Node as FlowNode } from '@xyflow/react';
import type { PlanGraphEdge, PlanGraphEdgeKind, PlanGraphNode, PlanNode, PlanStage } from '@/api/plans';

export const PLAN_DAG_NODE_W = 220;
export const PLAN_DAG_NODE_H = 84;
export const PLAN_DAG_CTRL_W = 76;
export const PLAN_DAG_STAGE_HEADER_H = 96;

const PAD_X = 36;
const PAD_Y = 28;
const LEVEL_GAP = 78;
const NODE_GAP = 40;
const elk = new ELK();

export interface FlowStageData extends Record<string, unknown> {
  kind: 'stage';
  stage: PlanStage;
}

export interface FlowBusinessData extends Record<string, unknown> {
  kind: 'business';
  node: PlanGraphNode;
}

export interface FlowControlData extends Record<string, unknown> {
  kind: 'control';
  node: PlanGraphNode;
}

export interface FlowLegacyData extends Record<string, unknown> {
  kind: 'legacy';
  node: PlanNode;
}

export interface FlowSyntheticData extends Record<string, unknown> {
  kind: 'synthetic';
  label: 'start' | 'end';
}

export type PlanDagFlowData = FlowStageData | FlowBusinessData | FlowControlData | FlowLegacyData | FlowSyntheticData;

export type PlanDagFlowNode = FlowNode<PlanDagFlowData>;
export type PlanDagFlowEdge = FlowEdge<{ kind: PlanGraphEdgeKind | 'synthetic'; fromTaskId?: string; toTaskId?: string }>;

export interface PlanDagFlowLayout {
  nodes: PlanDagFlowNode[];
  edges: PlanDagFlowEdge[];
  width: number;
  height: number;
}

type ElkNode = {
  id: string;
  width?: number;
  height?: number;
  children?: ElkNode[];
  edges?: ElkEdge[];
  layoutOptions?: Record<string, string>;
  x?: number;
  y?: number;
};

type ElkEdge = {
  id: string;
  sources: string[];
  targets: string[];
};

function layoutOptions(): Record<string, string> {
  return {
    'elk.algorithm': 'layered',
    'elk.direction': 'DOWN',
    'elk.layered.spacing.nodeNodeBetweenLayers': String(LEVEL_GAP),
    'elk.spacing.nodeNode': String(NODE_GAP),
    'elk.spacing.edgeNode': '28',
    'elk.spacing.edgeEdge': '18',
    'elk.padding': `[top=${PAD_Y},left=${PAD_X},bottom=${PAD_Y},right=${PAD_X}]`,
    'elk.layered.crossingMinimization.strategy': 'LAYER_SWEEP',
    'elk.layered.nodePlacement.strategy': 'BRANDES_KOEPF',
    'elk.edgeRouting': 'ORTHOGONAL',
  };
}

function stageLayoutOptions(): Record<string, string> {
  return {
    ...layoutOptions(),
    'elk.padding': `[top=${PLAN_DAG_STAGE_HEADER_H},left=${PAD_X},bottom=${PAD_Y},right=${PAD_X}]`,
  };
}

function nodeSize(node: PlanGraphNode): { width: number; height: number } {
  return {
    width: node.category === 'control' ? PLAN_DAG_CTRL_W : PLAN_DAG_NODE_W,
    height: PLAN_DAG_NODE_H,
  };
}

function toStageMemberMap(nodes: PlanGraphNode[], stages: PlanStage[]): Map<string, string> {
  const taskStage = new Map<string, string>();
  for (const stage of stages) {
    for (const member of stage.members ?? []) taskStage.set(member.task_id, stage.id);
  }
  const nodeStage = new Map<string, string>();
  for (const node of nodes) {
    if (node.category !== 'business' || !node.task_id) continue;
    const stageId = taskStage.get(node.task_id);
    if (stageId) nodeStage.set(node.id, stageId);
  }
  return nodeStage;
}

function toAbsolute(node: ElkNode, offset = { x: 0, y: 0 }, out = new Map<string, { x: number; y: number; w: number; h: number }>()): Map<string, { x: number; y: number; w: number; h: number }> {
  const x = offset.x + (node.x ?? 0);
  const y = offset.y + (node.y ?? 0);
  if (node.id !== 'root') out.set(node.id, { x, y, w: node.width ?? 0, h: node.height ?? 0 });
  for (const child of node.children ?? []) toAbsolute(child, { x, y }, out);
  return out;
}

function edgeId(prefix: string, from: string, to: string, index: number): string {
  return `${prefix}:${from}->${to}:${index}`;
}

function flowEdge(edge: PlanGraphEdge, index: number): PlanDagFlowEdge {
  return {
    id: edgeId('graph', edge.from, edge.to, index),
    source: edge.from,
    target: edge.to,
    sourceHandle: 'bottom',
    targetHandle: 'top',
    type: edge.kind === 'loopback' ? 'smoothstep' : 'step',
    animated: edge.kind === 'loopback',
    data: { kind: edge.kind },
    className: `plan-flow-edge plan-flow-edge--${edge.kind}`,
    markerEnd: { type: MarkerType.ArrowClosed },
  };
}

export async function layoutGraphFlow(nodes: PlanGraphNode[], edges: PlanGraphEdge[], stages: PlanStage[] = []): Promise<PlanDagFlowLayout> {
  const nodeById = new Map(nodes.map((node) => [node.id, node]));
  const validEdges = edges.filter((edge) => nodeById.has(edge.from) && nodeById.has(edge.to));
  const nodeStage = toStageMemberMap(nodes, stages);
  const staged = stages.length > 0;

  const children: ElkNode[] = [];
  if (staged) {
    for (const stage of stages) {
      const stageChildren = nodes
        .filter((node) => nodeStage.get(node.id) === stage.id)
        .map((node) => ({ id: node.id, ...nodeSize(node) }));
      children.push({ id: `stage:${stage.id}`, children: stageChildren, layoutOptions: stageLayoutOptions() });
    }
    for (const node of nodes) {
      if (nodeStage.has(node.id)) continue;
      children.push({ id: node.id, ...nodeSize(node) });
    }
  } else {
    children.push(...nodes.map((node) => ({ id: node.id, ...nodeSize(node) })));
  }

  const graph: ElkNode = {
    id: 'root',
    layoutOptions: layoutOptions(),
    children,
    edges: validEdges.map((edge, index) => ({ id: edgeId('elk', edge.from, edge.to, index), sources: [edge.from], targets: [edge.to] })),
  };
  const laidOut = await elk.layout(graph);
  const absolute = toAbsolute(laidOut as ElkNode);

  const flowNodes: PlanDagFlowNode[] = [];
  if (staged) {
    for (const stage of stages) {
      const pos = absolute.get(`stage:${stage.id}`);
      if (!pos) continue;
      flowNodes.push({
        id: `stage:${stage.id}`,
        type: 'stage',
        position: { x: pos.x, y: pos.y },
        width: pos.w,
        height: pos.h,
        selectable: false,
        draggable: false,
        data: { kind: 'stage', stage },
        style: { width: pos.w, height: pos.h, zIndex: -1 },
      });
    }
  }
  for (const node of nodes) {
    const pos = absolute.get(node.id);
    if (!pos) continue;
    const stageId = nodeStage.get(node.id);
    const stagePos = stageId ? absolute.get(`stage:${stageId}`) : undefined;
    flowNodes.push({
      id: node.id,
      type: node.category === 'control' ? 'control' : 'business',
      position: stagePos ? { x: pos.x - stagePos.x, y: pos.y - stagePos.y } : { x: pos.x, y: pos.y },
      parentId: stageId ? `stage:${stageId}` : undefined,
      extent: stageId ? 'parent' : undefined,
      width: pos.w,
      height: pos.h,
      selectable: false,
      draggable: false,
      data: node.category === 'control' ? { kind: 'control', node } : { kind: 'business', node },
    });
  }

  return {
    nodes: flowNodes,
    edges: validEdges.map(flowEdge),
    width: Math.max(laidOut.width ?? 0, 200),
    height: Math.max(laidOut.height ?? 0, 180),
  };
}

export function legacyNodesToGraph(nodes: PlanNode[]): { graphNodes: PlanGraphNode[]; graphEdges: PlanGraphEdge[] } {
  const graphNodes: PlanGraphNode[] = nodes.map((node) => ({
    id: node.task_id,
    category: 'business',
    title: node.title,
    status: 'open',
    task_id: node.task_id,
    task_status: node.task_status,
    org_ref: node.org_ref,
    assignee_ref: node.assignee_ref,
  }));
  const graphEdges: PlanGraphEdge[] = [];
  const ids = new Set(nodes.map((node) => node.task_id));
  const dependedOn = new Set<string>();
  for (const node of nodes) {
    for (const dep of node.depends_on) {
      if (!ids.has(dep)) continue;
      graphEdges.push({ from: dep, to: node.task_id, kind: 'seq' });
      dependedOn.add(dep);
    }
  }
  if (nodes.length > 0) {
    const startId = '__legacy_start__';
    const endId = '__legacy_end__';
    graphNodes.push(
      { id: startId, category: 'control', control_kind: 'start', title: 'Start', status: 'open' },
      { id: endId, category: 'control', control_kind: 'end', title: 'End', status: 'open' },
    );
    for (const root of nodes.filter((node) => !node.depends_on.some((dep) => ids.has(dep)))) {
      graphEdges.push({ from: startId, to: root.task_id, kind: 'seq' });
    }
    for (const leaf of nodes.filter((node) => !dependedOn.has(node.task_id))) {
      graphEdges.push({ from: leaf.task_id, to: endId, kind: 'seq' });
    }
  }
  return { graphNodes, graphEdges };
}

export async function layoutLegacyFlow(nodes: PlanNode[], stages: PlanStage[] = []): Promise<PlanDagFlowLayout> {
  const { graphNodes, graphEdges } = legacyNodesToGraph(nodes);
  const layout = await layoutGraphFlow(graphNodes, graphEdges, stages);
  return {
    ...layout,
    nodes: layout.nodes.map((node) => {
      if (node.data.kind !== 'business') return node;
      const graphNode = (node.data as FlowBusinessData).node;
      const planNode = nodes.find((candidate) => candidate.task_id === graphNode.task_id);
      return planNode ? { ...node, type: 'legacy', data: { kind: 'legacy', node: planNode } } : node;
    }),
    edges: layout.edges.map((edge) => {
      const target = nodes.find((node) => node.task_id === edge.target);
      const source = nodes.find((node) => node.task_id === edge.source);
      if (!target || !source) return { ...edge, data: { kind: 'synthetic' } };
      return { ...edge, data: { kind: 'seq', fromTaskId: target.task_id, toTaskId: source.task_id }, className: 'plan-flow-edge plan-flow-edge--seq' };
    }),
  };
}

export function refreshGraphFlowNodes(
  flowNodes: PlanDagFlowNode[],
  nodes: PlanGraphNode[],
  stages: PlanStage[],
): PlanDagFlowNode[] {
  const nodeById = new Map(nodes.map((node) => [node.id, node]));
  const stageById = new Map(stages.map((stage) => [`stage:${stage.id}`, stage]));
  return flowNodes.map((flowNode) => {
    if (flowNode.data.kind === 'stage') {
      const stage = stageById.get(flowNode.id);
      return stage ? { ...flowNode, data: { kind: 'stage', stage } } : flowNode;
    }
    if (flowNode.data.kind !== 'business' && flowNode.data.kind !== 'control') return flowNode;
    const node = nodeById.get(flowNode.id);
    if (!node) return flowNode;
    return {
      ...flowNode,
      data: node.category === 'control' ? { kind: 'control', node } : { kind: 'business', node },
    };
  });
}

export function refreshLegacyFlowNodes(
  flowNodes: PlanDagFlowNode[],
  nodes: PlanNode[],
  stages: PlanStage[],
): PlanDagFlowNode[] {
  const nodeById = new Map(nodes.map((node) => [node.task_id, node]));
  const stageById = new Map(stages.map((stage) => [`stage:${stage.id}`, stage]));
  return flowNodes.map((flowNode) => {
    if (flowNode.data.kind === 'stage') {
      const stage = stageById.get(flowNode.id);
      return stage ? { ...flowNode, data: { kind: 'stage', stage } } : flowNode;
    }
    if (flowNode.data.kind !== 'legacy') return flowNode;
    const node = nodeById.get(flowNode.id);
    return node ? { ...flowNode, data: { kind: 'legacy', node } } : flowNode;
  });
}
