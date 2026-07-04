import type React from 'react';
import { useId, useState } from 'react';
import { useTranslation } from 'react-i18next';
import type { TFunction } from 'i18next';
import type { AgentActivityEvent } from '@/api/types';
import { CollapsibleCodeBlock } from './CollapsibleCodeBlock';
import { ActivityRefText } from './ActivityRefText';

// v2.7.1 #228 PR(c): the activity timeline labels each event by a user-facing
// CATEGORY ("what is the agent doing") rather than the raw event type. The raw
// 8 event_types stay visible in the expanded JSON viewer (#216). Mapping per PD.
// PR(e): rendered as a timeline — a category-colored dot + colored label (per
// design2), so `cls` is the label text color and `dot` the rail bullet color.
// `label` stays a STABLE English literal: it is used as the `data-category`
// test/data hook (and as the i18n key suffix). The displayed text is localised
// at render time via t(`activity.category.<labelKey>`).
type Category = { key: string; label: string; labelKey: string; cls: string; dot: string };
const CAT_OUTPUT: Category = { key: 'output', label: 'Output', labelKey: 'output', cls: 'text-success', dot: 'bg-success' };
const CAT_THINKING: Category = { key: 'thinking', label: 'Thinking', labelKey: 'thinking', cls: 'italic text-text-muted', dot: 'bg-text-muted' };
const CAT_CHECKING: Category = { key: 'checking', label: 'Checking messages', labelKey: 'checking', cls: 'text-status-orange-strong', dot: 'bg-status-orange-solid' };
// v2.8 #274 increment 4: tool_use / tool_result get their own categories
// (replacing the broad Running command / Searching code labels — Q1). The badge
// label + icon are computed dynamically at render time (search-vs-run icon via
// SEARCH_TOOLS — Q2; tool_result ok/error via payload.ok — Q3).
const CAT_TOOL_USE: Category = { key: 'tool_use', label: 'Tool', labelKey: 'tool', cls: 'text-brand', dot: 'bg-brand' };
const CAT_TOOL_RESULT: Category = { key: 'tool_result', label: 'Result', labelKey: 'result', cls: 'text-text-secondary', dot: 'bg-text-secondary' };
// T345 (@oopslink): agent control/lifecycle ops (start / stop / restart / reset)
// get their OWN category — they were falling through to CAT_CHECKING and being
// folded into the "Checking messages × N" group, so operators couldn't see when
// an agent was started/stopped/reset. Distinct blue label + dot, never grouped.
const CAT_CONTROL: Category = { key: 'control', label: 'Control', labelKey: 'control', cls: 'text-status-blue-fg', dot: 'bg-status-blue-solid' };
// v2.31.0 (oopslink DM 2026-07-03): concurrently-forked executors emit lifecycle
// events (executor.start / .stop / .progress — internal/workerdaemon/executor_activity.go)
// that previously fell into CAT_CONTROL, indistinguishable from the agent's own
// session control ops. They now get their OWN violet "Executor" category so the
// stream visibly separates session vs executor activity, and the preview surfaces
// which executor is running which task without expanding the row.
const CAT_EXECUTOR: Category = { key: 'executor', label: 'Executor', labelKey: 'executor', cls: 'text-status-violet-fg', dot: 'bg-status-violet-solid' };
// message-consumption activity (docs/design/features/agent-message-consumption-activity.md):
// Received = inbound message entered the agent context (message_delivered — primary debug signal,
// NEVER folded into Checking); Acknowledged = agent confirmed read via mark_seen (muted accent).
const CAT_DELIVERED: Category = { key: 'delivered', label: 'Received', labelKey: 'received', cls: 'text-status-teal-fg', dot: 'bg-status-teal-solid' };
const CAT_ACK: Category = { key: 'acknowledged', label: 'Acknowledged', labelKey: 'acknowledged', cls: 'text-text-muted', dot: 'bg-text-muted' };

// search-y tool names (lowercased) → "Searching code"; otherwise tool events
// are "Running command". These are claude's real content-block tool names
// (Dev #228 core verified). Shell-level searches (rg/grep/find via the Bash
// tool) carry tool_name="Bash" — the command lives in tool_input, not tool_name
// — so they degrade to Running command in v2.7.1; deeper tool_input heuristics
// are v2.8 work (PD-accepted degradation).
const SEARCH_TOOLS = new Set(['grep', 'glob', 'read', 'websearch', 'webfetch']);

// isExecutorEvent — a lifecycle event produced by a forked executor (payload.event
// is executor.start / executor.stop / executor.progress), as opposed to the agent's
// own session control ops which share event_type='lifecycle'.
function isExecutorEvent(eventType: string, p: Record<string, unknown>): boolean {
  return eventType === 'lifecycle' && str(p.event).startsWith('executor.');
}

function categoryOf(eventType: string, p?: Record<string, unknown>): Category {
  // Executor lifecycle events split off from CAT_CONTROL into their own category
  // (needs the payload to tell them apart from session control ops).
  if (p && isExecutorEvent(eventType, p)) return CAT_EXECUTOR;
  switch (eventType) {
    case 'assistant_text':
    case 'result':
      return CAT_OUTPUT;
    case 'thinking':
      return CAT_THINKING;
    case 'tool_use':
      return CAT_TOOL_USE;
    case 'tool_result':
      return CAT_TOOL_RESULT;
    case 'lifecycle':
      // T345: start / stop / restart / reset — a distinct "Control" category, NOT
      // folded into "Checking messages".
      return CAT_CONTROL;
    case 'message_delivered':
      return CAT_DELIVERED;
    case 'message_acknowledged':
      return CAT_ACK;
    default:
      // system_init, rate_limit, generic system, unknown → Checking.
      return CAT_CHECKING;
  }
}

function parsePayload(raw: string): Record<string, unknown> {
  if (!raw) return {};
  try {
    const v = JSON.parse(raw);
    return v && typeof v === 'object' ? (v as Record<string, unknown>) : { value: v };
  } catch {
    return {};
  }
}

function str(v: unknown): string {
  return typeof v === 'string' ? v : v == null ? '' : String(v);
}

// #274: the displayable tool_result output — prefer the nested tool_result's
// human-readable `.content`, else pretty-print the nested JSON (Lock 12). The
// extraction lives in the consumer (here), keeping CollapsibleCodeBlock a pure
// `{code}` component.
function extractToolOutput(p: Record<string, unknown>): string {
  const tr = p.tool_result;
  if (tr && typeof tr === 'object') {
    const c = (tr as Record<string, unknown>).content;
    if (typeof c === 'string') return c;
  }
  try {
    return JSON.stringify(tr ?? p, null, 2);
  } catch {
    return '';
  }
}

// #274 increment 4: the dynamic tool badge — an SVG icon-component (never emoji,
// ux-standards red-line) + a label. tool_use distinguishes search vs run (Q2);
// tool_result distinguishes ok vs error from payload.ok (Q3). not-color-only:
// the icon + label carry the meaning, color is supplementary.
interface ToolBadge {
  Icon: () => React.ReactElement;
  label: string;
  cls: string;
  aria: string;
  status?: 'ok' | 'error';
}
function toolBadge(key: string, p: Record<string, unknown>, t: TFunction): ToolBadge | null {
  if (key === 'tool_use') {
    const search = SEARCH_TOOLS.has(str(p.tool_name).toLowerCase());
    return search
      ? { Icon: SearchIcon, label: t('activity.tool.searching'), cls: 'text-status-purple-strong', aria: t('activity.tool.searchingAria') }
      : { Icon: WrenchIcon, label: t('activity.tool.running'), cls: 'text-brand', aria: t('activity.tool.runningAria') };
  }
  if (key === 'tool_result') {
    const ok = p.ok !== false && p.is_error !== true;
    return ok
      ? { Icon: CheckIcon, label: t('activity.tool.result'), cls: 'text-success', aria: t('activity.tool.resultOkAria'), status: 'ok' }
      : { Icon: XIcon, label: t('activity.tool.result'), cls: 'text-danger', aria: t('activity.tool.resultErrorAria'), status: 'error' };
  }
  return null;
}

// Inline single-stroke SVGs (no-emoji UX rule, matching #240/#250/#270).
function SearchIcon(): React.ReactElement {
  return (
    <svg viewBox="0 0 20 20" fill="none" className="h-full w-full stroke-current" strokeWidth="1.5" aria-hidden="true">
      <circle cx="9" cy="9" r="5" />
      <path d="M13 13l4 4" strokeLinecap="round" />
    </svg>
  );
}
function WrenchIcon(): React.ReactElement {
  return (
    <svg viewBox="0 0 20 20" fill="none" className="h-full w-full stroke-current" strokeWidth="1.5" aria-hidden="true">
      <path d="M13 3a4 4 0 0 0-3.5 6L4 14.5 5.5 16l5.5-5.5A4 4 0 1 0 13 3Z" strokeLinejoin="round" />
    </svg>
  );
}
function CheckIcon(): React.ReactElement {
  return (
    <svg viewBox="0 0 20 20" fill="none" className="h-full w-full stroke-current" strokeWidth="2" aria-hidden="true">
      <path d="M4 10l4 4 8-8" strokeLinecap="round" strokeLinejoin="round" />
    </svg>
  );
}
function XIcon(): React.ReactElement {
  return (
    <svg viewBox="0 0 20 20" fill="none" className="h-full w-full stroke-current" strokeWidth="2" aria-hidden="true">
      <path d="M5 5l10 10M15 5L5 15" strokeLinecap="round" />
    </svg>
  );
}

// v2.8 #274: an event is a "Checking" event when it falls through to CAT_CHECKING
// (system_init / lifecycle / rate_limit / unknown). Consecutive runs of these are
// folded into one "Checking messages × N" group in the timeline.
export function isCheckingEvent(event: AgentActivityEvent): boolean {
  return categoryOf(event.event_type, parsePayload(event.payload)) === CAT_CHECKING;
}

// shortExecId trims a long executor id to a readable tail for the row preview
// (the full executor:<id> stays in the expanded interaction_ref).
function shortExecId(id: string): string {
  if (!id) return '';
  const tail = id.includes('-') ? id.slice(id.lastIndexOf('-') + 1) : id;
  return tail.length > 8 ? tail.slice(-8) : tail;
}

// executorPreview — one-line summary for an executor lifecycle event: the kind
// (start/stop/progress), which executor, which task, and the state/outcome — so
// an operator sees "which executor is running which task" without expanding.
function executorPreview(p: Record<string, unknown>): string {
  const kind = str(p.event).replace(/^executor\./, ''); // start | stop | progress
  const exec = shortExecId(str(p.executor_id));
  const task = truncate(str(p.title) || str(p.task_ref), 60);
  // scope is the emitter's precomputed one-word summary: model (start),
  // outcome[:reason] (stop), or state (progress); fall back to the raw fields.
  const detail = str(p.scope) || str(p.outcome) || str(p.state);
  return [kind, exec && `exec ${exec}`, task, detail].filter(Boolean).join(' · ');
}

function truncate(s: string, n: number): string {
  return s.length > n ? `${s.slice(0, n)}…` : s;
}

// PR(e): the timeline shows a compact wall-clock time; the full ISO timestamp
// stays on hover (title) for operators.
function formatTime(iso: string): string {
  const d = new Date(iso);
  if (Number.isNaN(d.getTime())) return iso;
  return d.toLocaleTimeString([], { hour: '2-digit', minute: '2-digit', second: '2-digit', hour12: false });
}

function summarizeArgs(args: unknown): string {
  if (args == null) return '';
  if (typeof args === 'string') return truncate(args, 40);
  try {
    return truncate(JSON.stringify(args), 40);
  } catch {
    return '';
  }
}

// preview builds the one-line summary for a row (Phase A). It must NOT render
// raw entity ids as the visible handle (#192) — these are content/operational
// summaries, not entity references.
function preview(eventType: string, p: Record<string, unknown>, t: TFunction): string {
  switch (eventType) {
    case 'assistant_text':
    case 'thinking':
      return truncate(str(p.text), 120);
    case 'result': {
      // End-of-turn aggregate: total tokens + cost (claude `result` row).
      const tokens =
        (typeof p.tokens_in === 'number' ? p.tokens_in : 0) +
        (typeof p.tokens_out === 'number' ? p.tokens_out : 0);
      const parts: string[] = [];
      if (tokens > 0) parts.push(`${tokens} tok`);
      if (p.cost_usd != null) {
        // Format the summary cost (the raw float stays in the expanded JSON).
        const c = Number(p.cost_usd);
        parts.push(Number.isFinite(c) ? `$${c.toFixed(4)}` : `$${str(p.cost_usd)}`);
      }
      return parts.join(' · ') || str(p.subtype);
    }
    case 'rate_limit':
      return str(p.message) || t('activity.row.rateLimited');
    case 'tool_use':
      return `${str(p.tool_name)}(${summarizeArgs(p.args)})`;
    case 'tool_result': {
      const parts = [str(p.tool_name)];
      if (p.duration_ms != null) parts.push(`${str(p.duration_ms)}ms`);
      if (p.tokens != null) parts.push(`${str(p.tokens)} tok`);
      return parts.filter(Boolean).join(' · ');
    }
    case 'system_init': {
      const sid = str(p.session_id);
      return [str(p.model), sid ? sid.slice(0, 8) : ''].filter(Boolean).join(' · ');
    }
    case 'status_change':
      return `${str(p.from)} → ${str(p.to)}`;
    case 'lifecycle': {
      // Executor lifecycle → richer executor-scoped preview.
      if (str(p.event).startsWith('executor.')) return executorPreview(p);
      // T338 payload = {event:<verb>, scope?}; show e.g. "reset (workspace)".
      const verb = str(p.event);
      const scope = str(p.scope);
      return scope ? `${verb} (${scope})` : verb;
    }
    case 'message_delivered': {
      const who = str(p.sender_display) || str(p.sender_ref);
      const body = str(p.content_preview);
      return [who, truncate(body, 100)].filter(Boolean).join(': ');
    }
    case 'message_acknowledged':
      return t('activity.row.readConfirmed');
    default:
      try {
        return truncate(JSON.stringify(p), 120);
      } catch {
        return '';
      }
  }
}

// AgentActivityRow (v2.7.1 #216) — Phase A: subtype badge + one-line preview.
// Phase B: click to expand the raw payload JSON (operator/debug view, exempt
// from the #192 zero-raw-id sweep via data-testid="agent-activity-payload-json")
// plus the task / interaction refs.
export function AgentActivityRow({ event }: { event: AgentActivityEvent }): React.ReactElement {
  const { t } = useTranslation('insights');
  const [open, setOpen] = useState(false);
  const payload = parsePayload(event.payload);
  // v2.7.1 #228 PR(c): main badge shows the user-facing category; the raw
  // event_type stays on data-event-type + inside the expanded JSON viewer.
  const cat = categoryOf(event.event_type, payload);
  // A failed tool_result / result keeps its category but flags the failure
  // inline so the error signal isn't buried in the JSON viewer.
  const errored =
    (event.event_type === 'tool_result' || event.event_type === 'result') &&
    (payload.ok === false || payload.is_error === true);

  // #274 increment 4: tool_use / tool_result badges are computed dynamically —
  // an SVG icon (NOT emoji, ux-standards red-line) + a label that distinguishes
  // search vs run (Q2) / ok vs error (Q3).
  const tool = toolBadge(cat.key, payload, t);
  // tool_result inline output → the shared CollapsibleCodeBlock (contextLabel
  // 'output'): prefer the human-readable .content, else the pretty-printed
  // nested tool_result JSON (Lock 12). #192: output body is content-exempt.
  const toolOutput = cat.key === 'tool_result' ? extractToolOutput(payload) : '';

  return (
    <li
      className="text-xs"
      data-testid="agent-activity-row"
      data-activity-id={event.id}
      data-event-type={event.event_type}
    >
      <button
        type="button"
        onClick={() => setOpen((v) => !v)}
        aria-expanded={open}
        className="flex w-full items-start gap-2 py-2 text-left"
        data-testid="agent-activity-toggle"
      >
        {/* timeline: timestamp (left) + category-colored rail dot (design2). */}
        <time
          className="w-14 shrink-0 pt-px tabular-nums text-text-muted"
          dateTime={event.occurred_at}
          title={event.occurred_at}
        >
          {formatTime(event.occurred_at)}
        </time>
        <span className="relative flex w-3 shrink-0 justify-center pt-1" aria-hidden="true">
          <span className="absolute bottom-[-1rem] left-1/2 top-1.5 w-px -translate-x-1/2 bg-border-base" />
          <span className={`relative z-10 h-2 w-2 rounded-full ${cat.dot}`} />
        </span>
        <span className="flex min-w-0 flex-1 items-center gap-2">
          {tool ? (
            <span
              className={`flex shrink-0 items-center gap-1 text-[0.6875rem] font-semibold uppercase tracking-wide ${tool.cls}`}
              data-testid="agent-activity-badge"
              data-category={cat.label}
              data-tool-status={tool.status ?? undefined}
              aria-label={tool.aria}
            >
              <span aria-hidden="true" className="inline-flex h-3 w-3">
                <tool.Icon />
              </span>
              {tool.label}
            </span>
          ) : (
            <span
              className={`shrink-0 text-[0.6875rem] font-semibold uppercase tracking-wide ${cat.cls}`}
              data-testid="agent-activity-badge"
              data-category={cat.label}
            >
              {t(`activity.category.${cat.labelKey}`)}
            </span>
          )}
          {errored && (
            <span
              className="shrink-0 rounded bg-danger/10 px-1 py-0.5 text-[0.5625rem] font-medium uppercase tracking-wide text-danger"
              data-testid="agent-activity-failed"
            >
              {t('activity.row.failed')}
            </span>
          )}
          <span className="min-w-0 truncate text-text-secondary" data-testid="agent-activity-preview">
            {preview(event.event_type, payload, t)}
          </span>
        </span>
      </button>

      {open && (
        <div className="mb-2 ml-[4.75rem] space-y-2" data-testid="agent-activity-detail">
          {(event.task_ref || event.interaction_ref) && (
            <dl className="grid grid-cols-[7rem_1fr] gap-x-2 text-[0.6875rem] text-text-muted">
              {event.task_ref && (
                <>
                  <dt>{t('activity.detail.task')}</dt>
                  <dd className="truncate font-mono" data-testid="agent-activity-task-ref">
                    <ActivityRefText text={event.task_ref} />
                  </dd>
                </>
              )}
              {event.interaction_ref && (
                <>
                  <dt>{t('activity.detail.interaction')}</dt>
                  <dd className="truncate font-mono" data-testid="agent-activity-interaction-ref">{event.interaction_ref}</dd>
                </>
              )}
            </dl>
          )}
          {/* #274: tool_result output rendered via the shared collapsible block
              (long output auto-collapses; copy/expand a11y from #187). */}
          {cat.key === 'tool_result' && toolOutput && (
            <div data-testid="agent-activity-tool-output">
              <CollapsibleCodeBlock code={toolOutput} contextLabel="output" />
            </div>
          )}
          {/* #192 EXEMPT: raw payload debug view (opt-in expand = debug surface). */}
          <pre
            className="overflow-x-auto rounded bg-bg-subtle p-2 font-mono text-[0.6875rem] text-text-secondary"
            data-testid="agent-activity-payload-json"
          >
            <ActivityRefText text={JSON.stringify(payload, null, 2)} />
          </pre>
        </div>
      )}
    </li>
  );
}

// The checking-group range uses the same local wall-clock as the per-event
// rows (formatTime). Slicing the raw ISO printed UTC (e.g. "09:48") next to
// local-time siblings ("17:48") — convert to the viewer's timezone instead.
function timeOf(iso: string): string {
  const d = new Date(iso);
  if (Number.isNaN(d.getTime())) return iso;
  return d.toLocaleTimeString([], { hour: '2-digit', minute: '2-digit', hour12: false });
}

// v2.8 #274: a folded run of consecutive "Checking" events — "Checking messages
// × N" + the run's time range + a disclosure to reveal each raw event. The
// disclosure (button, aria-expanded + aria-controls→the region's useId() id) is
// NOT the list container; expanding is lossless (every original event shown).
export function CheckingGroup({ events }: { events: AgentActivityEvent[] }): React.ReactElement {
  const { t } = useTranslation('insights');
  const [expanded, setExpanded] = useState(false);
  const regionId = useId();
  const n = events.length;
  // events are newest-first (ULID DESC) → [0] = latest, [n-1] = earliest.
  const latest = events[0]?.occurred_at ?? '';
  const earliest = events[n - 1]?.occurred_at ?? '';
  return (
    <li data-testid="agent-activity-checking-group" data-count={n}>
      <button
        type="button"
        className="flex w-full items-center justify-between gap-2 py-2 text-left text-xs text-status-orange-strong hover:bg-bg-subtle"
        data-testid="agent-activity-checking-toggle"
        aria-expanded={expanded}
        aria-controls={regionId}
        aria-label={t('activity.checkingGroup.aria', {
          count: n,
          state: expanded ? t('activity.checkingGroup.expanded') : t('activity.checkingGroup.collapsed'),
        })}
        onClick={() => setExpanded((e) => !e)}
      >
        <span className="flex items-center gap-2">
          <span aria-hidden="true" className="h-2 w-2 rounded-full bg-status-orange-solid" />
          {t('activity.checkingGroup.label', { count: n })}
        </span>
        <span className="tabular-nums text-text-muted">
          {timeOf(earliest)}–{timeOf(latest)}
        </span>
      </button>
      {expanded && (
        <ul
          id={regionId}
          className="ml-4 border-l border-border-base pl-2"
          data-testid="agent-activity-checking-expanded"
        >
          {events.map((ev) => (
            <AgentActivityRow key={ev.id} event={ev} />
          ))}
        </ul>
      )}
    </li>
  );
}
