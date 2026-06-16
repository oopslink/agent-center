import type React from 'react';
import { useMemo, useState } from 'react';
import { useParams } from 'react-router-dom';
import {
  useReminders,
  useUpdateReminder,
  type Reminder,
  type ReminderListFilter,
} from '@/api/reminders';
import { useDisplayNameResolver } from '@/api/members';
import { EntityRef } from '@/components/EntityRef';
import { ReminderCreateModal } from '@/components/ReminderCreateModal';

// =============================================================================
// T207 [提醒-3] Reminder management — screen ① (list / management). Built against
// the real /api/orgs/{slug}/reminders API. Columns mirror the mockup: 对象 /
// 触发 / 内容 / 创建者 / 状态 / 操作 (暂停·编辑·取消); top stats (Active /
// Paused / 下次触发); left filter (全部 / 我创建的 / 状态).
// =============================================================================

const FILTERS: ReadonlyArray<{ key: ReminderListFilter; label: string }> = [
  { key: 'all', label: '全部' },
  { key: 'created', label: '我创建的' },
];

function fmtNext(r: Reminder): string {
  if (!r.next_run_at) return '—';
  const d = new Date(r.next_run_at);
  return d.toLocaleString();
}

function triggerLabel(r: Reminder): string {
  if (r.schedule.kind === 'once') return '一次性';
  return `周期 · ${r.schedule.cron_expr ?? ''}`;
}

function StatusBadge({ status }: { status: Reminder['status'] }): React.ReactElement {
  const tone =
    status === 'active'
      ? 'bg-success/15 text-success'
      : status === 'paused'
        ? 'bg-warning/15 text-warning'
        : 'bg-bg-subtle text-text-muted';
  return (
    <span className={`rounded-full px-2 py-0.5 text-xs font-semibold ${tone}`} data-testid="reminder-status">
      {status}
    </span>
  );
}

export default function Reminders(): React.ReactElement {
  const { slug } = useParams<{ slug: string }>();
  const [filter, setFilter] = useState<ReminderListFilter>('all');
  const [createOpen, setCreateOpen] = useState(false);
  const { data: reminders, isLoading, isError } = useReminders(slug, { filter });
  const displayName = useDisplayNameResolver();
  const update = useUpdateReminder(slug);

  const stats = useMemo(() => {
    const list = reminders ?? [];
    const active = list.filter((r) => r.status === 'active');
    const paused = list.filter((r) => r.status === 'paused').length;
    const next = active
      .map((r) => r.next_run_at)
      .filter((x): x is string => !!x)
      .sort()[0];
    return { active: active.length, paused, next: next ? new Date(next).toLocaleString() : '—' };
  }, [reminders]);

  return (
    <div className="flex min-h-0 flex-1 flex-col" data-testid="reminders-page">
      <header className="flex items-center justify-between border-b border-border-base px-5 py-3">
        <h1 className="text-lg font-semibold text-text-primary">Reminders</h1>
        <button
          type="button"
          onClick={() => setCreateOpen(true)}
          className="inline-flex items-center gap-1.5 rounded-md bg-brand px-3 py-1.5 text-xs font-semibold text-white hover:opacity-90"
          data-testid="reminder-new"
        >
          + 新建提醒
        </button>
      </header>

      {/* Top stats */}
      <div className="grid grid-cols-3 gap-3 px-5 py-4">
        <Stat label="Active" value={String(stats.active)} testId="stat-active" />
        <Stat label="Paused" value={String(stats.paused)} testId="stat-paused" />
        <Stat label="下次触发" value={stats.next} testId="stat-next" />
      </div>

      {/* Filter tabs */}
      <div className="flex gap-1.5 px-5 pb-3" role="tablist" aria-label="Reminder filter">
        {FILTERS.map((f) => (
          <button
            key={f.key}
            type="button"
            role="tab"
            aria-selected={filter === f.key}
            data-testid={`reminder-filter-${f.key}`}
            onClick={() => setFilter(f.key)}
            className={`rounded-full px-3 py-1 text-xs font-semibold ${
              filter === f.key ? 'bg-brand text-white' : 'bg-bg-subtle text-text-secondary hover:bg-bg-base'
            }`}
          >
            {f.label}
          </button>
        ))}
      </div>

      <div className="min-h-0 flex-1 overflow-y-auto px-5 pb-6">
        {isLoading && <p className="py-6 text-sm text-text-muted">加载中…</p>}
        {isError && <p className="py-6 text-sm text-danger">加载提醒失败。</p>}
        {!isLoading && !isError && (reminders?.length ?? 0) === 0 && (
          <p className="py-6 text-sm text-text-muted" data-testid="reminders-empty">
            还没有提醒。点「新建提醒」创建一个。
          </p>
        )}
        {(reminders?.length ?? 0) > 0 && (
          <table className="w-full text-left text-sm" data-testid="reminders-table">
            <thead className="text-xs text-text-muted">
              <tr className="border-b border-border-base">
                <th className="py-2 font-medium">对象</th>
                <th className="py-2 font-medium">触发</th>
                <th className="py-2 font-medium">内容</th>
                <th className="py-2 font-medium">创建者</th>
                <th className="py-2 font-medium">状态</th>
                <th className="py-2 font-medium text-right">操作</th>
              </tr>
            </thead>
            <tbody>
              {(reminders ?? []).map((r) => (
                <tr key={r.id} className="border-b border-border-base/60" data-testid="reminder-row" data-id={r.id}>
                  <td className="py-2 pr-3">
                    <EntityRef id={`agent:${r.remindee_agent_id}`} name={displayName(`agent:${r.remindee_agent_id}`)} />
                  </td>
                  <td className="py-2 pr-3 text-text-secondary">
                    <div>{triggerLabel(r)}</div>
                    <div className="text-xs text-text-muted">下次 {fmtNext(r)}</div>
                  </td>
                  <td className="max-w-xs truncate py-2 pr-3 text-text-primary">{r.content}</td>
                  <td className="py-2 pr-3">
                    <EntityRef id={r.creator_ref} name={displayName(r.creator_ref)} className="text-xs" />
                  </td>
                  <td className="py-2 pr-3">
                    <StatusBadge status={r.status} />
                  </td>
                  <td className="py-2 text-right">
                    <div className="inline-flex gap-2">
                      {r.status === 'active' && (
                        <button
                          type="button"
                          className="text-xs text-text-secondary hover:text-text-primary"
                          data-testid="reminder-pause"
                          onClick={() => update.mutate({ id: r.id, action: 'pause' })}
                        >
                          暂停
                        </button>
                      )}
                      {r.status === 'paused' && (
                        <button
                          type="button"
                          className="text-xs text-text-secondary hover:text-text-primary"
                          data-testid="reminder-resume"
                          onClick={() => update.mutate({ id: r.id, action: 'resume' })}
                        >
                          恢复
                        </button>
                      )}
                      {(r.status === 'active' || r.status === 'paused') && (
                        <button
                          type="button"
                          className="text-xs text-danger hover:underline"
                          data-testid="reminder-cancel"
                          onClick={() => update.mutate({ id: r.id, action: 'cancel' })}
                        >
                          取消
                        </button>
                      )}
                    </div>
                  </td>
                </tr>
              ))}
            </tbody>
          </table>
        )}
      </div>

      {createOpen && <ReminderCreateModal slug={slug} onClose={() => setCreateOpen(false)} />}
    </div>
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
