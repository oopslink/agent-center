import { Fragment } from 'react';
import { useTranslation } from 'react-i18next';
import type { EnvWorker } from '@/api/types';

function formatDate(iso?: string): string {
  if (!iso) return '—';
  const d = new Date(iso);
  return Number.isNaN(d.getTime()) ? iso : d.toLocaleString();
}

// Worker-reported host + build identity fields (T752). The worker uploads these
// on every online (capabilities report); the backend emits each key only when
// the worker actually reported it. So a field shows its REAL value when present,
// and falls back to the explicit "Coming in v2.9" placeholder when absent (older
// worker / not yet reported) — the gap stays honest (T-9 completeness) rather
// than blank/fake.
type SysField = {
  slug: string; // stable testid slug
  labelKey: string; // i18n label key
  mono?: boolean; // render value monospaced (paths / versions)
  value: (w: EnvWorker) => string | undefined;
};

const SYS_FIELDS: readonly SysField[] = [
  { slug: 'hostname', labelKey: 'workers.profile.deferred.hostname', value: (w) => w.hostname },
  { slug: 'os', labelKey: 'workers.profile.deferred.os', value: (w) => w.os },
  { slug: 'architecture', labelKey: 'workers.profile.deferred.architecture', value: (w) => w.arch },
  {
    slug: 'install-path',
    labelKey: 'workers.profile.deferred.installPath',
    mono: true,
    value: (w) => w.install_path,
  },
  // The worker's OWN build identity (T752).
  {
    slug: 'worker-version',
    labelKey: 'workers.profile.deferred.workerVersion',
    mono: true,
    value: (w) => w.worker_version,
  },
];

// WorkerProfile — the #273 Profile tab. Renders the 5 core fields the backend
// always has (GET /api/workers/{id} = EnvWorker) plus the worker-reported
// system-info fields (T752): each shown as its real value, or the "Coming in
// v2.9" placeholder when the worker has not reported it yet.
export function WorkerProfile({ worker }: { worker: EnvWorker }): React.ReactElement {
  const { t } = useTranslation('members');
  const online = worker.status === 'online';
  return (
    <dl
      className="grid grid-cols-[max-content_1fr] gap-x-6 gap-y-2 text-sm"
      data-testid="worker-profile"
    >
      <dt className="text-text-muted">{t('workers.profile.id')}</dt>
      <dd className="font-mono text-xs" data-testid="worker-profile-id" title={worker.worker_id}>
        {worker.worker_id}
      </dd>

      <dt className="text-text-muted">{t('workers.profile.name')}</dt>
      <dd data-testid="worker-profile-name">
        {worker.name || <span className="italic text-text-muted">{t('workers.profile.unnamed')}</span>}
      </dd>

      <dt className="text-text-muted">{t('workers.profile.status')}</dt>
      <dd data-testid="worker-profile-status">
        {/* not-color-only: dot + text label */}
        <span className="inline-flex items-center gap-1">
          <span
            className={`inline-block h-2 w-2 rounded-full ${online ? 'bg-success' : 'bg-text-muted'}`}
            aria-hidden="true"
          />
          {online ? t('workers.profile.online') : t('workers.profile.offline')}
        </span>
      </dd>

      <dt className="text-text-muted">{t('workers.profile.registered')}</dt>
      <dd data-testid="worker-profile-enrolled" title={worker.enrolled_at}>
        {formatDate(worker.enrolled_at)}
      </dd>

      <dt className="text-text-muted">{t('workers.profile.lastHeartbeat')}</dt>
      <dd data-testid="worker-profile-heartbeat" title={worker.last_heartbeat_at}>
        {formatDate(worker.last_heartbeat_at)}
      </dd>

      {SYS_FIELDS.map((f) => {
        const v = f.value(worker)?.trim();
        return (
          <Fragment key={f.slug}>
            <dt className="text-text-muted">{t(f.labelKey)}</dt>
            {v ? (
              <dd
                className={f.mono ? 'break-all font-mono text-xs' : ''}
                data-testid={`worker-profile-${f.slug}`}
                title={v}
              >
                {v}
              </dd>
            ) : (
              <dd
                className="italic text-text-muted"
                data-testid={`worker-profile-deferred-${f.slug}`}
              >
                {t('workers.profile.deferredValue')}
              </dd>
            )}
          </Fragment>
        );
      })}
    </dl>
  );
}
