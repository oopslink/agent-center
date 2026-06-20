import type React from 'react';
import { useMemo, useState } from 'react';
import { useParams } from 'react-router-dom';
import {
  useReminders,
  useUpdateReminder,
  type Reminder,
  type ReminderListFilter,
  type ReminderStatus,
} from '@/api/reminders';
import { useDisplayNameResolver } from '@/api/members';
import { Avatar } from '@/components/Avatar';
import { ReminderCreateModal } from '@/components/ReminderCreateModal';
import { ReminderDetailModal } from '@/components/ReminderDetailModal';
import { IconSearch, IconPause, IconPlay, IconClose } from '@/components/icons';

// =============================================================================
// T207 [提醒-3] Reminder management — screen ① (list / management), 1:1 to the
// mockup: left filter rail (搜索 + 范围 全部/我创建的 + 状态 Active/Paused) ·
// main (提醒 · {范围} header + 新建提醒 · Active/Paused/下次触发 统计 ·
// 7-列表 对象/触发/下次触发/内容/创建者/状态/操作). Row click → 详情 + 历史触发.
// =============================================================================

const RANGES: ReadonlyArray<{ key: ReminderListFilter; label: string }> = [
  { key: 'all', label: 'All' },
  { key: 'created', label: 'Created by me' },
  { key: 'remindee', label: 'Reminding me' },
];
const STATUSES: ReadonlyArray<{ key: ReminderStatus; label: string }> = [
  { key: 'active', label: 'Active' },
  { key: 'paused', label: 'Paused' },
];

function relTime(iso?: string | null): string {
  if (!iso) return '';
  const hrs = Math.round((new Date(iso).getTime() - Date.now()) / 3.6e6);
  if (hrs <= 0) return 'Overdue';
  if (hrs < 24) return `in ~${hrs}h`;
  return `in ~${Math.round(hrs / 24)}d`;
}

function KindBadge({ r }: { r: Reminder }): React.ReactElement {
  const once = r.schedule.kind === 'once';
  return (
    <span className={`rounded px-1.5 py-0.5 text-xs font-medium ${once ? 'bg-violet/15 text-violet' : 'bg-brand/15 text-brand'}`}>
      {once ? 'Once' : 'Recurring'}
    </span>
  );
}

function StatusBadge({ status }: { status: ReminderStatus }): React.ReactElement {
  const tone =
    status === 'active'
      ? 'bg-success/15 text-success'
      : status === 'paused'
        ? 'bg-warning/15 text-warning'
        : 'bg-bg-subtle text-text-muted';
  return <span className={`rounded-full px-2 py-0.5 text-xs font-semibold ${tone}`} data-testid="reminder-status">{status}</span>;
}

export default function Reminders(): React.ReactElement {
  const { slug } = useParams<{ slug: string }>();
  const [range, setRange] = useState<ReminderListFilter>('all');
  const [statusFilter, setStatusFilter] = useState<ReminderStatus | null>(null);
  const [search, setSearch] = useState('');
  const [createOpen, setCreateOpen] = useState(false);
  const [detailId, setDetailId] = useState<string | null>(null);
  const { data: reminders, isLoading, isError } = useReminders(slug, {
    filter: range,
    statuses: statusFilter ? [statusFilter] : undefined,
  });
  const displayName = useDisplayNameResolver();
  const update = useUpdateReminder();

  const rows = useMemo(() => {
    const list = reminders ?? [];
    const q = search.trim().toLowerCase();
    return q ? list.filter((r) => r.content.toLowerCase().includes(q)) : list;
  }, [reminders, search]);

  const stats = useMemo(() => {
    const list = reminders ?? [];
    const active = list.filter((r) => r.status === 'active');
    const next = active
      .map((r) => r.next_run_at)
      .filter((x): x is string => !!x)
      .sort()[0];
    return {
      active: active.length,
      paused: list.filter((r) => r.status === 'paused').length,
      next: next ? new Date(next).toLocaleTimeString([], { hour: '2-digit', minute: '2-digit' }) : '—',
    };
  }, [reminders]);

  const rangeLabel = RANGES.find((r) => r.key === range)?.label ?? 'All';

  return (
    <div className="flex min-h-0 flex-1" data-testid="reminders-page">
      {/* Left filter rail (mockup col②) */}
      <aside className="flex w-52 shrink-0 flex-col border-r border-border-base p-3">
        <h2 className="px-1 pb-2 text-sm font-semibold text-text-primary">Reminders</h2>
        <div className="relative mb-3">
          <IconSearch className="pointer-events-none absolute left-2 top-1/2 h-3.5 w-3.5 -translate-y-1/2 text-text-muted" />
          <input
            value={search}
            onChange={(e) => setSearch(e.target.value)}
            placeholder="Search reminders…"
            className="w-full rounded-md border border-border-base bg-bg-base py-1.5 pl-7 pr-2.5 text-xs"
            data-testid="reminder-search"
          />
        </div>
        <FilterGroup label="Scope">
          {RANGES.map((rg) => (
            <FilterItem key={rg.key} active={range === rg.key} onClick={() => setRange(rg.key)} testId={`reminder-range-${rg.key}`}>
              {rg.label}
            </FilterItem>
          ))}
        </FilterGroup>
        <FilterGroup label="Status">
          <FilterItem active={statusFilter === null} onClick={() => setStatusFilter(null)} testId="reminder-status-all">
            All statuses
          </FilterItem>
          {STATUSES.map((st) => (
            <FilterItem
              key={st.key}
              active={statusFilter === st.key}
              onClick={() => setStatusFilter(st.key)}
              testId={`reminder-status-${st.key}`}
              dot={st.key === 'active' ? 'bg-success' : 'bg-warning'}
            >
              {st.label}
            </FilterItem>
          ))}
        </FilterGroup>
      </aside>

      {/* Main */}
      <div className="flex min-h-0 flex-1 flex-col">
        <header className="flex items-center justify-between border-b border-border-base px-5 py-3">
          <h3 className="text-base font-semibold text-text-primary">Reminders · {rangeLabel}</h3>
          <button
            type="button"
            onClick={() => setCreateOpen(true)}
            className="inline-flex items-center gap-1.5 rounded-md bg-brand px-3 py-1.5 text-xs font-semibold text-white hover:opacity-90"
            data-testid="reminder-new"
          >
            + New reminder
          </button>
        </header>

        <div className="min-h-0 flex-1 overflow-y-auto px-5 py-4">
          <div className="mb-4 grid grid-cols-3 gap-3">
            <Stat label="Active" value={String(stats.active)} testId="stat-active" />
            <Stat label="Paused" value={String(stats.paused)} testId="stat-paused" />
            <Stat label="Next run" value={stats.next} testId="stat-next" />
          </div>

          {isLoading && <p className="py-6 text-sm text-text-muted">Loading…</p>}
          {isError && <p className="py-6 text-sm text-danger">Failed to load reminders.</p>}
          {!isLoading && !isError && rows.length === 0 && (
            <p className="py-6 text-sm text-text-muted" data-testid="reminders-empty">
              No reminders yet. Click “New reminder” to create one.
            </p>
          )}
          {rows.length > 0 && (
            <table className="w-full text-left text-sm" data-testid="reminders-table">
              <thead className="text-xs text-text-muted">
                <tr className="border-b border-border-base">
                  <th className="py-2 font-medium">Target</th>
                  <th className="py-2 font-medium">Trigger</th>
                  <th className="py-2 font-medium">Next run</th>
                  <th className="py-2 font-medium">Content</th>
                  <th className="py-2 font-medium">Creator</th>
                  <th className="py-2 font-medium">Status</th>
                  <th className="py-2 font-medium text-right">Actions</th>
                </tr>
              </thead>
              <tbody>
                {rows.map((r) => {
                  const isSelf = r.creator_ref === `agent:${r.remindee_agent_id}`;
                  return (
                    <tr
                      key={r.id}
                      className="cursor-pointer border-b border-border-base/60 hover:bg-bg-subtle/50"
                      data-testid="reminder-row"
                      data-id={r.id}
                      onClick={() => setDetailId(r.id)}
                    >
                      <td className="py-2 pr-3">
                        <span className="inline-flex items-center gap-1.5">
                          <Avatar name={displayName(`agent:${r.remindee_agent_id}`)} kind="agent" size="sm" />
                          <span className="truncate">{displayName(`agent:${r.remindee_agent_id}`)}</span>
                        </span>
                      </td>
                      <td className="py-2 pr-3">
                        <div className="flex items-center gap-1.5">
                          <KindBadge r={r} />
                          {r.schedule.kind === 'cron' ? (
                            <span className="rounded bg-bg-subtle px-1.5 py-0.5 font-mono text-xs text-text-secondary">{r.schedule.cron_expr}</span>
                          ) : (
                            <span className="text-xs text-text-secondary">{r.schedule.once_at?.slice(0, 16).replace('T', ' ')}</span>
                          )}
                        </div>
                      </td>
                      <td className="py-2 pr-3">
                        {r.status === 'paused' ? (
                          <span className="text-xs text-text-muted">— Paused</span>
                        ) : r.next_run_at ? (
                          <>
                            <div className="text-text-secondary">{new Date(r.next_run_at).toLocaleString()}</div>
                            <div className="text-xs text-text-muted">{relTime(r.next_run_at)}</div>
                          </>
                        ) : (
                          <span className="text-xs text-text-muted">—</span>
                        )}
                      </td>
                      <td className="max-w-[16rem] truncate py-2 pr-3 text-text-primary">{r.content}</td>
                      <td className="py-2 pr-3">
                        <span className="inline-flex items-center gap-1.5">
                          <Avatar name={displayName(r.creator_ref)} kind={r.creator_ref.startsWith('agent:') ? 'agent' : 'human'} size="sm" />
                          <span className="truncate text-xs">{displayName(r.creator_ref)}</span>
                          {isSelf && <span className="text-xs text-text-muted">(self)</span>}
                        </span>
                      </td>
                      <td className="py-2 pr-3">
                        <StatusBadge status={r.status} />
                      </td>
                      <td className="py-2 text-right" onClick={(e) => e.stopPropagation()}>
                        <div className="inline-flex gap-2">
                          {r.status === 'active' && (
                            <button type="button" title="Pause" aria-label="Pause" data-testid="reminder-pause" className="text-text-secondary hover:text-text-primary" onClick={() => update.mutate({ id: r.id, action: 'pause' })}>
                              <IconPause className="h-3.5 w-3.5" />
                            </button>
                          )}
                          {r.status === 'paused' && (
                            <button type="button" title="Resume" aria-label="Resume" data-testid="reminder-resume" className="text-text-secondary hover:text-text-primary" onClick={() => update.mutate({ id: r.id, action: 'resume' })}>
                              <IconPlay className="h-3.5 w-3.5" />
                            </button>
                          )}
                          {(r.status === 'active' || r.status === 'paused') && (
                            <button type="button" title="Cancel" aria-label="Cancel" data-testid="reminder-cancel" className="text-danger hover:opacity-80" onClick={() => update.mutate({ id: r.id, action: 'cancel' })}>
                              <IconClose className="h-3.5 w-3.5" />
                            </button>
                          )}
                        </div>
                      </td>
                    </tr>
                  );
                })}
              </tbody>
            </table>
          )}
          <p className="mt-3 text-xs text-text-muted">Click a row for details + firing history (each fire time, whether delivered, and whether skipped due to overlap).</p>
        </div>
      </div>

      {createOpen && <ReminderCreateModal onClose={() => setCreateOpen(false)} />}
      {detailId && <ReminderDetailModal slug={slug} reminderId={detailId} onClose={() => setDetailId(null)} />}
    </div>
  );
}

function FilterGroup({ label, children }: { label: string; children: React.ReactNode }): React.ReactElement {
  return (
    <div className="mb-3">
      <div className="px-1 pb-1 text-[0.6875rem] font-semibold uppercase tracking-wider text-text-muted">{label}</div>
      <div className="space-y-0.5">{children}</div>
    </div>
  );
}

function FilterItem({
  active,
  onClick,
  testId,
  dot,
  children,
}: {
  active: boolean;
  onClick: () => void;
  testId: string;
  dot?: string;
  children: React.ReactNode;
}): React.ReactElement {
  return (
    <button
      type="button"
      onClick={onClick}
      aria-pressed={active}
      data-testid={testId}
      className={`flex w-full items-center gap-2 rounded-md px-2 py-1.5 text-left text-xs ${
        active ? 'bg-brand/10 font-semibold text-brand' : 'text-text-secondary hover:bg-bg-subtle'
      }`}
    >
      {dot && <span className={`h-1.5 w-1.5 rounded-full ${dot}`} />}
      {children}
    </button>
  );
}

function Stat({ label, value, testId }: { label: string; value: string; testId: string }): React.ReactElement {
  return (
    <div className="rounded-lg border border-border-base bg-bg-elevated px-4 py-3" data-testid={testId}>
      <div className="text-2xl font-bold tabular-nums text-text-primary">{value}</div>
      <div className="text-xs text-text-muted">{label}</div>
    </div>
  );
}
