import type React from 'react';
import { useEffect, useMemo, useState } from 'react';
import { Trans, useTranslation } from 'react-i18next';
import { useParams, useSearchParams } from 'react-router-dom';
import {
  useReminders,
  useUpdateReminder,
  useDeleteReminder,
  type Reminder,
  type ReminderListFilter,
  type ReminderStatus,
} from '@/api/reminders';
import { useDisplayNameResolver } from '@/api/members';
import { Avatar } from '@/components/Avatar';
import { ReminderCreateModal, reminderToPrefill } from '@/components/ReminderCreateModal';
import { ReminderDetailModal } from '@/components/ReminderDetailModal';
import { SortHeader, Pagination, useListControls } from '@/components/listControls';
import { IconPause, IconPlay, IconClose, IconEdit, IconCopy, IconTrash } from '@/components/icons';
import { formatLocalTime } from '@/utils/time';

// =============================================================================
// T207 Reminder management — screen ① (list / management). 1:1 to the mockup
// (docs/design/v2.11.0/mockups/reminder-mockup-v0.1-I4.png): col③ is the LIST
// (Reminders · {scope} header + New reminder · Active/Paused/Next-run stats ·
// the 7-column table). Row click → detail + firing history.
//
// T248 (issue-c438cde1) three-column fix: the filter rail (search + Scope +
// Status) moved OUT of this page into col② (RemindersSecondaryNav) so the list
// occupies the middle column, not its own page-internal sidebar. The filters
// drive this list via the URL query (?range=&status=&q=) — this page READS them.
// =============================================================================

const RANGE_KEYS: ReadonlyArray<ReminderListFilter> = ['all', 'created', 'remindee'];

function relTime(t: (key: string, opts?: Record<string, unknown>) => string, iso?: string | null): string {
  if (!iso) return '';
  const hrs = Math.round((new Date(iso).getTime() - Date.now()) / 3.6e6);
  if (hrs <= 0) return t('reminders.relTime.overdue');
  if (hrs < 24) return t('reminders.relTime.hours', { hours: hrs });
  return t('reminders.relTime.days', { days: Math.round(hrs / 24) });
}

function KindBadge({ r }: { r: Reminder }): React.ReactElement {
  const { t } = useTranslation('insights');
  const kind = r.schedule.kind;
  // once = violet, on_event = accent (distinct from recurring/cron = brand), so an
  // event-driven reminder is no longer mislabeled as "Recurring".
  const cls =
    kind === 'once'
      ? 'bg-violet/15 text-violet'
      : kind === 'on_event'
        ? 'bg-accent/15 text-accent'
        : 'bg-brand/15 text-brand';
  const label = kind === 'once' ? t('reminders.kind.once') : kind === 'on_event' ? t('reminders.kind.event') : t('reminders.kind.recurring');
  return <span className={`rounded px-1.5 py-0.5 text-xs font-medium ${cls}`} data-testid="reminder-kind-badge">{label}</span>;
}

// onEventSummary renders an on_event trigger as "<entity> <event>" (+ delay when > 0),
// e.g. "task completed" or "plan failed +30s". i18n-driven for entity/event nouns.
function onEventSummary(r: Reminder, t: (k: string, o?: Record<string, unknown>) => string): string {
  const oe = r.on_event;
  if (!oe) return '—';
  const base = t('reminders.onEvent.summary', { entity: oe.entity_type, event: oe.event });
  return oe.delay_seconds > 0 ? `${base} +${oe.delay_seconds}s` : base;
}

function StatusBadge({ status }: { status: ReminderStatus }): React.ReactElement {
  // T719 defensive: was rendering the raw `status` enum (untranslated). The
  // status words already exist as reminders.statusChip.* (active/paused/
  // completed/canceled) — reuse them; the enum stays the tone/testid key.
  const { t } = useTranslation('insights');
  const tone =
    status === 'active'
      ? 'bg-success/15 text-success'
      : status === 'paused'
        ? 'bg-warning/15 text-warning'
        : 'bg-bg-subtle text-text-muted';
  return <span className={`rounded-full px-2 py-0.5 text-xs font-semibold ${tone}`} data-testid="reminder-status">{t(`reminders.statusChip.${status}`)}</span>;
}

export default function Reminders(): React.ReactElement {
  const { t } = useTranslation('insights');
  const { slug } = useParams<{ slug: string }>();
  // T248: filter state lives in the URL query, driven by col② (RemindersSecondaryNav)
  // AND the in-page status chips below (so the status filter is reachable on mobile,
  // where col② is collapsed) — both write the same ?status= param.
  const [params, setParams] = useSearchParams();
  const range = (params.get('range') as ReminderListFilter) || 'all';
  // status filter (per @oopslink): the DEFAULT view hides terminal reminders
  // (completed/canceled). '' / no param → active+paused only; 'all' → every
  // status (the explicit opt-in to see terminal); a specific status → just that.
  const statusParam = params.get('status') ?? '';
  const statuses: ReminderStatus[] | undefined =
    statusParam === ''
      ? ['active', 'paused']
      : statusParam === 'all'
        ? undefined
        : [statusParam as ReminderStatus];
  const search = params.get('q') ?? '';
  const [createOpen, setCreateOpen] = useState(false);
  const [detailId, setDetailId] = useState<string | null>(null);
  // T477 entry management: edit (PATCH in place) / clone (prefill a new create) /
  // delete (remove the entry). Each holds the target reminder; null = closed.
  const [editTarget, setEditTarget] = useState<Reminder | null>(null);
  const [cloneTarget, setCloneTarget] = useState<Reminder | null>(null);
  const [deleteTarget, setDeleteTarget] = useState<Reminder | null>(null);
  // server-side sort + pagination (per @oopslink). Default newest-first.
  const controls = useListControls({ pageSize: 25, defaultSort: 'updated_at', defaultDir: 'desc' });
  const { data, isLoading, isError } = useReminders(slug, {
    filter: range,
    statuses,
    // content search is now SERVER-side so it spans all pages, not just the loaded one.
    q: search.trim() || undefined,
    sort: controls.sort,
    dir: controls.dir,
    page: controls.page,
    page_size: controls.pageSize,
  });
  const displayName = useDisplayNameResolver();
  const update = useUpdateReminder();
  const del = useDeleteReminder();

  const rows = data?.items ?? [];
  const total = data?.total ?? 0;

  // Reset to page 1 whenever the filter/search/status changes (else you could be
  // stranded on an out-of-range page after the result count shrinks).
  const setPage = controls.setPage;
  useEffect(() => {
    setPage(1);
  }, [range, statusParam, search, setPage]);

  const stats = useMemo(() => {
    const list = rows;
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
  }, [rows]);

  const rangeKey = RANGE_KEYS.find((r) => r === range) ?? 'all';
  const rangeLabel = t(`reminders.range.${rangeKey}`);

  // In-page status filter (per @oopslink): the same ?status= param the col② nav
  // drives, surfaced on the page so it works on mobile (where col② is collapsed).
  const statusChips: { key: string; labelKey: string }[] = [
    { key: '', labelKey: 'reminders.statusChip.default' },
    { key: 'all', labelKey: 'reminders.statusChip.all' },
    { key: 'active', labelKey: 'reminders.statusChip.active' },
    { key: 'paused', labelKey: 'reminders.statusChip.paused' },
    { key: 'completed', labelKey: 'reminders.statusChip.completed' },
    { key: 'canceled', labelKey: 'reminders.statusChip.canceled' },
  ];
  const setStatus = (key: string): void => {
    const next = new URLSearchParams(params);
    if (key) next.set('status', key);
    else next.delete('status');
    setParams(next, { replace: true });
  };

  return (
    // T248: col③ (middle workspace) is the LIST only — a single column. The
    // filter rail now lives in col② (RemindersSecondaryNav). On mobile the shell
    // gives this the full screen; the filters live in the nav sheet.
    <div className="flex min-h-0 flex-1 flex-col" data-testid="reminders-page">
      <header className="flex items-center justify-between border-b border-border-base px-5 py-3">
        <h3 className="text-base font-semibold text-text-primary">{t('reminders.header', { scope: rangeLabel })}</h3>
        <button
          type="button"
          onClick={() => setCreateOpen(true)}
          className="inline-flex items-center gap-1.5 rounded-md bg-brand px-3 py-2 md:py-1.5 text-xs font-semibold text-white hover:opacity-90"
          data-testid="reminder-new"
        >
          {t('reminders.newButton')}
        </button>
      </header>

      <div className="min-h-0 flex-1 overflow-y-auto px-5 py-4">
          <div className="mb-4 grid grid-cols-3 gap-3">
            <Stat label={t('reminders.stat.active')} value={String(stats.active)} testId="stat-active" />
            <Stat label={t('reminders.stat.paused')} value={String(stats.paused)} testId="stat-paused" />
            <Stat label={t('reminders.stat.nextRun')} value={stats.next} testId="stat-next" />
          </div>

          {/* In-page status filter chips (mobile-reachable). Default "Active &
              Paused" hides terminal; "All" shows every status. */}
          <div className="mb-4 flex flex-wrap items-center gap-1.5" data-testid="reminder-status-filter">
            {statusChips.map((c) => {
              const active = statusParam === c.key;
              return (
                <button
                  key={c.key || 'default'}
                  type="button"
                  onClick={() => setStatus(c.key)}
                  aria-pressed={active}
                  data-testid={`reminder-statuschip-${c.key || 'default'}`}
                  className={`rounded-full px-2.5 py-0.5 text-xs min-h-[44px] md:min-h-0 ${
                    active ? 'bg-brand text-white' : 'bg-bg-subtle text-text-secondary hover:bg-border-base'
                  }`}
                >
                  {t(c.labelKey)}
                </button>
              );
            })}
          </div>

          {isLoading && <p className="py-6 text-sm text-text-muted">{t('reminders.loading')}</p>}
          {isError && <p className="py-6 text-sm text-danger">{t('reminders.loadError')}</p>}
          {!isLoading && !isError && rows.length === 0 && (
            <p className="py-6 text-sm text-text-muted" data-testid="reminders-empty">
              {t('reminders.empty')}
            </p>
          )}
          {rows.length > 0 && (
            // overflow-x-auto + min-w so a wide row scrolls horizontally inside
            // the container instead of overflowing the page on narrow/mobile
            // screens (mirrors the Agents table convention).
            <div className="overflow-x-auto">
            <table className="w-full min-w-[44rem] text-left text-sm" data-testid="reminders-table">
              <thead className="text-xs text-text-muted">
                <tr className="border-b border-border-base">
                  <th className="py-2 font-medium">{t('reminders.col.target')}</th>
                  <th className="py-2 font-medium">{t('reminders.col.trigger')}</th>
                  <SortHeader label={t('reminders.col.nextRun')} sortKey="next_run_at" controls={controls} className="py-2 font-medium" />
                  <th className="py-2 font-medium">{t('reminders.col.content')}</th>
                  <th className="py-2 font-medium">{t('reminders.col.creator')}</th>
                  <SortHeader label={t('reminders.col.status')} sortKey="status" controls={controls} className="py-2 font-medium" />
                  <th className="py-2 font-medium text-right">{t('reminders.col.actions')}</th>
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
                        <span className="flex min-w-0 max-w-[12rem] items-center gap-1.5">
                          <Avatar name={displayName(`agent:${r.remindee_agent_id}`)} kind="agent" size="sm" />
                          <span className="min-w-0 truncate">{displayName(`agent:${r.remindee_agent_id}`)}</span>
                        </span>
                      </td>
                      <td className="py-2 pr-3">
                        <div className="flex items-center gap-1.5">
                          <KindBadge r={r} />
                          {r.schedule.kind === 'cron' ? (
                            <span className="rounded bg-bg-subtle px-1.5 py-0.5 font-mono text-xs text-text-secondary">{r.schedule.cron_expr}</span>
                          ) : r.schedule.kind === 'on_event' ? (
                            <span className="text-xs text-text-secondary" data-testid="reminder-trigger-onevent">{onEventSummary(r, t)}</span>
                          ) : (
                            <span className="text-xs text-text-secondary">{r.schedule.once_at ? formatLocalTime(r.schedule.once_at) : '—'}</span>
                          )}
                        </div>
                      </td>
                      <td className="py-2 pr-3">
                        {r.status === 'paused' ? (
                          <span className="text-xs text-text-muted">{t('reminders.nextRunPaused')}</span>
                        ) : r.next_run_at ? (
                          <>
                            <div className="text-text-secondary">{formatLocalTime(r.next_run_at)}</div>
                            <div className="text-xs text-text-muted">{relTime(t, r.next_run_at)}</div>
                          </>
                        ) : (
                          <span className="text-xs text-text-muted">—</span>
                        )}
                      </td>
                      <td className="max-w-[16rem] truncate py-2 pr-3 text-text-primary">{r.content}</td>
                      <td className="py-2 pr-3">
                        <span className="flex min-w-0 max-w-[12rem] items-center gap-1.5">
                          <Avatar name={displayName(r.creator_ref)} kind={r.creator_ref.startsWith('agent:') ? 'agent' : 'human'} size="sm" />
                          <span className="min-w-0 truncate text-xs">{displayName(r.creator_ref)}</span>
                          {isSelf && <span className="shrink-0 text-xs text-text-muted">{t('reminders.self')}</span>}
                        </span>
                      </td>
                      <td className="py-2 pr-3">
                        <StatusBadge status={r.status} />
                      </td>
                      <td className="py-2 text-right" onClick={(e) => e.stopPropagation()}>
                        <div className="inline-flex gap-2">
                          {r.status === 'active' && (
                            <button type="button" title={t('reminders.action.pause')} aria-label={t('reminders.action.pause')} data-testid="reminder-pause" className="p-2.5 md:p-0 text-text-secondary hover:text-text-primary" onClick={() => update.mutate({ id: r.id, action: 'pause' })}>
                              <IconPause className="h-3.5 w-3.5" />
                            </button>
                          )}
                          {r.status === 'paused' && (
                            <button type="button" title={t('reminders.action.resume')} aria-label={t('reminders.action.resume')} data-testid="reminder-resume" className="p-2.5 md:p-0 text-text-secondary hover:text-text-primary" onClick={() => update.mutate({ id: r.id, action: 'resume' })}>
                              <IconPlay className="h-3.5 w-3.5" />
                            </button>
                          )}
                          {(r.status === 'active' || r.status === 'paused') && (
                            <button type="button" title={t('reminders.action.cancel')} aria-label={t('reminders.action.cancel')} data-testid="reminder-cancel" className="p-2.5 md:p-0 text-danger hover:opacity-80" onClick={() => update.mutate({ id: r.id, action: 'cancel' })}>
                              <IconClose className="h-3.5 w-3.5" />
                            </button>
                          )}
                          {/* T477: edit (active/paused only — terminal reminders
                              aren't editable), clone (any), delete (any). */}
                          {(r.status === 'active' || r.status === 'paused') && (
                            <button type="button" title={t('reminders.action.edit')} aria-label={t('reminders.action.edit')} data-testid="reminder-edit" className="p-2.5 md:p-0 text-text-secondary hover:text-text-primary" onClick={() => setEditTarget(r)}>
                              <IconEdit className="h-3.5 w-3.5" />
                            </button>
                          )}
                          <button type="button" title={t('reminders.action.clone')} aria-label={t('reminders.action.clone')} data-testid="reminder-clone" className="p-2.5 md:p-0 text-text-secondary hover:text-text-primary" onClick={() => setCloneTarget(r)}>
                            <IconCopy className="h-3.5 w-3.5" />
                          </button>
                          <button type="button" title={t('reminders.action.delete')} aria-label={t('reminders.action.delete')} data-testid="reminder-delete" className="p-2.5 md:p-0 text-danger hover:opacity-80" onClick={() => setDeleteTarget(r)}>
                            <IconTrash className="h-3.5 w-3.5" />
                          </button>
                        </div>
                      </td>
                    </tr>
                  );
                })}
              </tbody>
            </table>
            </div>
          )}
          {!isLoading && !isError && (
            <Pagination
              page={controls.page}
              pageSize={controls.pageSize}
              total={total}
              onPageChange={controls.setPage}
            />
          )}
          <p className="mt-3 text-xs text-text-muted">{t('reminders.rowHint')}</p>
        </div>

      {createOpen && <ReminderCreateModal onClose={() => setCreateOpen(false)} />}
      {/* T477 clone: open the create modal prefilled from the row (a NEW reminder). */}
      {cloneTarget && (
        <ReminderCreateModal
          prefill={reminderToPrefill(cloneTarget, displayName(`agent:${cloneTarget.remindee_agent_id}`))}
          onClose={() => setCloneTarget(null)}
        />
      )}
      {/* T477 edit: same modal in edit mode (PATCH action=edit, remindee fixed). */}
      {editTarget && (
        <ReminderCreateModal
          editId={editTarget.id}
          prefill={reminderToPrefill(editTarget, displayName(`agent:${editTarget.remindee_agent_id}`))}
          onClose={() => setEditTarget(null)}
        />
      )}
      {/* T477 delete: confirm before hard-deleting the entry. */}
      {deleteTarget && (
        <ConfirmDeleteDialog
          name={displayName(`agent:${deleteTarget.remindee_agent_id}`)}
          pending={del.isPending}
          error={del.isError ? (del.error as Error).message : null}
          onCancel={() => {
            del.reset();
            setDeleteTarget(null);
          }}
          onConfirm={() =>
            del.mutate(deleteTarget.id, {
              onSuccess: () => setDeleteTarget(null),
            })
          }
        />
      )}
      {detailId && <ReminderDetailModal slug={slug} reminderId={detailId} onClose={() => setDetailId(null)} />}
    </div>
  );
}

// ConfirmDeleteDialog — a small confirm modal for the destructive hard-delete
// (T477). Delete is distinct from Cancel: it removes the entry + its firing
// history entirely, so it's gated behind an explicit confirm.
function ConfirmDeleteDialog({
  name,
  pending,
  error,
  onConfirm,
  onCancel,
}: {
  name: string;
  pending: boolean;
  error: string | null;
  onConfirm: () => void;
  onCancel: () => void;
}): React.ReactElement {
  const { t } = useTranslation('insights');
  return (
    <div
      className="fixed inset-0 z-50 flex items-center justify-center bg-black/40 p-4"
      role="dialog"
      aria-modal="true"
      aria-label={t('reminders.delete.ariaLabel')}
      data-testid="reminder-delete-confirm"
    >
      <div className="w-full max-w-sm rounded-xl bg-bg-elevated p-5 shadow-xl">
        <h4 className="text-base font-semibold text-text-primary">{t('reminders.delete.title')}</h4>
        <p className="mt-2 text-sm text-text-secondary">
          <Trans
            t={t}
            i18nKey="reminders.delete.body"
            values={{ name }}
            components={{ name: <span className="font-medium text-text-primary" /> }}
          />
        </p>
        {error && (
          <p className="mt-2 text-xs text-danger" data-testid="reminder-delete-error">
            {error}
          </p>
        )}
        <div className="mt-4 flex justify-end gap-2">
          <button
            type="button"
            onClick={onCancel}
            className="rounded-md px-3 py-1.5 text-sm text-text-secondary hover:bg-bg-subtle"
          >
            {t('reminders.delete.cancel')}
          </button>
          <button
            type="button"
            disabled={pending}
            onClick={onConfirm}
            data-testid="reminder-delete-confirm-btn"
            className="rounded-md bg-danger px-4 py-1.5 text-sm font-semibold text-white disabled:opacity-50"
          >
            {pending ? t('reminders.delete.pending') : t('reminders.delete.confirm')}
          </button>
        </div>
      </div>
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
