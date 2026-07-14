import type React from 'react';
import type { TFunction } from 'i18next';
import { useTranslation } from 'react-i18next';
import {
  AGENT_BATCH_ACTIONS,
  type AgentBatchAction,
  type BatchLifecycleProgress,
} from '@/api/agents';

// T232 batch lifecycle UI — shared between the (Org Settings) Agents page and the
// merged Teams directory Agents page so the batch multi-select capability is not
// duplicated. Strings come from the `members` namespace (agents.batch.*).

// User-facing verb for each batch action (buttons, confirm copy, progress). The
// action key is the STABLE discriminator; the label is localized at render.
export function batchLabel(action: AgentBatchAction, t: TFunction): string {
  return t(`agents.batch.actions.${action}`);
}

// BatchToolbar — the selection action bar above the Agents table (T232). Shows
// the selected count, the four lifecycle actions, a live progress bar while a
// batch runs, and a succeeded/failed summary once it finishes. Buttons are
// disabled mid-run so a batch can't overlap itself.
export function BatchToolbar({
  selectedCount,
  progress,
  onAction,
  onClear,
}: {
  selectedCount: number;
  progress: BatchLifecycleProgress;
  onAction: (action: AgentBatchAction) => void;
  onClear: () => void;
}): React.ReactElement {
  const { t } = useTranslation('members');
  const { running, total, done, results, action } = progress;
  const failed = results.filter((r) => !r.ok);
  const succeeded = results.length - failed.length;
  const pct = total > 0 ? Math.round((done / total) * 100) : 0;
  return (
    <div
      className="flex flex-wrap items-center gap-3 rounded border border-border-base bg-bg-subtle px-3 py-2"
      data-testid="agents-batch-toolbar"
    >
      <span className="text-sm font-medium" data-testid="agents-batch-selected-count">
        {t('agents.batch.selectedCount', { count: selectedCount })}
      </span>
      <div className="flex flex-wrap items-center gap-2">
        {AGENT_BATCH_ACTIONS.map((act) => (
          <button
            key={act}
            type="button"
            data-testid={`agents-batch-${act}`}
            disabled={running || selectedCount === 0}
            onClick={() => onAction(act)}
            className={[
              'rounded px-2.5 py-1 text-xs font-medium motion-safe:transition-colors disabled:opacity-50',
              act === 'reset'
                ? 'text-danger hover:bg-danger/10'
                : 'text-text-secondary hover:bg-bg-elevated hover:text-text-primary',
            ].join(' ')}
          >
            {batchLabel(act, t)}
          </button>
        ))}
      </div>

      {running && (
        <div className="flex min-w-[10rem] flex-1 items-center gap-2" data-testid="agents-batch-progress">
          <div className="h-1.5 flex-1 overflow-hidden rounded-full bg-bg-elevated">
            <div
              className="h-full bg-brand motion-safe:transition-[width]"
              style={{ width: `${pct}%` }}
            />
          </div>
          <span className="whitespace-nowrap text-xs text-text-muted">
            {action ? batchLabel(action, t) : ''} {done}/{total}
          </span>
        </div>
      )}

      {!running && results.length > 0 && (
        <span className="text-xs text-text-muted" data-testid="agents-batch-summary">
          {failed.length > 0
            ? t('agents.batch.summaryWithFailed', { succeeded, failed: failed.length })
            : t('agents.batch.summary', { succeeded })}
        </span>
      )}

      <button
        type="button"
        data-testid="agents-batch-clear"
        onClick={onClear}
        disabled={running}
        className="ml-auto rounded px-2 py-1 text-xs text-text-muted hover:bg-bg-elevated hover:text-text-primary disabled:opacity-50"
      >
        {t('agents.batch.clear')}
      </button>
    </div>
  );
}
