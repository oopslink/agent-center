import React, { useMemo, useState } from 'react';
import { useParams } from 'react-router-dom';
import { OrgLink } from '@/OrgContext';
import { useProject } from '@/api/projects';
import {
  usePlan,
  useStartPlan,
  useStopPlan,
  useAdvancePlan,
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
// or edit it. This execution view renders the derived DAG (display-only) +
// header lifecycle controls (Start / Stop / Advance, each once). Dependency-edge
// editing is a separate PLANNING concern (planning/execution are separated) and
// is NOT part of this page — the backend AddPlanDependency/RemovePlanDependency
// are draft-gated, but the edge editor is a tracked P1 fast-follow, built
// elsewhere.

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
              <PlanDag plan={p} />
            ) : (
              <PlanTaskList plan={p} />
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

function layoutDag(nodes: PlanNode[]): { positioned: Positioned[]; width: number; height: number } {
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

  // Even vertical spread within each level.
  const positioned: Positioned[] = [];
  let maxRows = 0;
  for (const [lvl, group] of byLevel) {
    maxRows = Math.max(maxRows, group.length);
    group.forEach((node, row) => {
      positioned.push({
        node,
        level: lvl,
        x: PAD_X + lvl * COL_W,
        y: PAD_Y + row * (NODE_H + ROW_GAP),
      });
    });
  }

  const width = PAD_X * 2 + (maxLevel + 1) * COL_W - (COL_W - NODE_W);
  const height = Math.max(PAD_Y * 2 + maxRows * (NODE_H + ROW_GAP) - ROW_GAP, 200);
  return { positioned, width, height };
}

function PlanDag({ plan }: { plan: Plan }): React.ReactElement {
  const nodes = plan.nodes ?? [];

  const { positioned, width, height } = useMemo(() => layoutDag(nodes), [nodes]);
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

      {/* This execution-page DAG is DISPLAY-ONLY. node_status is DERIVED (§9.2)
          and shown, not edited; the graph shows the dependency structure; and
          Advance (header) dispatches ready nodes. Dependency-edge editing is a
          separate planning concern (planning/execution are separated) — not
          available here. */}
      <p className="mt-2 text-[0.6875rem] text-text-muted" data-testid="plan-dag-note">
        Display-only graph: node status is derived (= f(task status, all upstream done, dispatch record))
        and shown, not edited. A running plan auto-advances (the system dispatches ready nodes as upstream
        tasks complete); "Advance now" is a manual override. Dependency editing is a planning concern and
        isn't done on this execution view.
      </p>
    </div>
  );
}

// ── Task list tab ────────────────────────────────────────────────────────────
function PlanTaskList({ plan }: { plan: Plan }): React.ReactElement {
  const nodes = plan.nodes ?? [];
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
              </tr>
            </thead>
            <tbody className="divide-y divide-border-base">
              {nodes.map((n) => (
                <tr key={n.task_id} data-testid="plan-task-row" data-task-id={n.task_id}>
                  <td className="max-w-[18rem] truncate py-1.5 pr-3 text-text-primary" title={n.title}>
                    {n.title || `#${idHandle(n.task_id)}`}
                  </td>
                  <td className="py-1.5 pr-3">
                    <AssigneeTag assigneeRef={n.assignee_ref} />
                  </td>
                  <td className="py-1.5 pr-3">
                    <StatusChip status={n.task_status} />
                  </td>
                  <td className="py-1.5">
                    <NodeStateChip status={n.node_status} />
                  </td>
                </tr>
              ))}
            </tbody>
          </table>
        </div>
      )}
    </div>
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
