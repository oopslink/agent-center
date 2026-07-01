import { useParams, useSearchParams } from 'react-router-dom';
import { useTranslation } from 'react-i18next';
import { OrgLink } from '@/OrgContext';
import { Breadcrumb } from '@/components/Breadcrumb';
import { EmptyState } from '@/components/EmptyState';
import { useWorker } from '@/api/workers';
import { useTablistKeyboard } from '@/components/useTablistKeyboard';
import { WorkerProfile } from '@/components/WorkerProfile';
import { BoundAgents } from '@/components/BoundAgents';
import { WorkerManagement } from '@/components/WorkerManagement';

// WorkerDetail (/workers/:id). Environment BC. A dedicated worker page (v2.8 #273)
// mirroring AgentDetail's 4-tab framework (#228) + the shared manual-activation
// keyboard hook (#273 increment 1). Backend reuses existing endpoints (no new):
// GET /api/workers/{id} (Profile) + GET /api/agents?worker_id= (Bound Agents) +
// re-mint / rename / remove (Management). Activity is a v2.9 placeholder.
const WORKER_TABS = [
  { key: 'profile' },
  { key: 'agents' },
  { key: 'management' },
  { key: 'activity' },
] as const;
type WorkerTab = (typeof WORKER_TABS)[number]['key'];

export default function WorkerDetail(): React.ReactElement {
  const { t } = useTranslation('members');
  const { id = '' } = useParams<{ id: string }>();
  const worker = useWorker(id);

  // Active tab synced to ?tab= so a tab is shareable/bookmarkable (#228 pattern).
  const [searchParams, setSearchParams] = useSearchParams();
  const tabParam = searchParams.get('tab');
  const tab: WorkerTab = (
    WORKER_TABS.some((t) => t.key === tabParam) ? tabParam : 'profile'
  ) as WorkerTab;
  const setTab = (t: WorkerTab) =>
    setSearchParams(
      (prev) => {
        const p = new URLSearchParams(prev);
        p.set('tab', t);
        return p;
      },
      { replace: true },
    );
  // v2.8 #273: shared WAI-ARIA manual-activation tablist keyboard nav.
  const tablist = useTablistKeyboard({ keys: WORKER_TABS.map((t) => t.key), active: tab });

  if (worker.isLoading) {
    return (
      <section className="text-sm text-text-muted" data-testid="page-WorkerDetail">
        {t('workers.detail.loading')}
      </section>
    );
  }
  if (worker.isError) {
    return (
      <section className="space-y-3" data-testid="page-WorkerDetail">
        <p className="text-sm text-danger" data-testid="worker-not-found">
          {(worker.error as Error).message}
        </p>
        <OrgLink to="/environment" className="text-accent hover:underline">
          {t('workers.detail.backToEnvironment')}
        </OrgLink>
      </section>
    );
  }
  if (!worker.data) {
    return (
      <section className="text-sm text-danger" data-testid="page-WorkerDetail">
        {t('workers.detail.lookupFailed')}
      </section>
    );
  }

  const w = worker.data;
  const online = w.status === 'online';

  return (
    <section className="space-y-4" data-testid="page-WorkerDetail" data-worker-id={w.worker_id}>
      <Breadcrumb
        items={[
          { label: t('workers.detail.breadcrumb.environment'), to: '/environment' },
          { label: t('workers.detail.breadcrumb.workers') },
          { label: w.name || w.worker_id },
        ]}
      />
      <header className="flex flex-wrap items-center justify-between gap-3 border-b border-border-base pb-3">
        <div className="space-y-1">
          <div className="flex flex-wrap items-center gap-2">
            <h2 className="text-xl font-semibold">{w.name || w.worker_id}</h2>
            {/* status badge — not-color-only: dot + text label (#273 a11y). */}
            <span
              className="inline-flex items-center gap-1 rounded px-2 py-0.5 text-xs text-text-muted"
              data-testid="worker-status-badge"
              data-status={w.status}
            >
              <span
                className={`inline-block h-2 w-2 rounded-full ${online ? 'bg-success' : 'bg-text-muted'}`}
                aria-hidden="true"
              />
              {online ? t('workers.detail.status.online') : t('workers.detail.status.offline')}
            </span>
          </div>
          {/* #192: worker_id as a handle, full id on hover (chrome, no raw-id leak). */}
          <p className="text-xs text-text-muted">
            <span title={w.worker_id} data-testid="worker-id-handle">
              {w.worker_id}
            </span>
          </p>
        </div>
      </header>

      <nav
        className="flex gap-1 [&>button]:min-h-[44px] md:[&>button]:min-h-0"
        role="tablist"
        aria-orientation="horizontal"
        ref={tablist.tablistRef}
        onKeyDown={tablist.onKeyDown}
        onBlur={tablist.onBlur}
        data-testid="worker-tabs"
      >
        {WORKER_TABS.map((wt) => (
          <button
            key={wt.key}
            type="button"
            role="tab"
            id={`worker-tab-${wt.key}`}
            aria-selected={tab === wt.key}
            aria-controls={`worker-panel-${wt.key}`}
            tabIndex={tablist.tabIndexFor(wt.key)}
            onClick={() => setTab(wt.key)}
            data-testid={`worker-tab-${wt.key}`}
            className={`-mb-px border-b-2 px-3 py-2 text-sm font-medium ${
              tab === wt.key
                ? 'border-brand text-text-primary'
                : 'border-transparent text-text-muted hover:text-text-primary'
            }`}
          >
            {t(`workers.detail.tabs.${wt.key}`)}
          </button>
        ))}
      </nav>

      <div
        role="tabpanel"
        id={`worker-panel-${tab}`}
        aria-labelledby={`worker-tab-${tab}`}
        tabIndex={0}
        data-testid={`worker-tabpanel-${tab}`}
      >
        {/* Tab contents land in subsequent increments; Activity is v2.9. */}
        {tab === 'profile' && <WorkerProfile worker={w} />}
        {tab === 'agents' && <BoundAgents workerId={w.worker_id} />}
        {tab === 'management' && <WorkerManagement worker={w} />}
        {tab === 'activity' && (
          <EmptyState
            testId="worker-activity-stub"
            title={t('workers.detail.activity.title')}
            body={t('workers.detail.activity.body')}
          />
        )}
      </div>
    </section>
  );
}
