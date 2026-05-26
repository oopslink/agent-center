import type React from 'react';
import { useAppStore } from '@/store/app';

const COLORS: Record<string, string> = {
  idle: 'bg-text-muted',
  connecting: 'bg-warning',
  open: 'bg-success',
  reconnecting: 'bg-warning animate-pulse',
  closed: 'bg-text-muted',
};

const LABEL: Record<string, string> = {
  idle: 'idle',
  connecting: 'connecting',
  open: 'live',
  reconnecting: 'reconnecting',
  closed: 'offline',
};

// SSEIndicator — small colored dot + label for the topbar. Renders the
// current Zustand sseStatus so users see the live/reconnecting state
// without surfacing the underlying EventSource details.
export function SSEIndicator(): React.ReactElement {
  const status = useAppStore((s) => s.sseStatus);
  return (
    <span
      className="flex items-center gap-1.5 text-xs text-text-muted"
      data-testid="sse-indicator"
      data-status={status}
    >
      <span className={`inline-block h-2 w-2 rounded-full ${COLORS[status] ?? COLORS.idle}`} />
      <span>{LABEL[status] ?? status}</span>
    </span>
  );
}
