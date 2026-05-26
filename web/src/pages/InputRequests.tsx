import type React from 'react';
import { useMemo, useState } from 'react';
import {
  useCancelInputRequest,
  useInputRequests,
} from '@/api/inputRequests';
import type { InputRequest } from '@/api/types';
import { RespondInputRequestModal } from '@/components/RespondInputRequestModal';
import { EmptyState } from '@/components/EmptyState';
import { Skeleton } from '@/components/Skeleton';

type StatusFilter = 'pending' | 'responded' | 'cancelled' | 'all';

const TABS: Array<{ label: string; value: StatusFilter }> = [
  { label: 'Pending', value: 'pending' },
  { label: 'Responded', value: 'responded' },
  { label: 'Cancelled', value: 'cancelled' },
  { label: 'All', value: 'all' },
];

// InputRequests page (/inputrequests). Lists pending IRs by default with
// Respond + Cancel actions; tabs flip the status filter. SSE keeps the
// list fresh via F5's `input_request.*` → invalidate dispatch.
export default function InputRequests(): React.ReactElement {
  const [filter, setFilter] = useState<StatusFilter>('pending');
  const [respondTo, setRespondTo] = useState<InputRequest | null>(null);
  const all = useInputRequests();
  const cancel = useCancelInputRequest();

  const filtered = useMemo(() => {
    const list = all.data ?? [];
    if (filter === 'all') return list;
    return list.filter((ir) => ir.status === filter);
  }, [all.data, filter]);

  const handleCancel = (ir: InputRequest) => {
    if (!window.confirm(`Cancel input request "${ir.question}"?`)) return;
    cancel.mutate({ id: ir.id, message: 'user cancelled from UI' });
  };

  return (
    <section className="space-y-4" data-testid="page-InputRequests">
      <header>
        <h2 className="text-xl font-semibold">Input Requests</h2>
      </header>

      <div className="flex gap-1" role="tablist" aria-label="status filter">
        {TABS.map((t) => (
          <button
            key={t.value}
            type="button"
            role="tab"
            aria-selected={filter === t.value}
            onClick={() => setFilter(t.value)}
            className={[
              'rounded px-3 py-1 text-xs uppercase tracking-wide',
              filter === t.value
                ? 'bg-text-primary text-bg-elevated'
                : 'bg-bg-subtle text-text-secondary hover:bg-border-base',
            ].join(' ')}
            data-testid="ir-status-tab"
            data-status={t.value}
          >
            {t.label}
          </button>
        ))}
      </div>

      {all.isLoading && (
        <div className="space-y-2" data-testid="ir-loading">
          <Skeleton height="3rem" />
          <Skeleton height="3rem" />
        </div>
      )}
      {all.isError && (
        <p className="text-sm text-danger" data-testid="ir-error">
          {(all.error as Error).message}
        </p>
      )}
      {all.isSuccess && filtered.length === 0 && (
        <EmptyState
          testId="ir-empty"
          title={
            filter === 'pending'
              ? 'No pending input requests'
              : filter === 'all'
                ? 'No input requests yet'
                : `No ${filter} input requests`
          }
          body={
            filter === 'pending'
              ? 'When an agent needs a decision from you mid-task, it shows up here. The sidebar badge tracks pending count in realtime.'
              : 'Agents raise input requests when they need a human-in-the-loop answer.'
          }
        />
      )}

      {filtered.length > 0 && (
        <ul
          className="divide-y divide-border-base rounded border border-border-base bg-bg-elevated text-text-primary"
          data-testid="ir-list"
        >
          {filtered.map((ir) => (
            <li
              key={ir.id}
              className="flex items-start justify-between gap-4 px-4 py-3"
              data-testid="ir-row"
              data-ir-id={ir.id}
              data-ir-status={ir.status}
            >
              <div className="min-w-0 flex-1">
                <p className="font-medium text-text-primary">{ir.question}</p>
                <p className="mt-0.5 text-xs text-text-muted">
                  execution <span className="font-mono">{ir.execution_id}</span> ·{' '}
                  {ir.created_at}
                </p>
                {ir.answer && (
                  <p
                    className="mt-1 rounded bg-bg-subtle p-2 text-xs text-text-secondary"
                    data-testid="ir-answer-preview"
                  >
                    {ir.answer}
                  </p>
                )}
              </div>
              <div className="flex shrink-0 gap-2">
                {ir.status === 'pending' && (
                  <>
                    <button
                      type="button"
                      onClick={() => setRespondTo(ir)}
                      className="rounded bg-text-primary px-3 py-1.5 text-xs font-medium text-bg-elevated hover:opacity-90"
                      data-testid="ir-respond-button"
                    >
                      Respond
                    </button>
                    <button
                      type="button"
                      onClick={() => handleCancel(ir)}
                      disabled={cancel.isPending}
                      className="rounded px-3 py-1.5 text-xs text-text-primary hover:bg-bg-subtle disabled:opacity-50"
                      data-testid="ir-cancel-button"
                    >
                      Cancel
                    </button>
                  </>
                )}
              </div>
            </li>
          ))}
        </ul>
      )}

      <RespondInputRequestModal
        open={respondTo !== null}
        ir={respondTo}
        onClose={() => setRespondTo(null)}
      />
    </section>
  );
}
