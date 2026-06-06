import { Fragment } from 'react';
import type { EnvWorker } from '@/api/types';

function formatDate(iso?: string): string {
  if (!iso) return '—';
  const d = new Date(iso);
  return Number.isNaN(d.getTime()) ? iso : d.toLocaleString();
}

// Fields the worker does not yet report (no backend source in v2.8 — the
// workforce.Worker entity lacks them, no worker-side reporting). Shown as
// explicit "Coming in v2.9" deferred-with-pointer rather than blank/omitted, so
// the gap is honest (T-9 completeness, same as the Activity tab). The v2.9
// worker-info-reporting epic adds worker-side enroll reporting + schema + DTO.
const DEFERRED_FIELDS = [
  'Hostname',
  'OS',
  'Architecture',
  'agent-center version',
  'Install path',
] as const;

const slug = (s: string) => s.toLowerCase().replace(/[^a-z0-9]+/g, '-').replace(/^-|-$/g, '');

// WorkerProfile — the #273 Profile tab. Renders the 5 fields the backend really
// has (GET /api/workers/{id} = EnvWorker) + the 5 deferred fields as v2.9
// placeholders.
export function WorkerProfile({ worker }: { worker: EnvWorker }): React.ReactElement {
  const online = worker.status === 'online';
  return (
    <dl
      className="grid grid-cols-[max-content_1fr] gap-x-6 gap-y-2 text-sm"
      data-testid="worker-profile"
    >
      <dt className="text-text-muted">Worker ID</dt>
      <dd className="font-mono text-xs" data-testid="worker-profile-id" title={worker.worker_id}>
        {worker.worker_id}
      </dd>

      <dt className="text-text-muted">Name</dt>
      <dd data-testid="worker-profile-name">
        {worker.name || <span className="italic text-text-muted">unnamed</span>}
      </dd>

      <dt className="text-text-muted">Status</dt>
      <dd data-testid="worker-profile-status">
        {/* not-color-only: dot + text label */}
        <span className="inline-flex items-center gap-1">
          <span
            className={`inline-block h-2 w-2 rounded-full ${online ? 'bg-success' : 'bg-text-muted'}`}
            aria-hidden="true"
          />
          {online ? 'Online' : 'Offline'}
        </span>
      </dd>

      <dt className="text-text-muted">Registered</dt>
      <dd data-testid="worker-profile-enrolled" title={worker.enrolled_at}>
        {formatDate(worker.enrolled_at)}
      </dd>

      <dt className="text-text-muted">Last heartbeat</dt>
      <dd data-testid="worker-profile-heartbeat" title={worker.last_heartbeat_at}>
        {formatDate(worker.last_heartbeat_at)}
      </dd>

      {DEFERRED_FIELDS.map((label) => (
        <Fragment key={label}>
          <dt className="text-text-muted">{label}</dt>
          <dd
            className="italic text-text-muted"
            data-testid={`worker-profile-deferred-${slug(label)}`}
          >
            Coming in v2.9
          </dd>
        </Fragment>
      ))}
    </dl>
  );
}
