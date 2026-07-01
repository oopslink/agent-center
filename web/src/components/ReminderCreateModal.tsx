import type React from 'react';
import { useMemo, useState } from 'react';
import { useTranslation } from 'react-i18next';
import {
  useCreateReminder,
  useUpdateReminder,
  type Reminder,
  type ReminderEndCondition,
  type ReminderScheduleKind,
} from '@/api/reminders';
import { useAgents } from '@/api/agents';
import { Avatar } from './Avatar';
import { EntityMultiSelect } from './EntityMultiSelect';
import type { EntityOption } from './EntitySelect';
import { IconClose, IconCalendar, IconClock } from './icons';

// =============================================================================
// T207 [提醒-3] — screens ② (新建·周期 cron) + ③ (新建·一次性 once), 1:1 to the
// mockup: 提醒对象 pills (同 project agents) · 触发方式 toggle · cron 表达式 +
// 常用预设 + 人话预览 · 一次性 日期+时间 + 预览 · 内容 · 高级(重叠跳过 + 结束条件).
// Submits to POST /api/orgs/{slug}/reminders. The remindee is an agent.
// =============================================================================

const CRON_PRESETS: ReadonlyArray<{ labelKey: string; expr: string }> = [
  { labelKey: 'reminders.create.cronPreset.hourly', expr: '0 * * * *' },
  { labelKey: 'reminders.create.cronPreset.daily0900', expr: '0 9 * * *' },
  { labelKey: 'reminders.create.cronPreset.weekdays1800', expr: '0 18 * * 1-5' },
  { labelKey: 'reminders.create.cronPreset.mondays0900', expr: '0 9 * * 1' },
  { labelKey: 'reminders.create.cronPreset.every30min', expr: '*/30 * * * *' },
];

const browserTz =
  typeof Intl !== 'undefined' ? Intl.DateTimeFormat().resolvedOptions().timeZone : 'UTC';

const WEEKDAY_KEYS = [
  'reminders.create.weekday.sunday',
  'reminders.create.weekday.monday',
  'reminders.create.weekday.tuesday',
  'reminders.create.weekday.wednesday',
  'reminders.create.weekday.thursday',
  'reminders.create.weekday.friday',
  'reminders.create.weekday.saturday',
];

// cronHuman renders a best-effort natural-language gloss for the common shapes
// the presets cover (the mockup's plain-language preview); unknown exprs fall
// back to raw.
function cronHuman(t: (key: string, opts?: Record<string, unknown>) => string, expr: string): string {
  const parts = expr.trim().split(/\s+/);
  if (parts.length !== 5) return expr;
  const [min, hr, dom, mon, dow] = parts;
  const hhmm = (h: string, m: string) => `${h.padStart(2, '0')}:${m.padStart(2, '0')}`;
  if (min === '*/30' && hr === '*') return t('reminders.create.cronHuman.every30min');
  if (hr === '*' && min === '0') return t('reminders.create.cronHuman.everyHour');
  if (dom === '*' && mon === '*') {
    const time = /^\d+$/.test(hr) && /^\d+$/.test(min) ? hhmm(hr, min) : `${hr}:${min}`;
    if (dow === '*') return t('reminders.create.cronHuman.daily', { time });
    if (dow === '1-5') return t('reminders.create.cronHuman.weekdays', { time });
    if (/^\d$/.test(dow)) return t('reminders.create.cronHuman.everyDay', { weekday: t(WEEKDAY_KEYS[Number(dow)]), time });
    return t('reminders.create.cronHuman.weekly', { dow, time });
  }
  return expr;
}

// T474: an optional prefill so callers (e.g. the agent detail sidebar's "create
// reminder" button) can open the modal with the remindee already selected and —
// when an LLM session-limit reset was detected in the agent's recent activity —
// a one-shot trigger time + content already filled in.
export interface ReminderPrefill {
  /** remindee agent ids to pre-select. */
  remindeeIds?: string[];
  /** fallback {id,name} pairs so a pre-selected remindee that isn't in the
   *  project agents list still renders a labelled chip. */
  remindeeOptions?: ReadonlyArray<{ id: string; name: string }>;
  kind?: ReminderScheduleKind;
  onceDate?: string; // YYYY-MM-DD
  onceTime?: string; // HH:MM
  cronExpr?: string; // cron expression (recurring)
  tz?: string; // IANA tz (cron preview / payload)
  content?: string;
}

// reminderToPrefill maps an existing reminder onto a create/edit prefill — used by
// the list-row "Clone" (open create prefilled → modify → create a NEW one) and
// "Edit" (modify in place) actions (T477). A once `once_at` instant is rendered to
// LOCAL date/time inputs so it round-trips to the same instant on submit.
export function reminderToPrefill(r: Reminder, remindeeName?: string): ReminderPrefill {
  const p: ReminderPrefill = {
    remindeeIds: [r.remindee_agent_id],
    remindeeOptions: remindeeName ? [{ id: r.remindee_agent_id, name: remindeeName }] : undefined,
    kind: r.schedule.kind,
    content: r.content,
  };
  if (r.schedule.kind === 'cron') {
    p.cronExpr = r.schedule.cron_expr;
    p.tz = r.schedule.timezone;
  } else if (r.schedule.once_at) {
    const d = new Date(r.schedule.once_at);
    if (!Number.isNaN(d.getTime())) {
      const pad = (n: number) => String(n).padStart(2, '0');
      p.onceDate = `${d.getFullYear()}-${pad(d.getMonth() + 1)}-${pad(d.getDate())}`;
      p.onceTime = `${pad(d.getHours())}:${pad(d.getMinutes())}`;
    }
  }
  return p;
}

interface Props {
  onClose: () => void;
  prefill?: ReminderPrefill;
  /** T477: when set, the modal EDITS this reminder in place (PATCH action=edit)
   *  instead of creating new ones; the remindee is fixed (edit can't retarget). */
  editId?: string;
}

export function ReminderCreateModal({ onClose, prefill, editId }: Props): React.ReactElement {
  const { t } = useTranslation('insights');
  const create = useCreateReminder();
  const update = useUpdateReminder();
  const isEdit = !!editId;
  const { data: agents } = useAgents();
  const [kind, setKind] = useState<ReminderScheduleKind>(prefill?.kind ?? 'cron');
  // Multi-select: a reminder can target several peer agents; on submit we fan out
  // one create call per remindee (the API takes a single remindee_agent_id).
  const [remindees, setRemindees] = useState<string[]>(prefill?.remindeeIds ?? []);
  const [content, setContent] = useState(prefill?.content ?? '');
  const [cronExpr, setCronExpr] = useState(prefill?.cronExpr ?? '0 18 * * 1-5');
  const [tz, setTz] = useState(prefill?.tz ?? browserTz);
  const [onceDate, setOnceDate] = useState(prefill?.onceDate ?? '');
  const [onceTime, setOnceTime] = useState(prefill?.onceTime ?? '09:00');
  const [skipOverlap, setSkipOverlap] = useState(true);
  const [deliverAsCreator, setDeliverAsCreator] = useState(true); // F-B: default ON per mockup
  const [endKind, setEndKind] = useState<ReminderEndCondition['kind']>('never');
  const [err, setErr] = useState<string | null>(null);

  const canSubmit =
    remindees.length > 0 &&
    content.trim() !== '' &&
    (kind === 'cron' ? cronExpr.trim() !== '' : onceDate !== '');

  const remindeeOptions = useMemo<EntityOption[]>(() => {
    const opts: EntityOption[] = (agents ?? []).map((a) => ({
      value: a.id,
      label: a.name,
      leading: <Avatar name={a.name} kind="agent" size="sm" />,
    }));
    // Inject any prefilled remindee that the project agents list doesn't carry
    // (so its chip still renders with a real label, not a bare id).
    const known = new Set(opts.map((o) => o.value));
    for (const f of prefill?.remindeeOptions ?? []) {
      if (!known.has(f.id)) {
        opts.push({
          value: f.id,
          label: f.name,
          leading: <Avatar name={f.name} kind="agent" size="sm" />,
        });
        known.add(f.id);
      }
    }
    return opts;
  }, [agents, prefill]);

  const oncePreview = useMemo(() => {
    if (!onceDate) return '—';
    const dt = new Date(`${onceDate}T${onceTime}:00`);
    const hrs = Math.round((dt.getTime() - Date.now()) / 3.6e6);
    const rel = hrs > 0 ? t('reminders.create.oncePreviewRel', { hours: hrs }) : t('reminders.create.oncePreviewOverdue');
    return t('reminders.create.oncePreview', { date: onceDate, time: onceTime, rel, tz });
  }, [onceDate, onceTime, tz, t]);

  async function submit(): Promise<void> {
    setErr(null);
    const schedule =
      kind === 'cron'
        ? { kind: 'cron' as const, cron_expr: cronExpr.trim(), timezone: tz }
        : { kind: 'once' as const, once_at: new Date(`${onceDate}T${onceTime}:00`).toISOString() };
    const end_condition: ReminderEndCondition = { kind: endKind };

    // T477 edit: PATCH the existing reminder's schedule + content in place (the
    // remindee is fixed — edit can't retarget). No fan-out, no end-condition/
    // overlap re-send (those aren't part of the edit contract).
    if (isEdit && editId) {
      try {
        await update.mutateAsync({ id: editId, action: 'edit', schedule, content: content.trim() });
        onClose();
      } catch (e) {
        setErr(e instanceof Error ? e.message : t('reminders.create.errFailedSave'));
      }
      return;
    }

    // Fan out one create per remindee; report a partial-failure summary rather
    // than aborting the rest (mirrors the batch-lifecycle pattern). The API
    // takes a single remindee_agent_id, so multi-target is a client-side loop.
    const results = await Promise.allSettled(
      remindees.map((id) =>
        create.mutateAsync({
          remindee_agent_id: id,
          schedule,
          content: content.trim(),
          skip_if_overlap: skipOverlap,
          deliver_as_creator: deliverAsCreator,
          end_condition,
        }),
      ),
    );
    const failed = results.filter((r): r is PromiseRejectedResult => r.status === 'rejected');
    if (failed.length === 0) {
      onClose();
      return;
    }
    const msg = failed[0].reason instanceof Error ? failed[0].reason.message : t('reminders.create.errFailedCreate');
    setErr(
      failed.length === remindees.length
        ? msg
        : t('reminders.create.errPartial', { failed: failed.length, total: remindees.length, msg }),
    );
  }

  return (
    <div
      className="fixed inset-0 z-50 flex items-center justify-center bg-black/40 p-4"
      role="dialog"
      aria-modal="true"
      aria-label={isEdit ? t('reminders.create.titleEdit') : t('reminders.create.titleNew')}
      data-testid="reminder-create-modal"
    >
      <div className="flex max-h-[88vh] w-full max-w-lg flex-col rounded-xl bg-bg-elevated shadow-xl">
        <div className="flex items-center justify-between border-b border-border-base px-5 py-3">
          <h4 className="text-base font-semibold text-text-primary">{isEdit ? t('reminders.create.titleEdit') : t('reminders.create.titleNew')}</h4>
          <button type="button" onClick={onClose} className="text-text-muted hover:text-text-primary" aria-label={t('reminders.create.close')}>
            <IconClose className="h-4 w-4" />
          </button>
        </div>

        <div className="min-h-0 flex-1 space-y-4 overflow-y-auto px-5 py-4">
          {/* Remindee — searchable multi-select dropdown with removable chips
              (UX standards §1 / §1a; no toggle-pill grid, no bare checkboxes).
              In edit mode the remindee is fixed (the edit contract is schedule +
              content only), so the select is disabled. */}
          <div>
            <label className="mb-1.5 block text-xs font-medium text-text-secondary">{t('reminders.create.remindeeLabel')}</label>
            <EntityMultiSelect
              testId="reminder-remindee"
              options={remindeeOptions}
              values={remindees}
              onChange={setRemindees}
              placeholder={t('reminders.create.remindeePlaceholder')}
              searchPlaceholder={t('reminders.create.remindeeSearchPlaceholder')}
              emptyLabel={t('reminders.create.remindeeEmpty')}
              ariaLabel={t('reminders.create.remindeeLabel')}
              disabled={isEdit}
            />
            <p className="mt-1.5 text-xs text-text-muted">
              {isEdit
                ? t('reminders.create.remindeeHintEdit')
                : t('reminders.create.remindeeHint')}
            </p>
          </div>

          {/* Trigger type */}
          <div>
            <label className="mb-1.5 block text-xs font-medium text-text-secondary">{t('reminders.create.triggerLabel')}</label>
            <div className="inline-flex rounded-md bg-bg-subtle p-0.5" role="tablist" aria-label={t('reminders.create.triggerTypeAria')}>
              {(['once', 'cron'] as const).map((k) => (
                <button
                  key={k}
                  type="button"
                  role="tab"
                  aria-selected={kind === k}
                  data-testid={`reminder-kind-${k}`}
                  onClick={() => setKind(k)}
                  className={`rounded px-3 py-1 text-xs font-semibold ${kind === k ? 'bg-brand text-white' : 'text-text-secondary'}`}
                >
                  {k === 'once' ? t('reminders.create.tabOnce') : t('reminders.create.tabRecurring')}
                </button>
              ))}
            </div>
          </div>

          {kind === 'cron' ? (
            <>
              <div>
                <label className="mb-1.5 block text-xs font-medium text-text-secondary">{t('reminders.create.cronLabel')}</label>
                <input
                  value={cronExpr}
                  onChange={(e) => setCronExpr(e.target.value)}
                  placeholder={t('reminders.create.cronPlaceholder')}
                  className="w-full rounded-md border border-border-base bg-bg-base px-3 py-2 font-mono text-sm"
                  data-testid="reminder-cron"
                />
                <div className="mt-2 flex flex-wrap gap-1.5">
                  {CRON_PRESETS.map((p) => (
                    <button
                      key={p.expr}
                      type="button"
                      onClick={() => setCronExpr(p.expr)}
                      aria-pressed={cronExpr === p.expr}
                      className={`rounded-full px-2.5 py-1 text-xs ${
                        cronExpr === p.expr ? 'bg-brand text-white' : 'bg-bg-subtle text-text-secondary hover:bg-bg-base'
                      }`}
                    >
                      {t(p.labelKey)}
                    </button>
                  ))}
                </div>
                <div
                  className="mt-2.5 flex items-center gap-2 rounded-lg border border-info/30 bg-info/10 px-3 py-2 text-xs text-info"
                  data-testid="reminder-preview"
                >
                  <IconCalendar className="h-3.5 w-3.5 shrink-0" /> <span>{t('reminders.create.cronPreview', { human: cronHuman(t, cronExpr), tz })}</span>
                </div>
                <input
                  value={tz}
                  onChange={(e) => setTz(e.target.value)}
                  aria-label={t('reminders.create.timezoneAria')}
                  className="mt-2 w-full rounded-md border border-border-base bg-bg-base px-3 py-2 text-sm"
                />
              </div>
              {/* Advanced (create-only — skip-overlap + end-condition aren't part
                  of the edit contract, so hidden when editing). */}
              {!isEdit && (
              <div className="space-y-2 rounded-lg border border-border-base p-3">
                <div className="flex items-center justify-between gap-2 text-xs text-text-secondary">
                  <span>
                    {t('reminders.create.skipOverlapLabel')}
                    <span className="block text-text-muted">{t('reminders.create.skipOverlapHint')}</span>
                  </span>
                  {/* Toggle switch, not a checkbox (UX standards §1a). */}
                  <button
                    type="button"
                    role="switch"
                    aria-checked={skipOverlap}
                    aria-label={t('reminders.create.skipOverlapLabel')}
                    onClick={() => setSkipOverlap((v) => !v)}
                    data-testid="reminder-skip-overlap"
                    className={`relative inline-flex h-5 w-9 shrink-0 items-center rounded-full transition-colors ${
                      skipOverlap ? 'bg-brand' : 'bg-border-strong'
                    }`}
                  >
                    <span
                      className={`inline-block h-4 w-4 transform rounded-full bg-white shadow transition-transform ${
                        skipOverlap ? 'translate-x-4' : 'translate-x-0.5'
                      }`}
                    />
                  </button>
                </div>
                <label className="flex items-center justify-between gap-2 text-xs text-text-secondary">
                  <span>
                    {t('reminders.create.endConditionLabel')}
                    <span className="block text-text-muted">{t('reminders.create.endConditionHint')}</span>
                  </span>
                  <select
                    value={endKind}
                    onChange={(e) => setEndKind(e.target.value as ReminderEndCondition['kind'])}
                    className="rounded-md border border-border-base bg-bg-base px-2 py-1 text-xs"
                    data-testid="reminder-end-kind"
                  >
                    <option value="never">{t('reminders.create.endNever')}</option>
                    <option value="until">{t('reminders.create.endUntil')}</option>
                    <option value="max_count">{t('reminders.create.endMaxCount')}</option>
                  </select>
                </label>
              </div>
              )}
            </>
          ) : (
            <div>
              <label className="mb-1.5 block text-xs font-medium text-text-secondary">{t('reminders.create.triggerTimeLabel')}</label>
              <div className="flex gap-2">
                <input
                  type="date"
                  value={onceDate}
                  onChange={(e) => setOnceDate(e.target.value)}
                  className="flex-1 rounded-md border border-border-base bg-bg-base px-3 py-2 text-sm"
                  data-testid="reminder-once-date"
                />
                <input
                  type="time"
                  value={onceTime}
                  onChange={(e) => setOnceTime(e.target.value)}
                  className="rounded-md border border-border-base bg-bg-base px-3 py-2 font-mono text-sm"
                  data-testid="reminder-once-time"
                />
              </div>
              <div
                className="mt-2.5 flex items-center gap-2 rounded-lg border border-info/30 bg-info/10 px-3 py-2 text-xs text-info"
                data-testid="reminder-preview"
              >
                <IconClock className="h-3.5 w-3.5 shrink-0" /> <span>{oncePreview}</span>
              </div>
            </div>
          )}

          {/* Content */}
          <div>
            <label className="mb-1.5 block text-xs font-medium text-text-secondary">{t('reminders.create.contentLabel')}</label>
            <textarea
              value={content}
              onChange={(e) => setContent(e.target.value)}
              rows={2}
              placeholder={t('reminders.create.contentPlaceholder')}
              className="w-full rounded-md border border-border-base bg-bg-base px-3 py-2 text-sm"
              data-testid="reminder-content"
            />
          </div>

          {/* Send as yourself (F-B) — brand toggle, on by default. Create-only:
              the edit contract is schedule + content, so it's hidden when editing
              (delivery identity is fixed at creation). */}
          {!isEdit && (
            <div className="flex items-center justify-between gap-3">
              <div className="text-xs text-text-secondary">
                {t('reminders.create.sendAsSelfLabel')}
                <span className="block text-text-muted">{t('reminders.create.sendAsSelfHint')}</span>
              </div>
              <button
                type="button"
                role="switch"
                aria-checked={deliverAsCreator}
                aria-label={t('reminders.create.sendAsSelfLabel')}
                onClick={() => setDeliverAsCreator((v) => !v)}
                data-testid="reminder-deliver-as-creator"
                className={`relative inline-flex h-5 w-9 shrink-0 items-center rounded-full transition-colors ${
                  deliverAsCreator ? 'bg-brand' : 'bg-border-strong'
                }`}
              >
                <span
                  className={`inline-block h-4 w-4 transform rounded-full bg-white shadow transition-transform ${
                    deliverAsCreator ? 'translate-x-4' : 'translate-x-0.5'
                  }`}
                />
              </button>
            </div>
          )}

          {err && (
            <p className="text-xs text-danger" data-testid="reminder-error">
              {err}
            </p>
          )}
        </div>

        <div className="flex items-center justify-between border-t border-border-base px-5 py-3">
          <p className="text-xs text-text-muted">{t('reminders.create.footerNote')}</p>
          <div className="flex gap-2">
            <button type="button" onClick={onClose} className="rounded-md px-3 py-1.5 text-sm text-text-secondary hover:bg-bg-subtle">
              {t('reminders.create.cancel')}
            </button>
            <button
              type="button"
              disabled={!canSubmit || create.isPending || update.isPending}
              onClick={() => void submit()}
              className="rounded-md bg-brand px-4 py-1.5 text-sm font-semibold text-white disabled:opacity-50"
              data-testid="reminder-submit"
            >
              {isEdit
                ? update.isPending
                  ? t('reminders.create.saving')
                  : t('reminders.create.saveChanges')
                : create.isPending
                  ? t('reminders.create.creating')
                  : t('reminders.create.createButton')}
            </button>
          </div>
        </div>
      </div>
    </div>
  );
}
