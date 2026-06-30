import type React from 'react';
import type { Agent, AgentLifecycle, Availability } from '@/api/types';

// Shared status chips for the Agent BC surface (v2.7 #101). Reused by the
// Agents list + AgentDetail header so the colour mapping stays in one place.

const AVAILABILITY_CLASS: Record<Availability, string> = {
  available: 'bg-success/10 text-success',
  busy: 'bg-warning/10 text-warning',
  unavailable: 'bg-bg-subtle text-text-muted',
};

export function AvailabilityBadge({
  availability,
}: {
  availability: Availability;
}): React.ReactElement {
  return (
    <span
      className={[
        'rounded px-2 py-0.5 text-[0.6875rem] uppercase tracking-wide',
        AVAILABILITY_CLASS[availability],
      ].join(' ')}
      data-testid="agent-availability-badge"
      data-availability={availability}
      title="Availability — whether this agent will accept new work"
    >
      {availability}
    </span>
  );
}

// v2.8.1 list-enrich: provider badges for the Agents list (CLI + model). Reuses
// the same neutral chip style as Lifecycle/Availability above (solid bg-subtle
// token + text-secondary, AA in BOTH modes — NOT an alpha-tint-on-token which
// renders transparent, the recurring trap). Text labels, never color-only. An
// absent/blank value is omitted by the caller (this just renders the chip).
export function ProviderBadge({
  label,
  testId,
}: {
  label: string;
  testId?: string;
}): React.ReactElement {
  return (
    <span
      className="rounded bg-bg-subtle px-2 py-0.5 text-[0.6875rem] font-medium tracking-wide text-text-secondary"
      data-testid={testId}
    >
      {label}
    </span>
  );
}

// ── T235: derived activity status (Idle / Busy) ────────────────────────────
// Lifecycle + Availability describe the agent's MACHINE state; they don't tell
// an operator whether a *running* agent is actively working or sitting idle.
// T235 adds a derived activity status from `last_activity_at`: a running agent
// with no activity for ≥5 min (or none ever recorded) reads as "idle" — free to
// take new work; one with recent activity reads as "busy". Only running agents
// have an activity status (others are fully described by lifecycle → null).
export type AgentActivityStatus = 'idle' | 'busy';

// The idle threshold: ≥5 min since the last activity event ⇒ idle (per T235 §2).
export const AGENT_IDLE_MS = 5 * 60 * 1000;

// deriveAgentActivity is a PURE compute (now is injectable for tests, mirroring
// formatStatusDuration). A non-running agent has no activity status; a running
// agent with a missing/unparseable last_activity_at is treated as idle (it has
// produced no activity to count as busy).
export function deriveAgentActivity(
  agent: Pick<Agent, 'lifecycle' | 'last_activity_at'>,
  now: number = Date.now(),
): AgentActivityStatus | null {
  if (agent.lifecycle !== 'running') return null;
  if (!agent.last_activity_at) return 'idle';
  const last = new Date(agent.last_activity_at).getTime();
  if (Number.isNaN(last)) return 'idle';
  return now - last >= AGENT_IDLE_MS ? 'idle' : 'busy';
}

// Idle is GREEN (per T235 §2: "展示为 idle 状态（绿色）") — the success token reads
// as "available for work". Busy uses the neutral brand tint to stay distinct
// from Availability's amber "busy" chip (a different axis: machine availability
// vs. live activity). Text label carries the meaning; color is supplementary
// (never color-only).
const ACTIVITY_CLASS: Record<AgentActivityStatus, string> = {
  idle: 'bg-success/10 text-success',
  busy: 'bg-brand/10 text-brand',
};

// T320: the activity axis is labeled "Active"/"Idle", NOT "Busy"/"Idle" — the old
// "busy" label collided word-for-word with Availability's "busy" chip (a running,
// availability=busy, recently-active agent read as a baffling "BUSY  BUSY"). The
// vocabulary now disambiguates the two axes: Availability = Available/Busy
// (schedulable state), Activity = Active/Idle (recently doing work).
const ACTIVITY_LABEL: Record<AgentActivityStatus, string> = {
  idle: 'Idle',
  busy: 'Active',
};

export function ActivityBadge({
  status,
}: {
  status: AgentActivityStatus;
}): React.ReactElement {
  return (
    <span
      className={[
        'rounded px-2 py-0.5 text-[0.6875rem] uppercase tracking-wide',
        ACTIVITY_CLASS[status],
      ].join(' ')}
      data-testid="agent-activity-status-badge"
      data-activity-status={status}
      title="Activity — whether this running agent is actively working or idle"
    >
      {ACTIVITY_LABEL[status]}
    </span>
  );
}

// ── T322: single unified agent status (one dot + word) ─────────────────────
// @oopslink: three look-alike chips (lifecycle / availability / activity) were
// confusing — collapse them into ONE status pill derived by priority. Color
// principle: ALIVE states are a warm/bright family (green→blue→amber→orange, a
// "can-accept-work → increasingly occupied" gradient) and DEAD/broken states are
// clearly set apart (gray / red). "Can accept work" (Idle) is the brightest green,
// adjacent to the alive family. The full three-axis breakdown stays in the
// tooltip (and the AgentDetail page), so collapsing loses no information.
export type AgentStatus = 'error' | 'stopped' | 'unavailable' | 'busy' | 'working' | 'idle';

// deriveAgentStatus folds lifecycle + availability + activity into one status by
// priority: dead/broken first (error, stopped/archived), then — for a running
// agent — availability (unavailable / busy), then the derived activity splits an
// available agent into working (recently active) vs idle (quiet, ready). `now` is
// injectable for deterministic tests (mirrors deriveAgentActivity).
export function deriveAgentStatus(
  agent: Pick<Agent, 'lifecycle' | 'availability' | 'last_activity_at'>,
  now: number = Date.now(),
): AgentStatus {
  if (agent.lifecycle === 'error') return 'error';
  if (agent.lifecycle !== 'running') return 'stopped'; // stopped / archived (down)
  if (agent.availability === 'unavailable') return 'unavailable';
  if (agent.availability === 'busy') return 'busy';
  // available → split by live activity (busy activity ⇒ actively working).
  return deriveAgentActivity(agent, now) === 'busy' ? 'working' : 'idle';
}

const STATUS_META: Record<AgentStatus, { label: string; dot: string }> = {
  idle: { label: 'Idle', dot: 'bg-status-green-solid' },
  working: { label: 'Working', dot: 'bg-status-blue-solid' },
  busy: { label: 'Busy', dot: 'bg-status-amber-solid' },
  unavailable: { label: 'Unavailable', dot: 'bg-status-orange-solid' },
  stopped: { label: 'Stopped', dot: 'bg-text-muted' },
  error: { label: 'Error', dot: 'bg-danger' },
};

// agentStatusTooltip — the full three-axis breakdown, surfaced on hover so the
// single pill stays scannable without hiding detail.
function agentStatusTooltip(
  agent: Pick<Agent, 'lifecycle' | 'availability' | 'last_activity_at'>,
  now: number,
): string {
  if (agent.lifecycle !== 'running') return `Lifecycle: ${agent.lifecycle}`;
  const activity = deriveAgentActivity(agent, now);
  return `Lifecycle: running · Availability: ${agent.availability} · Activity: ${
    activity === 'busy' ? 'active' : 'idle'
  }`;
}

// AgentStatusBadge — the single status indicator: a colored dot (the alive/dead
// signal) + a neutral, always-AA-readable word. The dot carries color; the word
// disambiguates same-warmth states (Busy vs Unavailable). Tooltip = full breakdown.
export function AgentStatusBadge({
  agent,
  now = Date.now(),
}: {
  agent: Pick<Agent, 'lifecycle' | 'availability' | 'last_activity_at'>;
  now?: number;
}): React.ReactElement {
  const status = deriveAgentStatus(agent, now);
  const meta = STATUS_META[status];
  return (
    <span
      className="inline-flex items-center gap-1.5 text-[0.6875rem] text-text-secondary"
      data-testid="agent-status-badge"
      data-agent-status={status}
      title={agentStatusTooltip(agent, now)}
    >
      <span className={['h-2 w-2 shrink-0 rounded-full', meta.dot].join(' ')} aria-hidden="true" />
      <span>{meta.label}</span>
    </span>
  );
}

// ── T342: agent load (pressure) ────────────────────────────────────────────
// @oopslink: show how loaded an agent is — load = doing / (doing + pending),
// where doing = running tasks and pending = open (queued) tasks assigned to it,
// ∈ [0,1]. Color encodes pressure: green (low) → amber (mid) → red (high); an
// agent with no active task reads neutral "—". The fraction shown is doing/total
// (the metric's numerator/denominator) so the raw numbers are always visible.
export type AgentLoadLevel = 'none' | 'low' | 'medium' | 'high';

export interface AgentLoadInfo {
  running: number;
  pending: number;
  total: number;
  load: number; // [0,1]
  level: AgentLoadLevel;
}

export function deriveAgentLoad(
  agent: Pick<Agent, 'running_tasks' | 'pending_tasks' | 'task_load'>,
): AgentLoadInfo {
  const running = agent.running_tasks ?? 0;
  const pending = agent.pending_tasks ?? 0;
  const total = running + pending;
  // Prefer the server-computed value; fall back to a local compute (older API).
  const load = total > 0 ? (agent.task_load ?? running / total) : 0;
  let level: AgentLoadLevel = 'none';
  if (total > 0) level = load >= 0.67 ? 'high' : load >= 0.34 ? 'medium' : 'low';
  return { running, pending, total, load, level };
}

// T342c (@oopslink): the dot+fraction read as cryptic — render an explicit
// labeled TAG ("load: 0.7") instead. Pressure is carried by the TEXT color
// (high → danger, mid → warning, low → success, none → muted) on a neutral chip
// (solid bg-subtle, never an alpha tint — the transparent-token trap noted above).
const LOAD_TEXT: Record<AgentLoadLevel, string> = {
  none: 'text-text-muted',
  low: 'text-success',
  medium: 'text-warning',
  high: 'text-danger',
};

export function AgentLoadBadge({
  agent,
}: {
  agent: Pick<Agent, 'running_tasks' | 'pending_tasks' | 'task_load'>;
}): React.ReactElement {
  const info = deriveAgentLoad(agent);
  const value = info.load.toFixed(1); // 0.0 … 1.0 (one decimal, matches the spec)
  const title =
    info.total === 0
      ? 'Load — no active tasks (doing ÷ (doing + pending))'
      : `Load = doing ${info.running} ÷ (doing + pending ${info.total}) = ${info.load.toFixed(2)}`;
  return (
    <span
      className={[
        'inline-flex items-center whitespace-nowrap rounded bg-bg-subtle px-1.5 py-0.5 text-[0.6875rem] font-medium tabular-nums',
        LOAD_TEXT[info.level],
      ].join(' ')}
      data-testid="agent-load-badge"
      data-load-level={info.level}
      data-load={info.load.toFixed(2)}
      title={title}
    >
      load: {value}
    </span>
  );
}

// ── T342b: backlog (pending pressure) ──────────────────────────────────────
// @oopslink: load alone (doing/total) hides the queue depth — an agent with a
// big backlog can still read low-load. Surface the backlog = pending (open,
// queued) task count as its own metric, colored by depth: none → neutral,
// 1–2 → low, 3–5 → mid, 6+ → high (red). The count carries the meaning; the
// color/dot is supplementary (never color-only).
export type AgentBacklogLevel = 'none' | 'low' | 'medium' | 'high';

export function deriveBacklogLevel(pending: number): AgentBacklogLevel {
  if (pending <= 0) return 'none';
  if (pending <= 2) return 'low';
  if (pending <= 5) return 'medium';
  return 'high';
}

// T342c: explicit labeled tag ("backlog: 13") — pressure by text color on a
// neutral chip, mirroring AgentLoadBadge.
const BACKLOG_TEXT: Record<AgentBacklogLevel, string> = {
  none: 'text-text-muted',
  low: 'text-success',
  medium: 'text-warning',
  high: 'text-danger',
};

export function AgentBacklogBadge({
  agent,
}: {
  agent: Pick<Agent, 'pending_tasks'>;
}): React.ReactElement {
  const pending = agent.pending_tasks ?? 0;
  const level = deriveBacklogLevel(pending);
  const title =
    pending === 0
      ? 'Backlog — no pending (queued) tasks'
      : `Backlog — ${pending} pending (queued) task${pending === 1 ? '' : 's'}`;
  return (
    <span
      className={[
        'inline-flex items-center whitespace-nowrap rounded bg-bg-subtle px-1.5 py-0.5 text-[0.6875rem] font-medium tabular-nums',
        BACKLOG_TEXT[level],
      ].join(' ')}
      data-testid="agent-backlog-badge"
      data-backlog-level={level}
      data-backlog={pending}
      title={title}
    >
      backlog: {pending}
    </span>
  );
}

// ── @oopslink: unified status pill (one segmented tag) ─────────────────────
// In the Members second sidebar the three separate chips (status word + load:x +
// backlog:n) floated at their own natural widths, so rows looked ragged. This
// collapses them into ONE rounded pill of three EQUAL-WIDTH segments — status |
// load | backlog — so every row is the same length and the trio reads as a single
// tidy tag (per the reference mock). Each segment keeps its own semantic colour
// (status by state, load/backlog by pressure level) as a solid fill with white
// text; the full breakdown stays in the hover tooltip. The per-segment testids
// (agent-status-badge / agent-load-badge / agent-backlog-badge) are preserved so
// existing row assertions keep working.

const STATUS_PILL_BG: Record<AgentStatus, string> = {
  idle: 'bg-status-green-solid',
  working: 'bg-status-blue-solid',
  busy: 'bg-status-amber-solid',
  unavailable: 'bg-status-orange-solid',
  stopped: 'bg-text-muted',
  error: 'bg-danger',
};

// Pressure → solid fill, shared by the load and backlog segments (mirrors the
// none→muted, low→green, mid→amber, high→red gradient the text colours used).
const PRESSURE_PILL_BG: Record<AgentLoadLevel, string> = {
  none: 'bg-text-muted',
  low: 'bg-status-green-solid',
  medium: 'bg-status-amber-solid',
  high: 'bg-danger',
};

export function AgentStatsPill({
  agent,
  now = Date.now(),
}: {
  agent: Pick<
    Agent,
    'lifecycle' | 'availability' | 'last_activity_at' | 'running_tasks' | 'pending_tasks' | 'task_load'
  >;
  now?: number;
}): React.ReactElement {
  const status = deriveAgentStatus(agent, now);
  const statusMeta = STATUS_META[status];
  const load = deriveAgentLoad(agent);
  const backlog = agent.pending_tasks ?? 0;
  const backlogLevel = deriveBacklogLevel(backlog);

  const segBase = 'flex-1 basis-0 truncate px-1.5 py-1 text-center';
  return (
    <span
      className="flex w-full items-stretch overflow-hidden rounded text-[0.625rem] font-semibold leading-none text-white"
      data-testid="agent-stats-pill"
      title={`${agentStatusTooltip(agent, now)} · load ${load.load.toFixed(2)} · backlog ${backlog}`}
    >
      <span
        className={[segBase, STATUS_PILL_BG[status]].join(' ')}
        data-testid="agent-status-badge"
        data-agent-status={status}
      >
        {statusMeta.label}
      </span>
      <span
        className={[segBase, 'tabular-nums', PRESSURE_PILL_BG[load.level]].join(' ')}
        data-testid="agent-load-badge"
        data-load-level={load.level}
        data-load={load.load.toFixed(2)}
      >
        load:{load.load.toFixed(1)}
      </span>
      <span
        className={[segBase, 'tabular-nums', PRESSURE_PILL_BG[backlogLevel]].join(' ')}
        data-testid="agent-backlog-badge"
        data-backlog-level={backlogLevel}
        data-backlog={backlog}
      >
        backlog:{backlog}
      </span>
    </span>
  );
}

export function LifecycleBadge({
  lifecycle,
}: {
  lifecycle: AgentLifecycle;
}): React.ReactElement {
  return (
    <span
      className={[
        'rounded px-2 py-0.5 text-[0.6875rem] uppercase tracking-wide',
        lifecycle === 'error'
          ? 'bg-danger/10 text-danger'
          : 'bg-bg-subtle text-text-secondary',
      ].join(' ')}
      data-testid="agent-lifecycle-badge"
      data-lifecycle={lifecycle}
    >
      {lifecycle}
    </span>
  );
}
