import React, { useMemo, useState } from 'react';
import { useParams } from 'react-router-dom';
import { OrgLink } from '@/OrgContext';
import { useProject } from '@/api/projects';
import {
  usePlan,
  useStartPlan,
  useStopPlan,
  useAdvancePlan,
  useAddDependency,
  useRemoveDependency,
  useRemoveTaskFromPlan,
  type Plan,
  type PlanNode,
  type PlanNodeStatus,
} from '@/api/plans';
import { useConversation } from '@/api/conversations';
import { useDisplayNameResolver, normalizeIdentityRef, refKind } from '@/api/members';
import { formatLocalTime } from '@/utils/time';
import { Skeleton } from '@/components/Skeleton';
import { Breadcrumb } from '@/components/Breadcrumb';
import { ErrorState } from '@/components/ErrorState';
import { Avatar } from '@/components/Avatar';
import { StatusChip, idHandle } from '@/components/workItemDisplay';
import { PlanStatusChip, PlanFailedIndicator, AutoAdvancingIndicator, planProgressLabel } from '@/components/planDisplay';
import { ConversationView } from '@/components/ConversationView';
import { SenderSidebarProvider } from '@/components/SenderSidebarContext';

// PlanDetail (/projects/:id/plans/:planId) — v2.9 Plan-Orchestration EXECUTION
// view (#287). The mockup's ② Plan Detail: a header (name + status + failed +
// meta + Start/Stop), two tabs (DAG「推进计划」 / Task list「任务列表 N」 — NO
// backlog; selection lives on the #291 Work Board), a grid of the DAG (main) +
// the Plan conversation (side ~330px). The #286 backlog→Plan SELECTION is
// REMOVED here entirely.
//
// node_status is DERIVED by the orchestrator (§9.2) — we DISPLAY it, never store
// or edit it. This view renders the derived DAG + header lifecycle controls
// (Start / Stop / Advance, each once).
//
// v2.9 Stage A1 (#287 fast-follow): a DRAFT plan's DAG is now EDITABLE here — a
// draft-only dependency-edge editor (add via labeled selects, remove via a list
// of edges) sits below the graph. The backend AddPlanDependency/RemovePlanDependency
// are draft-gated (§9.4: running/done → ErrPlanNotDraft), cycle-guarded
// (ErrPlanCycle) and self-edge-rejected (ErrSelfDependency); the editor is gated
// to draft to match, and a running/done plan stays DISPLAY-ONLY.

type Tab = 'dag' | 'tasks';

export default function PlanDetail(): React.ReactElement {
  const { id = '', planId = '' } = useParams<{ id: string; planId: string }>();
  const project = useProject(id);
  const plan = usePlan(id, planId);
  const [tab, setTab] = useState<Tab>('dag');

  const projectName = project.data?.name ?? id;

  if (plan.isLoading) {
    return (
      <section className="space-y-3" data-testid="page-PlanDetail">
        <Skeleton width="16rem" height="1.75rem" />
        <Skeleton height="8rem" />
      </section>
    );
  }
  if (plan.isError) {
    return (
      <section className="space-y-3" data-testid="page-PlanDetail">
        <ErrorState
          message="Couldn't load this plan."
          error={plan.error}
          testId="plan-not-found"
        />
        <OrgLink
          to={`/projects/${encodeURIComponent(id)}/plans`}
          className="text-xs text-accent hover:underline"
        >
          ← Back to plans
        </OrgLink>
      </section>
    );
  }
  if (!plan.data) {
    return (
      <section className="text-sm text-danger" data-testid="page-PlanDetail">
        Plan lookup failed.
      </section>
    );
  }

  const p = plan.data;
  const nodes = p.nodes ?? [];

  return (
    <section className="space-y-4" data-testid="page-PlanDetail" data-plan-id={p.id}>
      <Breadcrumb
        items={[
          { label: 'Projects', to: '/projects' },
          { label: projectName, to: `/projects/${encodeURIComponent(id)}` },
          { label: 'Plans', to: `/projects/${encodeURIComponent(id)}/plans` },
          { label: p.name },
        ]}
      />

      <div className="rounded-lg border border-border-base bg-bg-elevated shadow-1" data-testid="plan-detail-card">
        <PlanDetailHeader projectId={id} plan={p} />

        {/* Tabs — DAG (推进计划) / Task list (任务列表 N). NO backlog tab. */}
        <div className="flex items-center gap-1 px-4 pt-2" role="tablist" data-testid="plan-tabs">
          <TabButton id="dag" active={tab === 'dag'} onSelect={setTab}>
            DAG (推进计划)
          </TabButton>
          <TabButton id="tasks" active={tab === 'tasks'} onSelect={setTab}>
            Task list (任务列表 {nodes.length})
          </TabButton>
          <span className="ml-2 self-center text-[0.6875rem] text-text-muted">
            ← execution view (no backlog — planning is on the Board)
          </span>
        </div>

        {/* Grid — main (DAG / task list) + side (plan conversation ~330px). */}
        <div className="grid grid-cols-1 lg:grid-cols-[1fr_330px]">
          <div className="border-b border-border-base p-4 lg:border-b-0 lg:border-r" data-testid="plan-detail-main">
            {tab === 'dag' ? (
              <PlanDag projectId={id} plan={p} />
            ) : (
              <PlanTaskList projectId={id} plan={p} />
            )}
          </div>
          <div className="p-4" data-testid="plan-detail-side">
            <PlanConversationSide conversationId={p.conversation_id} />
          </div>
        </div>
      </div>
    </section>
  );
}

// ── Header ────────────────────────────────────────────────────────────────
function PlanDetailHeader({ projectId, plan }: { projectId: string; plan: Plan }): React.ReactElement {
  const resolveName = useDisplayNameResolver();
  const start = useStartPlan(projectId, plan.id);
  const stop = useStopPlan(projectId, plan.id);
  const advance = useAdvancePlan(projectId, plan.id);

  const creatorName = resolveName(plan.creator_ref);
  const creatorLabel =
    creatorName === plan.creator_ref ? normalizeIdentityRef(plan.creator_ref) : creatorName;

  return (
    <header className="space-y-2 border-b border-border-base p-4" data-testid="plan-detail-header">
      <div className="flex flex-wrap items-center gap-2">
        <h1 className="font-heading text-xl font-semibold text-text-primary" title={plan.id}>
          {plan.name}
        </h1>
        <PlanStatusChip status={plan.status} />
        {/* P2-4: a RUNNING plan IS being auto-advanced (the orchestrator
            dispatches ready nodes by events). Subtle informational signal. */}
        {plan.status === 'running' && <AutoAdvancingIndicator variant="detail" />}
        <PlanFailedIndicator hasFailed={plan.has_failed} />
        <span className="flex-1" />
        {/* Lifecycle (§9.4 / §9.6): running → Advance (dispatch ready) + Stop
            (→ draft); draft → Start. Each control is rendered exactly ONCE here
            (the DAG footer keeps the legend only). */}
        {plan.status === 'running' && (
          <>
            {/* Manual Advance is KEPT as an OVERRIDE (§9.6): the system already
                auto-advances a running plan; this button is reframed as a "do it
                now" override (same idempotent dispatch path, INSERT-OR-IGNORE
                no-op if already dispatched). Function unchanged. */}
            <button
              type="button"
              data-testid="plan-advance-btn"
              disabled={advance.isPending}
              onClick={() => advance.mutate()}
              title="Manually dispatch ready nodes now (the system already advances automatically)"
              aria-label="Manually dispatch ready nodes now (the system already advances automatically)"
              className="rounded border border-border-strong bg-bg-subtle px-3 py-1.5 text-xs font-semibold text-text-secondary hover:bg-bg-base disabled:opacity-50"
            >
              ▸ Advance now
            </button>
            <button
              type="button"
              data-testid="plan-stop-btn"
              disabled={stop.isPending}
              onClick={() => stop.mutate()}
              className="rounded border border-border-strong bg-bg-subtle px-3 py-1.5 text-xs font-semibold text-text-secondary hover:bg-bg-base disabled:opacity-50"
            >
              ■ Stop (→ draft)
            </button>
          </>
        )}
        {plan.status === 'draft' && (
          <button
            type="button"
            data-testid="plan-start-btn"
            disabled={start.isPending}
            onClick={() => start.mutate()}
            className="rounded bg-accent px-3 py-1.5 text-xs font-semibold text-white hover:opacity-90 disabled:opacity-50"
          >
            ▸ Start
          </button>
        )}
      </div>
      <dl className="flex flex-wrap gap-x-4 gap-y-1 text-xs text-text-muted" data-testid="plan-detail-meta">
        <div className="flex items-center gap-1">
          <dt className="uppercase tracking-wide text-[0.625rem]">Progress</dt>
          <dd className="text-text-secondary" data-testid="plan-progress">
            {planProgressLabel(plan.progress)}
          </dd>
        </div>
        {plan.target_date && (
          <div className="flex items-center gap-1">
            <dt className="uppercase tracking-wide text-[0.625rem]">Target</dt>
            <dd className="text-text-secondary" title={plan.target_date}>
              {formatLocalTime(plan.target_date)}
            </dd>
          </div>
        )}
        <div className="flex items-center gap-1">
          <dt className="uppercase tracking-wide text-[0.625rem]">Creator</dt>
          <dd className="text-text-secondary" title={plan.creator_ref} data-testid="plan-creator">
            @{creatorLabel}
          </dd>
        </div>
      </dl>
      {(start.isError || stop.isError || advance.isError) && (
        <p className="text-xs text-danger" data-testid="plan-lifecycle-error">
          {((start.error ?? stop.error ?? advance.error) as Error).message}
        </p>
      )}
    </header>
  );
}

function TabButton({
  id,
  active,
  onSelect,
  children,
}: {
  id: Tab;
  active: boolean;
  onSelect: (t: Tab) => void;
  children: React.ReactNode;
}): React.ReactElement {
  return (
    <button
      type="button"
      role="tab"
      aria-selected={active}
      data-testid={`plan-tab-${id}`}
      onClick={() => onSelect(id)}
      className={`rounded-t-lg border border-b-0 px-3.5 py-1.5 text-xs font-semibold ${
        active
          ? 'border-border-base bg-bg-elevated text-text-primary shadow-[inset_0_2px_0_var(--color-accent,#3b82f6)]'
          : 'border-transparent bg-bg-subtle text-text-secondary hover:text-text-primary'
      }`}
    >
      {children}
    </button>
  );
}

// ── 6-state node palette (LOCKED, Tester2 computed-truth) ────────────────────
// SOLID X-100/X-800 literal pairs (theme-independent, both-mode AA ~6-9 contrast)
// + matching X-300 border + an inline SVG icon (NOT emoji). label+SVG+color is
// the triple-distinguisher (the 6 states are color-close, so never color alone).
interface NodeStateStyle {
  label: string;
  cls: string; // bg + text (the chip)
  border: string; // node box border (X-300)
  icon: React.ReactElement;
}

const ICON_CLS = 'h-2.5 w-2.5';

const NODE_STATE: Record<PlanNodeStatus, NodeStateStyle> = {
  blocked: {
    label: 'blocked',
    cls: 'bg-slate-100 text-slate-800',
    border: 'border-slate-300',
    // lock
    icon: (
      <svg viewBox="0 0 24 24" className={ICON_CLS} fill="none" stroke="currentColor" strokeWidth="2.5" aria-hidden="true">
        <rect x="5" y="11" width="14" height="9" rx="2" />
        <path d="M8 11V7a4 4 0 0 1 8 0v4" />
      </svg>
    ),
  },
  ready: {
    label: 'ready',
    cls: 'bg-blue-100 text-blue-800',
    border: 'border-blue-300',
    // circle (○)
    icon: (
      <svg viewBox="0 0 24 24" className={ICON_CLS} fill="none" stroke="currentColor" strokeWidth="2.5" aria-hidden="true">
        <circle cx="12" cy="12" r="8" />
      </svg>
    ),
  },
  dispatched: {
    label: 'dispatched',
    cls: 'bg-violet-100 text-violet-800',
    border: 'border-violet-300',
    // clock / hourglass
    icon: (
      <svg viewBox="0 0 24 24" className={ICON_CLS} fill="none" stroke="currentColor" strokeWidth="2.5" aria-hidden="true">
        <circle cx="12" cy="12" r="9" />
        <path d="M12 7v5l3 2" />
      </svg>
    ),
  },
  running: {
    label: 'running',
    cls: 'bg-amber-100 text-amber-800',
    border: 'border-amber-300',
    // play (▶)
    icon: (
      <svg viewBox="0 0 24 24" className={ICON_CLS} fill="currentColor" aria-hidden="true">
        <path d="M7 5v14l12-7z" />
      </svg>
    ),
  },
  done: {
    label: 'done',
    cls: 'bg-emerald-100 text-emerald-800',
    border: 'border-emerald-300',
    // check mark
    icon: (
      <svg viewBox="0 0 24 24" className={ICON_CLS} fill="none" stroke="currentColor" strokeWidth="3" aria-hidden="true">
        <path d="M5 13l4 4L19 7" />
      </svg>
    ),
  },
  failed: {
    label: 'failed',
    cls: 'bg-rose-100 text-rose-800',
    border: 'border-rose-300',
    // cross / x
    icon: (
      <svg viewBox="0 0 24 24" className={ICON_CLS} fill="none" stroke="currentColor" strokeWidth="3" aria-hidden="true">
        <path d="M6 6l12 12M18 6L6 18" />
      </svg>
    ),
  },
};

const NODE_STATE_ORDER: PlanNodeStatus[] = ['blocked', 'ready', 'dispatched', 'running', 'done', 'failed'];

function NodeStateChip({ status }: { status: PlanNodeStatus }): React.ReactElement {
  const s = NODE_STATE[status] ?? NODE_STATE.blocked;
  return (
    <span
      className={`inline-flex items-center gap-1 rounded px-1.5 py-0.5 text-[0.625rem] font-bold uppercase tracking-wide ${s.cls}`}
      data-testid="node-state-chip"
      data-node-status={status}
    >
      {s.icon}
      {s.label}
    </span>
  );
}

// assignee_ref → avatar (agent/human) + clean handle.
function AssigneeTag({ assigneeRef }: { assigneeRef: string }): React.ReactElement {
  const resolveName = useDisplayNameResolver();
  if (!assigneeRef) {
    return <span className="text-text-muted">—</span>;
  }
  const kind = refKind(assigneeRef) === 'agent' ? 'agent' : 'human';
  const resolved = resolveName(assigneeRef);
  const label = resolved === assigneeRef ? normalizeIdentityRef(assigneeRef) : resolved;
  return (
    <span className="inline-flex items-center gap-1.5 text-text-secondary" title={assigneeRef}>
      <Avatar name={label} kind={kind} size="sm" />
      <span className="truncate">{label}</span>
    </span>
  );
}

// ── DAG (the core) ──────────────────────────────────────────────────────────
// Layered left→right layout from node.depends_on:
//   level(n) = 0 if no (in-plan) deps else max(level(dep))+1   (longest-path)
//   x = level * COL_W;  y = even vertical spread within the level.
// Edges: SVG path from each dep node's RIGHT-mid → this node's LEFT-mid, with an
// arrow marker (upstream → downstream). node_status is DERIVED → display only.
const COL_W = 200;
const NODE_W = 168;
const NODE_H = 84;
const ROW_GAP = 28;
const PAD_X = 14;
const PAD_Y = 16;

interface Positioned {
  node: PlanNode;
  level: number;
  x: number;
  y: number;
}

// v2.9 Stage A5 — synthetic Start/End flow anchors (NOT real tasks): a Start
// node a column LEFT of all roots (edges → every level-0 root) and an End node
// a column RIGHT of the deepest level (edges ← every leaf, i.e. a node nothing
// else depends on). They give parallel/independent chains a clear left→right
// progression. They are layout/flow markers only: no node_status / 6-state
// chip, not dispatchable, not counted, not in the task list.
interface SyntheticAnchor {
  // center point of the anchor marker (used for both placement + edge endpoint)
  cx: number;
  cy: number;
  // the real-node anchor points this connects to (root left-mids for Start,
  // leaf right-mids for End)
  links: { taskId: string; x: number; y: number }[];
}

const SYNTH_R = 26; // synthetic marker radius (circle terminal)

function layoutDag(nodes: PlanNode[]): {
  positioned: Positioned[];
  width: number;
  height: number;
  start: SyntheticAnchor | null;
  end: SyntheticAnchor | null;
} {
  const byId = new Map(nodes.map((n) => [n.task_id, n]));
  // Only consider deps that are actually in this plan (defensive — a dangling
  // dep ref must not break level computation).
  const depsOf = (n: PlanNode) => n.depends_on.filter((d) => byId.has(d));

  // Longest-path level via memoized DFS (cycle-guarded; the DAG is acyclic by
  // contract, the guard just prevents a hang on bad data).
  const levelCache = new Map<string, number>();
  const inStack = new Set<string>();
  function level(id: string): number {
    if (levelCache.has(id)) return levelCache.get(id)!;
    if (inStack.has(id)) return 0; // cycle guard
    const n = byId.get(id);
    if (!n) return 0;
    inStack.add(id);
    const deps = depsOf(n);
    const lvl = deps.length === 0 ? 0 : Math.max(...deps.map((d) => level(d) + 1));
    inStack.delete(id);
    levelCache.set(id, lvl);
    return lvl;
  }

  // Group by level, preserving input order within a level.
  const byLevel = new Map<number, PlanNode[]>();
  let maxLevel = 0;
  for (const n of nodes) {
    const lvl = level(n.task_id);
    maxLevel = Math.max(maxLevel, lvl);
    const arr = byLevel.get(lvl) ?? [];
    arr.push(n);
    byLevel.set(lvl, arr);
  }

  // Reserve a left gutter column for the Start anchor when there are any nodes,
  // so real nodes are shifted right by one column (Start sits at x≈PAD_X, real
  // level-0 nodes at PAD_X + COL_W). Empty plan ⇒ no gutter, no anchors.
  const hasNodes = nodes.length > 0;
  const SYNTH_COL = COL_W; // width of each synthetic gutter column
  const baseX = PAD_X + (hasNodes ? SYNTH_COL : 0);

  // Even vertical spread within each level.
  const positioned: Positioned[] = [];
  let maxRows = 0;
  for (const [lvl, group] of byLevel) {
    maxRows = Math.max(maxRows, group.length);
    group.forEach((node, row) => {
      positioned.push({
        node,
        level: lvl,
        x: baseX + lvl * COL_W,
        y: PAD_Y + row * (NODE_H + ROW_GAP),
      });
    });
  }

  // Roots = real level-0 nodes (no in-plan deps). Leaves = nodes that no other
  // in-plan node depends on. Start → every root; every leaf → End.
  const dependedOn = new Set<string>();
  for (const n of nodes) for (const d of depsOf(n)) dependedOn.add(d);

  const contentHeight = Math.max(PAD_Y * 2 + maxRows * (NODE_H + ROW_GAP) - ROW_GAP, 200);
  const midY = contentHeight / 2;

  let start: SyntheticAnchor | null = null;
  let end: SyntheticAnchor | null = null;
  if (hasNodes) {
    const roots = positioned.filter((p) => p.level === 0);
    const leaves = positioned.filter((p) => !dependedOn.has(p.node.task_id));
    start = {
      cx: PAD_X + SYNTH_R,
      cy: midY,
      // edge endpoint = root node's LEFT-mid
      links: roots.map((p) => ({ taskId: p.node.task_id, x: p.x, y: p.y + NODE_H / 2 })),
    };
    const endCx = baseX + (maxLevel + 1) * COL_W + SYNTH_R;
    end = {
      cx: endCx,
      cy: midY,
      // edge endpoint = leaf node's RIGHT-mid
      links: leaves.map((p) => ({ taskId: p.node.task_id, x: p.x + NODE_W, y: p.y + NODE_H / 2 })),
    };
  }

  // Width spans from PAD_X (Start) to End marker (when present), else the real
  // layout extent.
  const realRight = baseX + maxLevel * COL_W + NODE_W;
  const width = hasNodes
    ? (end ? end.cx + SYNTH_R + PAD_X : realRight + PAD_X)
    : PAD_X * 2 + (maxLevel + 1) * COL_W - (COL_W - NODE_W);
  const height = contentHeight;
  return { positioned, width, height, start, end };
}

// Synthetic Start/End flow anchor — a distinct, non-task marker. No node_status
// / 6-state chip, no assignee, not clickable, not counted. Solid theme tokens
// (both-mode AA), plain text "Start"/"End" (no emoji). Positioned by its center
// (cx,cy) so it lines up with its flow edges.
function SyntheticAnchorMarker({
  kind,
  anchor,
}: {
  kind: 'start' | 'end';
  anchor: SyntheticAnchor;
}): React.ReactElement {
  const label = kind === 'start' ? 'Start' : 'End';
  return (
    <div
      className="absolute flex items-center justify-center rounded-full border-[1.5px] border-border-strong bg-bg-elevated text-[0.625rem] font-semibold uppercase tracking-wide text-text-secondary shadow-1"
      style={{
        left: anchor.cx - SYNTH_R,
        top: anchor.cy - SYNTH_R,
        width: SYNTH_R * 2,
        height: SYNTH_R * 2,
      }}
      data-testid={`plan-dag-synthetic-${kind}`}
      aria-hidden="true"
    >
      {label}
    </div>
  );
}

function PlanDag({ projectId, plan }: { projectId: string; plan: Plan }): React.ReactElement {
  const nodes = plan.nodes ?? [];
  const isDraft = plan.status === 'draft';

  const { positioned, width, height, start, end } = useMemo(() => layoutDag(nodes), [nodes]);
  const posById = useMemo(
    () => new Map(positioned.map((p) => [p.node.task_id, p])),
    [positioned],
  );

  // Edges: dep (upstream) → node (downstream). Path from dep right-mid to node
  // left-mid; a horizontal-ease cubic for a clean orthogonal-ish curve.
  const edges = useMemo(() => {
    const out: { key: string; d: string }[] = [];
    for (const p of positioned) {
      for (const depId of p.node.depends_on) {
        const dep = posById.get(depId);
        if (!dep) continue;
        const x1 = dep.x + NODE_W;
        const y1 = dep.y + NODE_H / 2;
        const x2 = p.x;
        const y2 = p.y + NODE_H / 2;
        const midX = (x1 + x2) / 2;
        out.push({
          key: `${depId}->${p.node.task_id}`,
          d: `M${x1},${y1} C${midX},${y1} ${midX},${y2} ${x2},${y2}`,
        });
      }
    }
    return out;
  }, [positioned, posById]);

  // v2.9 A5: synthetic flow edges — Start anchor → each root's left-mid, and
  // each leaf's right-mid → End anchor. Same cubic shape as real edges; kept on
  // a SEPARATE testid (`plan-dag-synthetic-edge`) so real-edge assertions/counts
  // are unaffected. A dashed/lighter stroke reads as a flow anchor, not a dep.
  const synthEdges = useMemo(() => {
    const out: { key: string; d: string }[] = [];
    if (start) {
      for (const l of start.links) {
        const midX = (start.cx + l.x) / 2;
        out.push({
          key: `start->${l.taskId}`,
          d: `M${start.cx},${start.cy} C${midX},${start.cy} ${midX},${l.y} ${l.x},${l.y}`,
        });
      }
    }
    if (end) {
      for (const l of end.links) {
        const midX = (l.x + end.cx) / 2;
        out.push({
          key: `${l.taskId}->end`,
          d: `M${l.x},${l.y} C${midX},${l.y} ${midX},${end.cy} ${end.cx},${end.cy}`,
        });
      }
    }
    return out;
  }, [start, end]);

  return (
    <div data-testid="plan-dag">
      {nodes.length === 0 ? (
        <p className="py-10 text-center text-xs text-text-muted" data-testid="plan-dag-empty">
          No tasks in this plan yet. Add tasks from the Work Board.
        </p>
      ) : (
        <div
          className="relative overflow-auto rounded-lg border border-border-base bg-bg-subtle"
          data-testid="plan-dag-canvas"
          style={{ maxHeight: 480 }}
        >
          <div className="relative" style={{ width, height }}>
            {/* Edges (z-0, behind nodes). */}
            <svg
              className="absolute left-0 top-0"
              width={width}
              height={height}
              data-testid="plan-dag-svg"
              aria-hidden="true"
            >
              <defs>
                <marker
                  id="plan-dag-arrow"
                  viewBox="0 0 10 10"
                  refX="8"
                  refY="5"
                  markerWidth="7"
                  markerHeight="7"
                  orient="auto-start-reverse"
                >
                  <path d="M0,0 L10,5 L0,10 z" className="fill-border-strong" />
                </marker>
              </defs>
              <g fill="none" className="stroke-border-strong" strokeWidth="1.6" markerEnd="url(#plan-dag-arrow)">
                {edges.map((e) => (
                  <path key={e.key} d={e.d} data-testid="plan-dag-edge" data-edge={e.key} />
                ))}
              </g>
              {/* Synthetic flow edges (Start→roots, leaves→End): lighter +
                  dashed so they read as flow anchors, not real dependencies. */}
              <g
                fill="none"
                className="stroke-border-base"
                strokeWidth="1.4"
                strokeDasharray="4 3"
                markerEnd="url(#plan-dag-arrow)"
              >
                {synthEdges.map((e) => (
                  <path
                    key={e.key}
                    d={e.d}
                    data-testid="plan-dag-synthetic-edge"
                    data-edge={e.key}
                  />
                ))}
              </g>
            </svg>
            {/* Nodes (z-10). */}
            {positioned.map((p) => {
              const s = NODE_STATE[p.node.node_status] ?? NODE_STATE.blocked;
              return (
                <div
                  key={p.node.task_id}
                  className={`absolute rounded-lg border-[1.5px] bg-bg-elevated p-2 shadow-1 ${s.border}`}
                  style={{ left: p.x, top: p.y, width: NODE_W }}
                  data-testid="plan-dag-node"
                  data-task-id={p.node.task_id}
                  data-level={p.level}
                >
                  <div className="mb-1.5 truncate text-xs font-semibold text-text-primary" title={p.node.title}>
                    {p.node.title || `#${idHandle(p.node.task_id)}`}
                  </div>
                  <div className="flex items-center justify-between gap-1.5">
                    <span className="min-w-0 text-[0.6875rem]">
                      <AssigneeTag assigneeRef={p.node.assignee_ref} />
                    </span>
                    <NodeStateChip status={p.node.node_status} />
                  </div>
                </div>
              );
            })}
            {/* Synthetic Start/End anchors (z-10) — distinct flow markers, NOT
                tasks: no node_status / 6-state chip, no assignee, not
                clickable/dispatchable, not in any count. Rendered as a labeled
                circular terminal. */}
            {start && <SyntheticAnchorMarker kind="start" anchor={start} />}
            {end && <SyntheticAnchorMarker kind="end" anchor={end} />}
          </div>
        </div>
      )}

      {/* Legend (all 6 states) — the lifecycle controls live in the header
          (Start / Stop / Advance), rendered exactly once each there. */}
      <div className="mt-3 flex flex-wrap items-center gap-1.5" data-testid="plan-dag-legend">
        {NODE_STATE_ORDER.map((st) => (
          <NodeStateChip key={st} status={st} />
        ))}
      </div>

      {/* node_status is DERIVED (§9.2) and shown, not edited. In DRAFT the
          dependency STRUCTURE is editable below; once running/done the graph is
          DISPLAY-ONLY (the backend rejects edits with ErrPlanNotDraft). */}
      <p className="mt-2 text-[0.6875rem] text-text-muted" data-testid="plan-dag-note">
        Node status is derived (= f(task status, all upstream done, dispatch record)) and shown, not edited.{' '}
        {isDraft ? (
          <>
            This plan is a draft, so its dependencies are editable below — add or remove edges to shape the DAG.
          </>
        ) : (
          <>
            Display-only graph: a running plan auto-advances (the system dispatches ready nodes as upstream
            tasks complete) and "Advance now" is a manual override. Dependencies can only be edited while the
            plan is a draft.
          </>
        )}
      </p>

      {/* Stage A1: the draft-only dependency-edge editor (add via labeled
          selects, remove via a list). Gated to draft — running/done = display
          only, matching the backend draft-gate (§9.4). */}
      {isDraft && <PlanDagEditor projectId={projectId} plan={plan} />}
    </div>
  );
}

// ── Draft-only dependency-edge editor (v2.9 Stage A1) ────────────────────────
// from/to semantics (verified against the backend, plan_view.go + plan_flow.go):
// a node's `depends_on` lists `edge.ToTaskID` where `edge.FromTaskID == node`,
// i.e. AddPlanDependency(from, to) means **from depends_on to** (`to` is the
// upstream dependency that completes first; `from` is the downstream dependent).
// So an edge "B depends on A" → { from_task_id: B, to_task_id: A }.
//
// #218: add/remove failures map the backend Go error message (all surface as a
// 400 invalid_request, distinguished by the message text) to a FRIENDLY string;
// the raw error is never shown.
function friendlyDependencyError(error: unknown): string {
  const raw = error instanceof Error ? error.message : String(error ?? '');
  const lower = raw.toLowerCase();
  if (lower.includes('itself') || lower.includes('self')) {
    return "A task can't depend on itself.";
  }
  if (lower.includes('cycle')) {
    return 'That would create a cycle in the plan.';
  }
  if (lower.includes('draft')) {
    return 'Dependencies can only be edited while the plan is a draft.';
  }
  return "Couldn't update the plan's dependencies. Please try again.";
}

interface ExistingEdge {
  from: PlanNode; // dependent (downstream) — has the dep in depends_on
  to: PlanNode; // dependency (upstream) — completes first
}

function PlanDagEditor({ projectId, plan }: { projectId: string; plan: Plan }): React.ReactElement {
  const nodes = plan.nodes ?? [];
  const addDep = useAddDependency(projectId, plan.id);
  const removeDep = useRemoveDependency(projectId, plan.id);

  // Add-form selects: `from` = the dependent task ("this task"); `to` = the
  // task it depends on (upstream). Matches the backend body exactly.
  const [fromId, setFromId] = useState('');
  const [toId, setToId] = useState('');

  const byId = useMemo(() => new Map(nodes.map((n) => [n.task_id, n])), [nodes]);
  const titleOf = (n: PlanNode) => n.title || `#${idHandle(n.task_id)}`;

  // Existing edges derived from nodes' depends_on (each = "from depends_on to").
  const edges = useMemo<ExistingEdge[]>(() => {
    const out: ExistingEdge[] = [];
    for (const n of nodes) {
      for (const depId of n.depends_on) {
        const to = byId.get(depId);
        if (!to) continue; // skip dangling refs
        out.push({ from: n, to });
      }
    }
    return out;
  }, [nodes, byId]);

  const canAdd = fromId !== '' && toId !== '' && fromId !== toId && !addDep.isPending;

  function onAdd(e: React.FormEvent) {
    e.preventDefault();
    if (!canAdd) return;
    addDep.mutate(
      { from_task_id: fromId, to_task_id: toId },
      {
        onSuccess: () => {
          setFromId('');
          setToId('');
        },
      },
    );
  }

  const mutationError = addDep.isError ? addDep.error : removeDep.isError ? removeDep.error : null;

  return (
    <div className="mt-3 rounded-lg border border-border-base bg-bg-subtle p-3" data-testid="plan-dag-editor">
      <h3 className="mb-2 text-xs font-semibold text-text-primary">Edit dependencies (draft)</h3>

      {nodes.length < 2 ? (
        <p className="text-[0.6875rem] text-text-secondary" data-testid="plan-edge-add-empty">
          Add at least two tasks to this plan (from the Work Board) to create a dependency.
        </p>
      ) : (
        <form className="flex flex-wrap items-end gap-2" onSubmit={onAdd} data-testid="plan-edge-add">
          <label className="flex flex-col gap-0.5 text-[0.625rem] uppercase tracking-wide text-text-secondary">
            Task
            <select
              className="rounded border border-border-strong bg-bg-elevated px-2 py-1 text-xs normal-case text-text-primary"
              data-testid="plan-edge-add-from"
              aria-label="Task that depends on another"
              value={fromId}
              onChange={(e) => setFromId(e.target.value)}
            >
              <option value="">Select a task…</option>
              {nodes.map((n) => (
                <option key={n.task_id} value={n.task_id}>
                  {titleOf(n)}
                </option>
              ))}
            </select>
          </label>
          <span className="pb-1.5 text-xs font-medium text-text-secondary">depends on</span>
          <label className="flex flex-col gap-0.5 text-[0.625rem] uppercase tracking-wide text-text-secondary">
            Depends on
            <select
              className="rounded border border-border-strong bg-bg-elevated px-2 py-1 text-xs normal-case text-text-primary"
              data-testid="plan-edge-add-to"
              aria-label="Upstream task it depends on"
              value={toId}
              onChange={(e) => setToId(e.target.value)}
            >
              <option value="">Select a task…</option>
              {nodes.map((n) => (
                <option key={n.task_id} value={n.task_id}>
                  {titleOf(n)}
                </option>
              ))}
            </select>
          </label>
          <button
            type="submit"
            data-testid="plan-edge-add-btn"
            disabled={!canAdd}
            className="rounded bg-accent px-3 py-1.5 text-xs font-semibold text-white hover:opacity-90 disabled:opacity-50"
          >
            Add dependency
          </button>
        </form>
      )}

      {/* #218 friendly error (never the raw API message). */}
      {mutationError && (
        <p className="mt-2 text-xs font-medium text-danger" role="alert" data-testid="plan-edge-error">
          {friendlyDependencyError(mutationError)}
        </p>
      )}

      {/* Existing edges → remove list (the a11y-accessible primary; a clickable
          SVG edge is a poor target). Each row = "<from> depends on <to>". */}
      <div className="mt-3">
        <h4 className="mb-1 text-[0.625rem] font-semibold uppercase tracking-wide text-text-secondary">
          Dependencies
        </h4>
        {edges.length === 0 ? (
          <p className="text-[0.6875rem] text-text-secondary" data-testid="plan-edge-list-empty">
            No dependencies yet. Add one above to order the tasks.
          </p>
        ) : (
          <ul className="space-y-1" data-testid="plan-edge-list">
            {edges.map((edge) => {
              const key = `${edge.from.task_id}->${edge.to.task_id}`;
              return (
                <li
                  key={key}
                  className="flex items-center justify-between gap-2 rounded border border-border-base bg-bg-elevated px-2 py-1"
                  data-testid="plan-edge-remove"
                  data-edge={key}
                >
                  <span className="min-w-0 truncate text-xs text-text-primary">
                    <span className="font-medium">{titleOf(edge.from)}</span>
                    <span className="text-text-secondary"> depends on </span>
                    <span className="font-medium">{titleOf(edge.to)}</span>
                  </span>
                  <button
                    type="button"
                    data-testid="plan-edge-remove-btn"
                    disabled={removeDep.isPending}
                    onClick={() =>
                      removeDep.mutate({ from_task_id: edge.from.task_id, to_task_id: edge.to.task_id })
                    }
                    aria-label={`Remove dependency: ${titleOf(edge.from)} depends on ${titleOf(edge.to)}`}
                    title="Remove this dependency"
                    className="shrink-0 rounded border border-border-strong bg-bg-subtle px-2 py-0.5 text-[0.6875rem] font-semibold text-text-secondary hover:bg-bg-base disabled:opacity-50"
                  >
                    Remove
                  </button>
                </li>
              );
            })}
          </ul>
        )}
      </div>
    </div>
  );
}

// ── Task list tab ────────────────────────────────────────────────────────────
// §9.4: removing a task from a Plan is a PLANNING action — only a DRAFT plan
// exposes a per-row "Remove" control (consistent with add-to-plan / the A1 edge
// editor). A running/done plan renders the rows read-only (no Remove column).
function PlanTaskList({ projectId, plan }: { projectId: string; plan: Plan }): React.ReactElement {
  const nodes = plan.nodes ?? [];
  const canRemove = plan.status === 'draft';
  return (
    <div data-testid="plan-task-list">
      {nodes.length === 0 ? (
        <p className="py-10 text-center text-xs text-text-muted" data-testid="plan-task-list-empty">
          No tasks in this plan yet.
        </p>
      ) : (
        <div className="overflow-x-auto">
          <table className="w-full text-left text-xs" data-testid="plan-task-list-table">
            <thead>
              <tr className="border-b border-border-base text-[0.625rem] uppercase tracking-wide text-text-muted">
                <th className="py-1.5 pr-3 font-medium">Title</th>
                <th className="py-1.5 pr-3 font-medium">Assignee</th>
                <th className="py-1.5 pr-3 font-medium">Task status</th>
                <th className="py-1.5 font-medium">Node status</th>
                {canRemove && <th className="py-1.5 pl-3 text-right font-medium">Action</th>}
              </tr>
            </thead>
            <tbody className="divide-y divide-border-base">
              {nodes.map((n) => (
                <PlanTaskRow
                  key={n.task_id}
                  projectId={projectId}
                  planId={plan.id}
                  node={n}
                  canRemove={canRemove}
                />
              ))}
            </tbody>
          </table>
        </div>
      )}
    </div>
  );
}

// PlanTaskRow — one task-list row. When the plan is draft, the trailing cell
// holds a "Remove" button → useRemoveTaskFromPlan(task_id) (task returns to the
// Backlog on success via query invalidation). #218: a remove failure surfaces a
// friendly inline message in the row, never a raw API error.
function PlanTaskRow({
  projectId,
  planId,
  node,
  canRemove,
}: {
  projectId: string;
  planId: string;
  node: PlanNode;
  canRemove: boolean;
}): React.ReactElement {
  const remove = useRemoveTaskFromPlan(projectId, planId);
  return (
    <tr data-testid="plan-task-row" data-task-id={node.task_id}>
      <td className="max-w-[18rem] truncate py-1.5 pr-3 text-text-primary" title={node.title}>
        {node.title || `#${idHandle(node.task_id)}`}
        {remove.isError && (
          <span
            className="mt-0.5 block text-[0.6875rem] font-normal text-danger"
            role="alert"
            data-testid={`plan-task-remove-error-${node.task_id}`}
          >
            Couldn't remove this task from the plan. Please try again.
          </span>
        )}
      </td>
      <td className="py-1.5 pr-3">
        <AssigneeTag assigneeRef={node.assignee_ref} />
      </td>
      <td className="py-1.5 pr-3">
        <StatusChip status={node.task_status} />
      </td>
      <td className="py-1.5">
        <NodeStateChip status={node.node_status} />
      </td>
      {canRemove && (
        <td className="py-1.5 pl-3 text-right">
          <button
            type="button"
            className="rounded border border-border-strong bg-bg-subtle px-2 py-0.5 text-[0.6875rem] font-semibold text-text-secondary hover:bg-bg-base hover:text-text-primary disabled:opacity-50"
            disabled={remove.isPending}
            aria-label={`Remove ${node.title || idHandle(node.task_id)} from plan`}
            title="Remove from plan (back to backlog)"
            data-testid={`plan-task-remove-${node.task_id}`}
            onClick={() => remove.mutate(node.task_id)}
          >
            Remove
          </button>
        </td>
      )}
    </tr>
  );
}

// ── Plan conversation side (REUSE ConversationView) ──────────────────────────
// This is where the orchestrator @-dispatches + discussion appear (bound post
// #266). Render the Plan's conversation by its conversation_id. Empty
// conversation_id → friendly "initializing" state (don't crash).
function PlanConversationSide({ conversationId }: { conversationId: string }): React.ReactElement {
  const conv = useConversation(conversationId || undefined);

  return (
    <SenderSidebarProvider>
      <section className="flex min-h-0 flex-col" data-testid="plan-conversation">
        <div className="mb-2 flex items-center gap-2 text-sm font-semibold text-text-primary">
          Plan conversation
          <span className="rounded border border-border-base px-1.5 py-0.5 text-[0.625rem] font-normal uppercase tracking-wide text-text-muted">
            chat
          </span>
        </div>

        {!conversationId ? (
          <p
            className="rounded border border-dashed border-border-base p-4 text-xs italic text-text-muted"
            data-testid="plan-conversation-initializing"
          >
            Conversation initializing — the plan's chat is being set up.
          </p>
        ) : (
          <div
            className="flex min-h-[20rem] flex-col overflow-hidden rounded border border-border-base"
            data-testid="plan-conversation-body"
          >
            <ConversationView surface="task-thread" conversationId={conversationId} />
            {conv.isError && (
              <p className="p-2 text-[0.6875rem] text-text-muted">
                Couldn't refresh conversation details.
              </p>
            )}
          </div>
        )}
        <p className="mt-2 text-[0.6875rem] text-text-muted">
          Dispatch = @assignee in this conversation (notify human / wake agent); also the place to discuss this plan.
        </p>
      </section>
    </SenderSidebarProvider>
  );
}
