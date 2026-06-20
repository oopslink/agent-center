import type React from 'react';
import { useMemo, useState } from 'react';
import { useCreateReminder, type ReminderEndCondition, type ReminderScheduleKind } from '@/api/reminders';
import { useAgents } from '@/api/agents';
import { Avatar } from './Avatar';

// =============================================================================
// T207 [提醒-3] — screens ② (新建·周期 cron) + ③ (新建·一次性 once), 1:1 to the
// mockup: 提醒对象 pills (同 project agents) · 触发方式 toggle · cron 表达式 +
// 常用预设 + 人话预览 · 一次性 日期+时间 + 预览 · 内容 · 高级(重叠跳过 + 结束条件).
// Submits to POST /api/orgs/{slug}/reminders. The remindee is an agent.
// =============================================================================

const CRON_PRESETS: ReadonlyArray<{ label: string; expr: string }> = [
  { label: '每小时', expr: '0 * * * *' },
  { label: '每天 09:00', expr: '0 9 * * *' },
  { label: '工作日 18:00', expr: '0 18 * * 1-5' },
  { label: '每周一 09:00', expr: '0 9 * * 1' },
  { label: '每 30 分钟', expr: '*/30 * * * *' },
];

const browserTz =
  typeof Intl !== 'undefined' ? Intl.DateTimeFormat().resolvedOptions().timeZone : 'UTC';

const WEEKDAYS = ['周日', '周一', '周二', '周三', '周四', '周五', '周六'];

// cronHuman renders a best-effort natural-language gloss for the common shapes
// the presets cover (the mockup's "人话预览"); unknown exprs fall back to raw.
function cronHuman(expr: string): string {
  const parts = expr.trim().split(/\s+/);
  if (parts.length !== 5) return expr;
  const [min, hr, dom, mon, dow] = parts;
  const hhmm = (h: string, m: string) => `${h.padStart(2, '0')}:${m.padStart(2, '0')}`;
  if (min === '*/30' && hr === '*') return '每 30 分钟';
  if (hr === '*' && min === '0') return '每小时整点';
  if (dom === '*' && mon === '*') {
    const time = /^\d+$/.test(hr) && /^\d+$/.test(min) ? hhmm(hr, min) : `${hr}:${min}`;
    if (dow === '*') return `每天 ${time}`;
    if (dow === '1-5') return `每周一至周五 ${time}`;
    if (/^\d$/.test(dow)) return `每${WEEKDAYS[Number(dow)]} ${time}`;
    return `每周(${dow}) ${time}`;
  }
  return expr;
}

interface Props {
  onClose: () => void;
}

export function ReminderCreateModal({ onClose }: Props): React.ReactElement {
  const create = useCreateReminder();
  const { data: agents } = useAgents();
  const [kind, setKind] = useState<ReminderScheduleKind>('cron');
  const [remindee, setRemindee] = useState('');
  const [content, setContent] = useState('');
  const [cronExpr, setCronExpr] = useState('0 18 * * 1-5');
  const [tz, setTz] = useState(browserTz);
  const [onceDate, setOnceDate] = useState('');
  const [onceTime, setOnceTime] = useState('09:00');
  const [skipOverlap, setSkipOverlap] = useState(true);
  const [deliverAsCreator, setDeliverAsCreator] = useState(true); // F-B: default ON per mockup
  const [endKind, setEndKind] = useState<ReminderEndCondition['kind']>('never');
  const [err, setErr] = useState<string | null>(null);

  const canSubmit =
    remindee.trim() !== '' &&
    content.trim() !== '' &&
    (kind === 'cron' ? cronExpr.trim() !== '' : onceDate !== '');

  const oncePreview = useMemo(() => {
    if (!onceDate) return '—';
    const dt = new Date(`${onceDate}T${onceTime}:00`);
    const hrs = Math.round((dt.getTime() - Date.now()) / 3.6e6);
    const rel = hrs > 0 ? `约 ${hrs} 小时后` : '已过期';
    return `${onceDate} ${onceTime} 触发一次 · ${rel} · 时区 ${tz}`;
  }, [onceDate, onceTime, tz]);

  function submit(): void {
    setErr(null);
    const schedule =
      kind === 'cron'
        ? { kind: 'cron' as const, cron_expr: cronExpr.trim(), timezone: tz }
        : { kind: 'once' as const, once_at: new Date(`${onceDate}T${onceTime}:00`).toISOString() };
    const end_condition: ReminderEndCondition = { kind: endKind };
    create.mutate(
      {
        remindee_agent_id: remindee.trim(),
        schedule,
        content: content.trim(),
        skip_if_overlap: skipOverlap,
        deliver_as_creator: deliverAsCreator,
        end_condition,
      },
      { onSuccess: () => onClose(), onError: (e) => setErr(e instanceof Error ? e.message : '创建失败') },
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
      <div className="flex max-h-[88vh] w-full max-w-lg flex-col rounded-xl bg-bg-elevated shadow-xl">
        <div className="flex items-center justify-between border-b border-border-base px-5 py-3">
          <h4 className="text-base font-semibold text-text-primary">新建提醒</h4>
          <button type="button" onClick={onClose} className="text-text-muted hover:text-text-primary" aria-label="关闭">
            {/* ASCII close glyph (no-emoji-icons a11y guardrail); aria-label carries the name. */}
            <span aria-hidden="true">X</span>
          </button>
        </div>

        <div className="min-h-0 flex-1 space-y-4 overflow-y-auto px-5 py-4">
          {/* 提醒对象 — agent pills */}
          <div>
            <label className="mb-1.5 block text-xs font-medium text-text-secondary">提醒对象</label>
            <div className="flex flex-wrap gap-1.5" data-testid="reminder-remindee-pills">
              {(agents ?? []).map((a) => {
                const on = remindee === a.id;
                return (
                  <button
                    key={a.id}
                    type="button"
                    onClick={() => setRemindee(a.id)}
                    aria-pressed={on}
                    data-testid="reminder-remindee-pill"
                    className={`inline-flex items-center gap-1.5 rounded-full border px-2.5 py-1 text-xs ${
                      on ? 'border-brand bg-brand/10 text-brand' : 'border-border-base text-text-secondary hover:bg-bg-subtle'
                    }`}
                  >
                    <Avatar name={a.name} kind="agent" size="sm" />
                    {a.name}
                  </button>
                );
              })}
            </div>
            <p className="mt-1.5 text-xs text-text-muted">可选同 project 的同伴 agent（护栏：仅同 project · 创建留审计）。</p>
          </div>

          {/* 触发方式 */}
          <div>
            <label className="mb-1.5 block text-xs font-medium text-text-secondary">触发方式</label>
            <div className="inline-flex rounded-md bg-bg-subtle p-0.5" role="tablist" aria-label="触发方式">
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
                  {k === 'once' ? '一次性' : '周期 (cron)'}
                </button>
              ))}
            </div>
          </div>

          {kind === 'cron' ? (
            <>
              <div>
                <label className="mb-1.5 block text-xs font-medium text-text-secondary">Cron 表达式</label>
                <input
                  value={cronExpr}
                  onChange={(e) => setCronExpr(e.target.value)}
                  placeholder="分 时 日 月 周"
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
                      {p.label}
                    </button>
                  ))}
                </div>
                <div
                  className="mt-2.5 flex items-center gap-2 rounded-lg border border-info/30 bg-info/10 px-3 py-2 text-xs text-info"
                  data-testid="reminder-preview"
                >
                  <span>{cronHuman(cronExpr)} 触发 · 时区 {tz}</span>
                </div>
                <input
                  value={tz}
                  onChange={(e) => setTz(e.target.value)}
                  aria-label="时区"
                  className="mt-2 w-full rounded-md border border-border-base bg-bg-base px-3 py-2 text-sm"
                />
              </div>
              {/* 高级 */}
              <div className="space-y-2 rounded-lg border border-border-base p-3">
                <label className="flex items-center justify-between gap-2 text-xs text-text-secondary">
                  <span>
                    上次未处理完则跳过本次
                    <span className="block text-text-muted">避免周期触发堆积（默认开）</span>
                  </span>
                  <input
                    type="checkbox"
                    checked={skipOverlap}
                    onChange={(e) => setSkipOverlap(e.target.checked)}
                    data-testid="reminder-skip-overlap"
                  />
                </label>
                <label className="flex items-center justify-between gap-2 text-xs text-text-secondary">
                  <span>
                    结束条件
                    <span className="block text-text-muted">永不结束 · 可设截止或最多触发 N 次</span>
                  </span>
                  <select
                    value={endKind}
                    onChange={(e) => setEndKind(e.target.value as ReminderEndCondition['kind'])}
                    className="rounded-md border border-border-base bg-bg-base px-2 py-1 text-xs"
                    data-testid="reminder-end-kind"
                  >
                    <option value="never">永不结束</option>
                    <option value="until">截止日期</option>
                    <option value="max_count">最多 N 次</option>
                  </select>
                </label>
              </div>
            </>
          ) : (
            <div>
              <label className="mb-1.5 block text-xs font-medium text-text-secondary">触发时间</label>
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
                ⏱ <span>{oncePreview}</span>
              </div>
            </div>
          )}

          {/* 内容 */}
          <div>
            <label className="mb-1.5 block text-xs font-medium text-text-secondary">提醒内容</label>
            <textarea
              value={content}
              onChange={(e) => setContent(e.target.value)}
              rows={2}
              placeholder="到点会作为一条 directed message 唤醒目标 agent"
              className="w-full rounded-md border border-border-base bg-bg-base px-3 py-2 text-sm"
              data-testid="reminder-content"
            />
          </div>

          {/* 以本人身份创建提醒文本 (F-B) — 蓝色 toggle, 两态共用, 默认开 */}
          <div className="flex items-center justify-between gap-3">
            <div className="text-xs text-text-secondary">
              以本人身份创建提醒文本
              <span className="block text-text-muted">到点的提醒消息以创建者身份发出（关闭则以系统身份）。</span>
            </div>
            <button
              type="button"
              role="switch"
              aria-checked={deliverAsCreator}
              aria-label="以本人身份创建提醒文本"
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

          {err && (
            <p className="text-xs text-danger" data-testid="reminder-error">
              {err}
            </p>
          )}
        </div>

        <div className="flex items-center justify-between border-t border-border-base px-5 py-3">
          <p className="text-xs text-text-muted">创建者：你 · 记入审计</p>
          <div className="flex gap-2">
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
              {create.isPending ? '创建中…' : '创建提醒'}
            </button>
          </div>
        </div>
      </div>
    </div>
  );
}
