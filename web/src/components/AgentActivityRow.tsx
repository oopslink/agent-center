import type React from 'react';
import { useState } from 'react';
import type { AgentActivityEvent } from '@/api/types';

// v2.7.1 #228 PR(c): the activity timeline labels each event by a user-facing
// CATEGORY ("what is the agent doing") rather than the raw event type. The raw
// 8 event_types stay visible in the expanded JSON viewer (#216). Mapping per PD.
// PR(e): rendered as a timeline — a category-colored dot + colored label (per
// design2), so `cls` is the label text color and `dot` the rail bullet color.
type Category = { label: string; cls: string; dot: string };
const CAT_OUTPUT: Category = { label: 'Output', cls: 'text-success', dot: 'bg-success' };
const CAT_THINKING: Category = { label: 'Thinking', cls: 'italic text-text-muted', dot: 'bg-text-muted' };
const CAT_RUNNING: Category = { label: 'Running command', cls: 'text-brand', dot: 'bg-brand' };
const CAT_SEARCHING: Category = { label: 'Searching code', cls: 'text-purple-600', dot: 'bg-purple-500' };
const CAT_CHECKING: Category = { label: 'Checking messages', cls: 'text-orange-600', dot: 'bg-orange-500' };

// search-y tool names (lowercased) → "Searching code"; otherwise tool events
// are "Running command". These are claude's real content-block tool names
// (Dev #228 core verified). Shell-level searches (rg/grep/find via the Bash
// tool) carry tool_name="Bash" — the command lives in tool_input, not tool_name
// — so they degrade to Running command in v2.7.1; deeper tool_input heuristics
// are v2.8 work (PD-accepted degradation).
const SEARCH_TOOLS = new Set(['grep', 'glob', 'read', 'websearch', 'webfetch']);

function categoryOf(eventType: string, p: Record<string, unknown>): Category {
  switch (eventType) {
    case 'assistant_text':
    case 'result':
      return CAT_OUTPUT;
    case 'thinking':
      return CAT_THINKING;
    case 'tool_use':
    case 'tool_result': {
      const tn = str(p.tool_name).toLowerCase();
      return tn && SEARCH_TOOLS.has(tn) ? CAT_SEARCHING : CAT_RUNNING;
    }
    default:
      // system_init, lifecycle, rate_limit, generic system, unknown → Checking.
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
function preview(eventType: string, p: Record<string, unknown>): string {
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
      return str(p.message) || 'rate limited';
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
    case 'lifecycle':
      return str(p.event);
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
// plus the work-item / interaction refs.
export function AgentActivityRow({ event }: { event: AgentActivityEvent }): React.ReactElement {
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
          <span
            className={`shrink-0 text-[0.6875rem] font-semibold uppercase tracking-wide ${cat.cls}`}
            data-testid="agent-activity-badge"
            data-category={cat.label}
          >
            {cat.label}
          </span>
          {errored && (
            <span
              className="shrink-0 rounded bg-danger/10 px-1 py-0.5 text-[0.5625rem] font-medium uppercase tracking-wide text-danger"
              data-testid="agent-activity-failed"
            >
              failed
            </span>
          )}
          <span className="min-w-0 truncate text-text-secondary" data-testid="agent-activity-preview">
            {preview(event.event_type, payload)}
          </span>
        </span>
      </button>

      {open && (
        <div className="mb-2 ml-[4.75rem] space-y-2" data-testid="agent-activity-detail">
          {(event.work_item_ref || event.interaction_ref) && (
            <dl className="grid grid-cols-[7rem_1fr] gap-x-2 text-[0.6875rem] text-text-muted">
              {event.work_item_ref && (
                <>
                  <dt>work item</dt>
                  <dd className="truncate font-mono" data-testid="agent-activity-workitem-ref">{event.work_item_ref}</dd>
                </>
              )}
              {event.interaction_ref && (
                <>
                  <dt>interaction</dt>
                  <dd className="truncate font-mono" data-testid="agent-activity-interaction-ref">{event.interaction_ref}</dd>
                </>
              )}
            </dl>
          )}
          {/* #192 EXEMPT: raw payload debug view (opt-in expand = debug surface). */}
          <pre
            className="overflow-x-auto rounded bg-bg-subtle p-2 font-mono text-[0.6875rem] text-text-secondary"
            data-testid="agent-activity-payload-json"
          >
            {JSON.stringify(payload, null, 2)}
          </pre>
        </div>
      )}
    </li>
  );
}
