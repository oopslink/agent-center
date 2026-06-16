import type React from 'react';
import { useState } from 'react';
import { useCreateReminder, type ReminderScheduleKind } from '@/api/reminders';

// =============================================================================
// T207 [提醒-3] — screens ② (新建·周期 cron) + ③ (新建·一次性 once). A modal that
// toggles between the two schedule shapes and submits to the real
// POST /api/orgs/{slug}/reminders. Cron presets + the "人话" preview keep the
// mockup's affordances; the remindee is an agent id (the agent the reminder fires
// to). Object-picker chips + full cron field-grid are a follow-up polish pass.
// =============================================================================

const CRON_PRESETS: ReadonlyArray<{ label: string; expr: string }> = [
  { label: '每天 9:00', expr: '0 9 * * *' },
  { label: '每周一 9:00', expr: '0 9 * * 1' },
  { label: '每工作日 18:00', expr: '0 18 * * 1-5' },
  { label: '每小时', expr: '0 * * * *' },
];

const browserTz =
  typeof Intl !== 'undefined' ? Intl.DateTimeFormat().resolvedOptions().timeZone : 'UTC';

interface Props {
  slug: string | undefined;
  onClose: () => void;
}

export function ReminderCreateModal({ slug, onClose }: Props): React.ReactElement {
  const create = useCreateReminder(slug);
  const [kind, setKind] = useState<ReminderScheduleKind>('cron');
  const [remindee, setRemindee] = useState('');
  const [content, setContent] = useState('');
  const [cronExpr, setCronExpr] = useState('0 9 * * 1');
  const [tz, setTz] = useState(browserTz);
  const [onceDate, setOnceDate] = useState('');
  const [onceTime, setOnceTime] = useState('09:00');
  const [skipOverlap, setSkipOverlap] = useState(true);
  const [err, setErr] = useState<string | null>(null);

  const canSubmit =
    remindee.trim() !== '' &&
    content.trim() !== '' &&
    (kind === 'cron' ? cronExpr.trim() !== '' : onceDate !== '');

  function submit(): void {
    setErr(null);
    const schedule =
      kind === 'cron'
        ? { kind: 'cron' as const, cron_expr: cronExpr.trim(), timezone: tz }
        : { kind: 'once' as const, once_at: new Date(`${onceDate}T${onceTime}:00`).toISOString() };
    create.mutate(
      {
        remindee_agent_id: remindee.trim(),
        schedule,
        content: content.trim(),
        skip_if_overlap: skipOverlap,
      },
      {
        onSuccess: () => onClose(),
        onError: (e) => setErr(e instanceof Error ? e.message : '创建失败'),
      },
    );
  }

  return (
    <div
      className="fixed inset-0 z-50 flex items-center justify-center bg-black/40 p-4"
      role="dialog"
      aria-modal="true"
      aria-label="新建提醒"
      data-testid="reminder-create-modal"
    >
      <div className="w-full max-w-lg rounded-xl bg-bg-elevated p-5 shadow-xl">
        <div className="mb-4 flex items-center justify-between">
          <h2 className="text-base font-semibold text-text-primary">新建提醒</h2>
          <button type="button" onClick={onClose} className="text-text-muted hover:text-text-primary" aria-label="关闭">
            ✕
          </button>
        </div>

        {/* 提醒对象 */}
        <label className="mb-1 block text-xs font-medium text-text-secondary">提醒对象（agent id）</label>
        <input
          value={remindee}
          onChange={(e) => setRemindee(e.target.value)}
          placeholder="例如 agent-xxxx"
          className="mb-3 w-full rounded-md border border-border-base bg-bg-base px-3 py-2 text-sm"
          data-testid="reminder-remindee"
        />

        {/* 触发方式切换 */}
        <div className="mb-3 inline-flex rounded-md bg-bg-subtle p-0.5" role="tablist" aria-label="触发方式">
          {(['once', 'cron'] as const).map((k) => (
            <button
              key={k}
              type="button"
              role="tab"
              aria-selected={kind === k}
              data-testid={`reminder-kind-${k}`}
              onClick={() => setKind(k)}
              className={`rounded px-3 py-1 text-xs font-semibold ${
                kind === k ? 'bg-brand text-white' : 'text-text-secondary'
              }`}
            >
              {k === 'once' ? '一次性' : '周期 (cron)'}
            </button>
          ))}
        </div>

        {kind === 'cron' ? (
          <div className="mb-3 space-y-2">
            <input
              value={cronExpr}
              onChange={(e) => setCronExpr(e.target.value)}
              placeholder="分 时 日 月 周  (e.g. 0 9 * * 1)"
              className="w-full rounded-md border border-border-base bg-bg-base px-3 py-2 font-mono text-sm"
              data-testid="reminder-cron"
            />
            <div className="flex flex-wrap gap-1.5">
              {CRON_PRESETS.map((p) => (
                <button
                  key={p.expr}
                  type="button"
                  onClick={() => setCronExpr(p.expr)}
                  className="rounded-full bg-bg-subtle px-2.5 py-1 text-xs text-text-secondary hover:bg-bg-base"
                >
                  {p.label}
                </button>
              ))}
            </div>
            <div className="rounded-md bg-bg-subtle px-3 py-2 text-xs text-text-secondary" data-testid="reminder-preview">
              cron <span className="font-mono">{cronExpr}</span> · 时区 {tz}
            </div>
            <input
              value={tz}
              onChange={(e) => setTz(e.target.value)}
              className="w-full rounded-md border border-border-base bg-bg-base px-3 py-2 text-sm"
              aria-label="时区"
            />
            <label className="flex items-center gap-2 text-xs text-text-secondary">
              <input
                type="checkbox"
                checked={skipOverlap}
                onChange={(e) => setSkipOverlap(e.target.checked)}
                data-testid="reminder-skip-overlap"
              />
              上次未完成则跳过本次
            </label>
          </div>
        ) : (
          <div className="mb-3 space-y-2">
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
                className="rounded-md border border-border-base bg-bg-base px-3 py-2 text-sm"
                data-testid="reminder-once-time"
              />
            </div>
            <div className="rounded-md bg-bg-subtle px-3 py-2 text-xs text-text-secondary" data-testid="reminder-preview">
              一次 · {onceDate || '—'} {onceTime} · 时区 {tz}
            </div>
          </div>
        )}

        <label className="mb-1 block text-xs font-medium text-text-secondary">提醒内容</label>
        <textarea
          value={content}
          onChange={(e) => setContent(e.target.value)}
          rows={3}
          placeholder="到点会以 directed message 投递给对象 agent"
          className="mb-3 w-full rounded-md border border-border-base bg-bg-base px-3 py-2 text-sm"
          data-testid="reminder-content"
        />

        {err && <p className="mb-2 text-xs text-danger" data-testid="reminder-error">{err}</p>}

        <div className="flex justify-end gap-2">
          <button type="button" onClick={onClose} className="rounded-md px-3 py-1.5 text-sm text-text-secondary hover:bg-bg-subtle">
            取消
          </button>
          <button
            type="button"
            disabled={!canSubmit || create.isPending}
            onClick={submit}
            className="rounded-md bg-brand px-4 py-1.5 text-sm font-semibold text-white disabled:opacity-50"
            data-testid="reminder-submit"
          >
            {create.isPending ? '创建中…' : '创建'}
          </button>
        </div>
      </div>
    </div>
  );
}
