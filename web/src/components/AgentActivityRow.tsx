import type React from 'react';
import { useState } from 'react';
import type { AgentActivityEvent } from '@/api/types';

// v2.7.1 #216: per-event-type presentation (badge color + label). Unknown
// types fall through to a neutral badge + JSON preview.
const SUBTYPES: Record<string, { label: string; cls: string }> = {
  system_init: { label: 'System', cls: 'bg-bg-subtle text-text-muted' },
  assistant_text: { label: 'Assistant', cls: 'bg-brand/10 text-brand' },
  thinking: { label: 'Thinking', cls: 'bg-bg-subtle text-text-secondary' },
  tool_use: { label: 'Tool', cls: 'bg-accent/10 text-accent' },
  tool_result: { label: 'Result', cls: 'bg-bg-subtle text-text-secondary' },
  // result = end-of-turn aggregate (tokens/cost); colored by ok like tool_result.
  result: { label: 'Turn', cls: 'bg-bg-subtle text-text-secondary' },
  rate_limit: { label: 'Rate limit', cls: 'bg-danger/10 text-danger' },
  status_change: { label: 'Status', cls: 'bg-bg-subtle text-text-secondary' },
  lifecycle: { label: 'Lifecycle', cls: 'bg-bg-subtle text-text-muted' },
};

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
      if (p.cost_usd != null) parts.push(`$${str(p.cost_usd)}`);
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
  const meta = SUBTYPES[event.event_type] ?? { label: event.event_type, cls: 'bg-bg-subtle text-text-muted' };
  // tool_result + result (end-of-turn) color by outcome (ok / is_error).
  const errored = payload.ok === false || payload.is_error === true;
  const cls =
    event.event_type === 'tool_result' || event.event_type === 'result'
      ? errored
        ? 'bg-danger/10 text-danger'
        : 'bg-success/10 text-success'
      : meta.cls;

  return (
    <li
      className="py-2 text-xs"
      data-testid="agent-activity-row"
      data-activity-id={event.id}
      data-event-type={event.event_type}
    >
      <button
        type="button"
        onClick={() => setOpen((v) => !v)}
        aria-expanded={open}
        className="flex w-full items-start justify-between gap-3 text-left"
        data-testid="agent-activity-toggle"
      >
        <span className="flex min-w-0 flex-1 items-center gap-2">
          <span
            className={`shrink-0 rounded px-1.5 py-0.5 text-[0.625rem] font-medium uppercase tracking-wide ${cls}`}
            data-testid="agent-activity-badge"
          >
            {meta.label}
          </span>
          <span className="min-w-0 truncate text-text-secondary" data-testid="agent-activity-preview">
            {preview(event.event_type, payload)}
          </span>
        </span>
        <span className="shrink-0 tabular-nums text-text-muted">{event.occurred_at}</span>
      </button>

      {open && (
        <div className="mt-2 space-y-2" data-testid="agent-activity-detail">
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
