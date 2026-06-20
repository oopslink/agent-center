import type React from 'react';
import { useReminder } from '@/api/reminders';

// =============================================================================
// T207 [提醒-3] — row-click detail: the reminder + its 历史触发 (reminder_firings:
// 每次触发时间 / 是否投递 / 是否因重叠被跳过), per the mockup's list-row note.
// =============================================================================

const OUTCOME_LABEL: Record<string, string> = {
  delivered: '已投递',
  skipped_overlap: '重叠跳过',
  failed: '失败',
};

interface Props {
  slug: string | undefined;
  reminderId: string;
  onClose: () => void;
}

export function ReminderDetailModal({ slug, reminderId, onClose }: Props): React.ReactElement {
  const { data, isLoading } = useReminder(slug, reminderId);

  return (
    <div
      className="fixed inset-0 z-50 flex items-center justify-center bg-black/40 p-4"
      role="dialog"
      aria-modal="true"
      aria-label="提醒详情"
      data-testid="reminder-detail-modal"
    >
      <div className="flex max-h-[88vh] w-full max-w-md flex-col rounded-xl bg-bg-elevated shadow-xl">
        <div className="flex items-center justify-between border-b border-border-base px-5 py-3">
          <h4 className="text-base font-semibold text-text-primary">提醒详情</h4>
          <button type="button" onClick={onClose} className="text-text-muted hover:text-text-primary" aria-label="关闭">
            {/* ASCII close glyph (no-emoji-icons a11y guardrail); aria-label carries the name. */}
            <span aria-hidden="true">X</span>
          </button>
        </div>
        <div className="min-h-0 flex-1 space-y-3 overflow-y-auto px-5 py-4 text-sm">
          {isLoading && <p className="text-text-muted">加载中…</p>}
          {data && (
            <>
              <Row label="对象">{data.remindee_agent_id}</Row>
              <Row label="触发">
                {data.schedule.kind === 'cron' ? (
                  <span className="font-mono text-xs">{data.schedule.cron_expr} · {data.schedule.timezone}</span>
                ) : (
                  <span>{data.schedule.once_at}</span>
                )}
              </Row>
              <Row label="内容">{data.content}</Row>
              <Row label="状态">{data.status}</Row>
              <Row label="已触发">{data.fired_count} 次</Row>

              <div className="pt-1">
                <div className="mb-1 text-xs font-semibold text-text-secondary">历史触发</div>
                {data.firings.length === 0 ? (
                  <p className="text-xs text-text-muted" data-testid="reminder-firings-empty">
                    还没有触发记录。
                  </p>
                ) : (
                  <ul className="space-y-1" data-testid="reminder-firings">
                    {data.firings.map((f) => (
                      <li
                        key={f.id}
                        className="flex items-center justify-between rounded border border-border-base/60 px-2 py-1 text-xs"
                      >
                        <span className="text-text-secondary">{new Date(f.fired_at).toLocaleString()}</span>
                        <span
                          className={
                            f.outcome === 'delivered'
                              ? 'text-success'
                              : f.outcome === 'skipped_overlap'
                                ? 'text-warning'
                                : 'text-danger'
                          }
                        >
                          {OUTCOME_LABEL[f.outcome] ?? f.outcome}
                        </span>
                      </li>
                    ))}
                  </ul>
                )}
              </div>
            </>
          )}
        </div>
      </div>
    </div>
  );
}

function Row({ label, children }: { label: string; children: React.ReactNode }): React.ReactElement {
  return (
    <div className="flex gap-3">
      <span className="w-14 shrink-0 text-xs text-text-muted">{label}</span>
      <span className="min-w-0 flex-1 text-text-primary">{children}</span>
    </div>
  );
}
