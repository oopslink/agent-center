import type { PlanGenerationRead, PlanGraphEdge, PlanGraphNode } from '@/api/plans';

// Display metadata only. These edges must never enter dependency mutations or
// readiness calculations: immutable ancestor snapshots are their provenance.
export type HistoricalGraphEdge = PlanGraphEdge & { historical?: boolean };

export function withHistoricalEdges(
  nodes: PlanGraphNode[],
  edges: PlanGraphEdge[],
  read: PlanGenerationRead | undefined,
  generationId: string | undefined,
  historicalTaskIds: Set<string>,
): HistoricalGraphEdge[] {
  const byTask = new Map(nodes.filter((node) => node.task_id).map((node) => [node.task_id!, node.id]));
  const historicalNodes = new Set([...historicalTaskIds].flatMap((id) => byTask.has(id) ? [byTask.get(id)!] : []));
  const touchesHistory = (edge: PlanGraphEdge) => historicalNodes.has(edge.from) || historicalNodes.has(edge.to);
  const key = (edge: PlanGraphEdge) => JSON.stringify([edge.from, edge.to, edge.kind]);
  const result: HistoricalGraphEdge[] = edges.map((edge) => ({ ...edge, historical: touchesHistory(edge) }));
  const seenEdges = new Set(result.map(key));
  const generations = new Map(read?.generations.map((generation) => [generation.id, generation]));
  const visited = new Set<string>();
  let id = generationId;
  while (id && !visited.has(id)) {
    visited.add(id);
    const generation = generations.get(id);
    if (!generation) break;
    for (const dependency of generation.snapshot.edges) {
      const from = byTask.get(dependency.to_task_id);
      const to = byTask.get(dependency.from_task_id);
      if (!from || !to) continue;
      const edge: HistoricalGraphEdge = {
        from, to,
        kind: dependency.kind === 'conditional' || dependency.kind === 'loopback' ? dependency.kind : 'seq',
        historical: true,
      };
      if (!touchesHistory(edge) || seenEdges.has(key(edge))) continue;
      seenEdges.add(key(edge));
      result.push(edge);
    }
    id = generation.parent_generation_id;
  }
  return result;
}
