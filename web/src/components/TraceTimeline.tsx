import type React from 'react';
import { useState } from 'react';
import type { TraceEvent } from '@/api/types';

interface Props {
  events: TraceEvent[];
}

// TraceTimeline — renders agent execution trace events as a collapsible
// vertical list. Each row shows event_type + occurred_at; clicking
// expands the payload JSON. Empty state is a calm placeholder.
export function TraceTimeline({ events }: Props): React.ReactElement {
  if (events.length === 0) {
    return (
      <p
        className="text-sm text-slate-500"
        data-testid="trace-empty"
      >
        No trace events yet.
      </p>
    );
  }
  return (
    <ol
      className="space-y-1 border-l border-slate-200 pl-4"
      data-testid="trace-timeline"
    >
      {events.map((ev) => (
        <TraceRow key={ev.id} ev={ev} />
      ))}
    </ol>
  );
}

function TraceRow({ ev }: { ev: TraceEvent }): React.ReactElement {
  const [open, setOpen] = useState(false);
  const hasPayload = !!ev.payload && Object.keys(ev.payload).length > 0;
  return (
    <li
      className="text-sm"
      data-testid="trace-row"
      data-event-id={ev.id}
      data-event-type={ev.event_type}
    >
      <button
        type="button"
        className="flex w-full items-center gap-2 rounded px-2 py-1 text-left hover:bg-slate-100"
        onClick={() => hasPayload && setOpen((o) => !o)}
        disabled={!hasPayload}
        data-testid="trace-toggle"
      >
        <span className="text-xs text-slate-400">
          {hasPayload ? (open ? '▾' : '▸') : '·'}
        </span>
        <span className="font-mono text-xs">{ev.event_type}</span>
        <time className="ml-auto text-xs text-slate-400">{ev.occurred_at}</time>
      </button>
      {open && hasPayload && (
        <pre
          className="mt-1 ml-4 overflow-x-auto rounded bg-slate-100 p-2 text-xs text-slate-800"
          data-testid="trace-payload"
        >
          {JSON.stringify(ev.payload, null, 2)}
        </pre>
      )}
    </li>
  );
}
