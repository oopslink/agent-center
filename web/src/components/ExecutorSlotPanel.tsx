import type React from 'react';
import { useMemo, useState } from 'react';
import { useTranslation } from 'react-i18next';
import type { TFunction } from 'i18next';
import type { AgentConcurrency, AgentConcurrencySlot, ConcurrencyExecutor } from '@/api/concurrency';
import type { Agent } from '@/api/types';

export type ConcurrencyMode = 'live' | 'offline' | 'expired' | 'disabled' | 'nodata';

export interface ExecutorSlotMatch {
  exec: NormalizedExecutorSlot;
  slotIndex?: number;
  stable: boolean;
}

export interface NormalizedExecutorSlot {
  slot_index?: number;
  state: string;
  executor_id?: string;
  task_id?: string;
  cli?: string;
  model?: string;
  started_at?: string;
  pid?: number;
  last_progress_at?: string;
  current_activity?: string;
}

export function concurrencyMode(data: AgentConcurrency): ConcurrencyMode {
  const reachable = data.reachable ?? true;
  const hasSnapshot = data.has_snapshot ?? !data.stale;
  if (!reachable) return 'offline';
  if (!hasSnapshot) return data.concurrency_enabled === false ? 'disabled' : 'nodata';
  if (data.stale) return 'expired';
  return 'live';
}

export function formatAge(ms: number): string {
  if (!Number.isFinite(ms) || ms < 0) return '0s';
  const s = Math.round(ms / 1000);
  if (s < 60) return `${s}s`;
  const m = Math.floor(s / 60);
  if (m < 60) return `${m}m ${s % 60}s`;
  return `${Math.floor(m / 60)}h ${m % 60}m`;
}

export function formatElapsed(startedAt: string | undefined): string {
  if (!startedAt) return '';
  const start = new Date(startedAt).getTime();
  if (Number.isNaN(start)) return '';
  const s = Math.floor((Date.now() - start) / 1000);
  if (s < 0) return '';
  if (s < 60) return `${s}s`;
  const m = Math.floor(s / 60);
  if (m < 60) return `${m}m ${s % 60}s`;
  const h = Math.floor(m / 60);
  return `${h}h ${m % 60}m`;
}

export function shortExecutorId(id: string | undefined): string {
  if (!id) return '';
  const tail = id.includes('-') ? id.slice(id.lastIndexOf('-') + 1) : id;
  return tail.length > 8 ? tail.slice(-8) : tail;
}

function asSlotFromExecutor(e: ConcurrencyExecutor): NormalizedExecutorSlot {
  return {
    slot_index: e.slot_index,
    state: e.state,
    executor_id: e.executor_id,
    task_id: e.task_id,
    cli: e.cli,
    model: e.model,
    started_at: e.started_at,
    pid: e.pid,
    last_progress_at: e.last_progress_at,
    current_activity: e.current_activity,
  };
}

function asSlot(s: AgentConcurrencySlot): NormalizedExecutorSlot {
  return {
    slot_index: s.slot_index,
    state: s.state,
    executor_id: s.executor_id,
    task_id: s.task_id,
    cli: s.cli,
    model: s.model,
    started_at: s.started_at,
    pid: s.pid,
    last_progress_at: s.last_progress_at,
    current_activity: s.current_activity,
  };
}

function slotStable(data: AgentConcurrency): boolean {
  if (data.slot_stable === false) return false;
  if (data.slot_stable === true) return true;
  return Boolean(data.slots?.some((slot) => Number.isInteger(slot.slot_index)));
}

function slotCount(data: AgentConcurrency): number {
  return Math.max(0, data.slot_count ?? data.cap ?? 0);
}

function configuredCap(data: AgentConcurrency): number {
  return Math.max(0, data.configured_cap ?? data.cap ?? 0);
}

function hasLastKnownSnapshot(data: AgentConcurrency): boolean {
  return data.has_snapshot ?? !data.stale;
}

function normalizedSlots(data: AgentConcurrency): NormalizedExecutorSlot[] {
  const stable = slotStable(data);
  if (!stable) return [];
  const mode = concurrencyMode(data);
  const source = data.slots?.length
    ? data.slots.map(asSlot)
    : (data.executors ?? [])
        .filter((exec) => Number.isInteger(exec.slot_index))
        .map(asSlotFromExecutor);
  const byIndex = new Map<number, NormalizedExecutorSlot>();
  for (const slot of source) {
    if (Number.isInteger(slot.slot_index) && (slot.slot_index as number) >= 0) {
      byIndex.set(slot.slot_index as number, slot);
    }
  }
  if (mode === 'live') {
    const count = slotCount(data);
    return Array.from({ length: count }, (_, i) => byIndex.get(i) ?? { slot_index: i, state: 'idle' });
  }
  return [...byIndex.values()].sort((a, b) => (a.slot_index ?? 0) - (b.slot_index ?? 0));
}

export function buildExecutorSlotByTask(data?: AgentConcurrency): Map<string, ExecutorSlotMatch> {
  const out = new Map<string, ExecutorSlotMatch>();
  if (!data) return out;
  const stable = slotStable(data);
  const slots = stable ? normalizedSlots(data) : (data.executors ?? []).map(asSlotFromExecutor);
  for (const slot of slots) {
    if (!slot.task_id || !slot.executor_id) continue;
    out.set(slot.task_id, {
      exec: slot,
      slotIndex: stable && Number.isInteger(slot.slot_index) ? slot.slot_index : undefined,
      stable,
    });
  }
  return out;
}

export function ExecutorSlotPanel({
  data,
  loading,
  error,
  title,
  className = '',
  compact = false,
  testId = 'executor-slot-panel',
}: {
  data?: AgentConcurrency;
  loading?: boolean;
  error?: Error | null;
  title?: string;
  className?: string;
  compact?: boolean;
  testId?: string;
}): React.ReactElement | null {
  const { t } = useTranslation('members');
  const slots = useMemo(() => (data ? normalizedSlots(data) : []), [data]);
  const [selectedSlotIndex, setSelectedSlotIndex] = useState<number | null>(null);

  if (loading && !data) {
    return (
      <section className={panelClass(false, className)} data-testid={testId} aria-busy="true">
        <p className="text-xs text-text-muted">{t('agentRuntime.executorSlots.loading')}</p>
      </section>
    );
  }
  if (!data) {
    if (!error) return null;
    return (
      <section className={panelClass(true, className)} data-testid={testId} data-mode="unavailable">
        <p className="text-xs text-status-amber-fg">{t('agentRuntime.executorSlots.unavailable')}</p>
      </section>
    );
  }

  const mode = concurrencyMode(data);
  const hasSnapshot = hasLastKnownSnapshot(data);
  const cap = slotCount(data);
  const configured = configuredCap(data);
  const active = Math.max(0, data.active);
  const fallback = Math.max(0, data.running ?? 0);
  const measured = hasSnapshot && (mode === 'live' || mode === 'expired' || mode === 'offline');
  const occupancy = measured ? active : fallback;
  const approximate = !measured && occupancy > 0;
  const amber = mode === 'offline' || mode === 'expired';
  const stable = slotStable(data);
  const draining = cap > configured || (data.admission_cap != null && data.admission_cap < cap);
  const chipSlots = stable && slots.length > 0
    ? slots
    : Array.from({ length: cap }, (_, i) => ({ slot_index: i, state: i < occupancy ? 'running' : 'idle' }));
  const selectedSlot = slots.find((slot) => slot.slot_index === selectedSlotIndex);
  const canRevealSlotDetails = !compact && stable && slots.length > 0;
  const detailId = `${testId}-slot-detail`;

  return (
    <section
      className={panelClass(amber, className)}
      data-testid={testId}
      data-stale={data.stale ? 'true' : 'false'}
      data-mode={mode}
      data-slot-stable={stable ? 'true' : 'false'}
      data-draining={draining ? 'true' : 'false'}
      aria-label={title ?? t('agentRuntime.executorSlots.heading')}
    >
      <div className="flex flex-wrap items-center justify-between gap-2">
        <div className="flex min-w-0 flex-wrap items-center gap-2">
          <h3 className="text-xs font-semibold uppercase tracking-wide text-text-muted">
            {title ?? t('agentRuntime.executorSlots.heading')}
          </h3>
          <span
            className="text-sm font-bold tabular-nums text-text-primary"
            data-testid="agent-concurrency-slots"
            aria-label={t('agentRuntime.executorSlots.summaryAria', {
              active: occupancy,
              slots: cap,
              approx: approximate ? t('agentRuntime.executorSlots.approx') : '',
            })}
          >
            {approximate ? '~' : ''}
            {measured || occupancy > 0 ? occupancy : '—'}
            <span className="text-text-muted">/{cap}</span>
          </span>
          <span className="text-xs text-text-muted">{t('agentRuntime.executorSlots.active')}</span>
          <span className="text-xs text-text-muted" data-testid="agent-concurrency-queued">
            {t('agentRuntime.executorSlots.queued', { count: Math.max(0, data.queued) })}
          </span>
          {draining && (
            <span className="rounded bg-status-amber-bg px-1.5 py-0.5 text-[0.625rem] font-semibold uppercase tracking-wide text-status-amber-fg" data-testid="agent-concurrency-draining">
              {t('agentRuntime.executorSlots.draining', { count: configured })}
            </span>
          )}
        </div>
        <div className="flex flex-wrap items-center gap-2 text-xs text-text-muted" data-testid="agent-concurrency-age">
          {mode === 'live' ? (
            <>
              <span className="inline-flex items-center gap-1" title={t('agentRuntime.executorSlots.adaptiveTitle')}><HeartIcon /> {t('agentRuntime.executorSlots.adaptive')}</span>
              <span>{t('agentRuntime.executorSlots.updatedAgo', { age: formatAge(data.snapshot_age_ms) })}</span>
            </>
          ) : mode === 'offline' ? (
            <span className="inline-flex items-center gap-1 font-medium text-status-amber-fg"><WarnIcon /> {t('agentRuntime.executorSlots.workerOffline')}</span>
          ) : mode === 'expired' ? (
            <span className="inline-flex items-center gap-1 font-medium text-status-amber-fg"><WarnIcon /> {t('agentRuntime.executorSlots.expiredAge', { age: formatAge(data.snapshot_age_ms) })}</span>
          ) : mode === 'disabled' ? (
            <span>{t('agentRuntime.executorSlots.disabledDetail')}</span>
          ) : (
            <span>{t('agentRuntime.executorSlots.nodataDetail')}</span>
          )}
        </div>
      </div>

      <div className="mt-2 flex flex-wrap items-center gap-1">
        {chipSlots.map((slot, i) => {
          const index = slot.slot_index ?? i;
          const state = normalizeState(slot.state);
          const selected = canRevealSlotDetails && selectedSlotIndex === index;
          const className = [
            'h-2.5 w-7 rounded-sm border transition focus:ring-2 focus:ring-accent focus:ring-offset-1 focus:ring-offset-bg-subtle',
            slotChipClass(state, amber || !measured),
            selected ? 'ring-2 ring-accent ring-offset-1 ring-offset-bg-subtle' : '',
            canRevealSlotDetails ? 'cursor-pointer hover:opacity-80' : 'cursor-default',
          ].filter(Boolean).join(' ');
          if (!canRevealSlotDetails) {
            return (
              <span
                key={index}
                className={className}
                data-testid="executor-slot-chip"
                data-slot-index={index}
                data-slot-state={state}
                title={stateLabel(slot.state, t)}
                aria-hidden="true"
              />
            );
          }
          return (
            <button
              key={index}
              type="button"
              className={className}
              data-testid="executor-slot-chip"
              data-slot-index={index}
              data-slot-state={state}
              aria-label={t('agentRuntime.executorSlots.slotAria', { index, state: stateLabel(slot.state, t) })}
              aria-pressed={selected ? 'true' : 'false'}
              aria-expanded={selected ? 'true' : 'false'}
              aria-controls={detailId}
              title={stateLabel(slot.state, t)}
              onClick={() => setSelectedSlotIndex((current) => (current === index ? null : index))}
            />
          );
        })}
      </div>

      {!compact && (
        stable ? (
          selectedSlot ? (
            <div className="mt-3" id={detailId}>
              <ul className="grid gap-2" data-testid="executor-slot-list">
                <ExecutorSlotRow
                  slot={selectedSlot}
                  targetCap={configured}
                  stale={mode !== 'live'}
                />
              </ul>
            </div>
          ) : slots.length === 0 ? (
            <div className="mt-3" id={detailId}>
              <p className="text-xs text-text-muted" data-testid="executor-slot-empty">
                {t('agentRuntime.executorSlots.empty')}
              </p>
            </div>
          ) : null
        ) : (
          <div className="mt-3" id={detailId}>
            <LegacyExecutorList data={data} />
          </div>
        )
      )}
    </section>
  );
}

function LegacyExecutorList({ data }: { data: AgentConcurrency }): React.ReactElement {
  const { t } = useTranslation('members');
  const executors = data.executors ?? [];
  return (
    <div data-testid="executor-slot-legacy" className="space-y-2">
      <p className="text-xs text-text-muted">{t('agentRuntime.executorSlots.slotNumbersUnavailable')}</p>
      {executors.length > 0 && (
        <ul className="space-y-1">
          {executors.map((exec) => (
            <li key={exec.executor_id} className="flex min-w-0 flex-wrap items-center gap-2 text-xs text-text-secondary">
              <span className="font-mono text-text-primary">{t('agentRuntime.executorSlots.execShort', { id: shortExecutorId(exec.executor_id) })}</span>
              <span className={stateClass(exec.state)}>{stateLabel(exec.state, t)}</span>
              {exec.task_id && <span className="truncate text-text-muted">{exec.task_id}</span>}
            </li>
          ))}
        </ul>
      )}
    </div>
  );
}

function ExecutorSlotRow({
  slot,
  targetCap,
  stale,
}: {
  slot: NormalizedExecutorSlot;
  targetCap: number;
  stale: boolean;
}): React.ReactElement {
  const { t } = useTranslation('members');
  const index = slot.slot_index ?? 0;
  const highSlotDraining = index >= targetCap;
  const state = normalizeState(slot.state);
  const elapsed = formatElapsed(slot.started_at);
  const progressAge = slot.last_progress_at
    ? formatAge(Math.max(0, Date.now() - new Date(slot.last_progress_at).getTime()))
    : '';

  return (
    <li
      className={`min-w-0 rounded border px-2 py-2 ${
        state === 'orphan' ? 'border-warning/50 bg-status-amber-bg' : 'border-border-base bg-bg-elevated/40'
      }`}
      data-testid="executor-slot-row"
      data-slot-index={index}
      data-slot-state={state}
      aria-label={t('agentRuntime.executorSlots.slotAria', { index, state: stateLabel(slot.state, t) })}
    >
      <div className="flex items-center justify-between gap-2">
        <span className="font-mono text-xs font-semibold text-text-primary" data-testid="executor-slot-index">
          {t('agentRuntime.executorSlots.slotLabel', { index })}
        </span>
        <span className={stateClass(slot.state)} data-testid="executor-slot-state">
          {stateLabel(slot.state, t)}
        </span>
      </div>
      <div className="mt-1 flex min-w-0 flex-wrap items-center gap-x-2 gap-y-1 text-[0.6875rem] text-text-muted">
        {slot.task_id && <span className="truncate text-text-secondary" data-testid="executor-slot-task">{slot.task_id}</span>}
        {slot.cli && <span data-testid="executor-slot-cli-model">{[slot.cli, slot.model].filter(Boolean).join(' · ')}</span>}
        {elapsed && <span data-testid="executor-slot-elapsed">{elapsed}</span>}
        {slot.pid ? <span>{t('agentRuntime.executorSlots.pid', { pid: slot.pid })}</span> : null}
        {progressAge && !stale ? <span>{t('agentRuntime.executorSlots.progressAge', { age: progressAge })}</span> : null}
        {highSlotDraining && (
          <span className="rounded bg-status-amber-bg px-1 py-0.5 font-semibold uppercase tracking-wide text-status-amber-fg" data-testid="executor-slot-row-draining">
            {t('agentRuntime.executorSlots.rowDraining')}
          </span>
        )}
        {slot.current_activity && (
          <span className="basis-full truncate text-text-secondary" data-testid="executor-slot-current-activity" title={slot.current_activity}>
            {t('agentRuntime.executorSlots.currentActivity', { activity: slot.current_activity })}
          </span>
        )}
      </div>
    </li>
  );
}

export function ExecutorTaskOverlay({
  match,
  stale,
  snapshotAgeMs,
}: {
  match: ExecutorSlotMatch;
  stale?: boolean;
  snapshotAgeMs?: number;
}): React.ReactElement {
  const { t } = useTranslation('members');
  const exec = match.exec;
  const state = normalizeState(exec.state);
  const elapsed = formatElapsed(exec.started_at);
  return (
    <div className="mt-1 flex flex-wrap items-center gap-x-2 gap-y-1 text-[0.6875rem]" data-testid="agent-task-overlay">
      {(exec.cli || exec.model) && (
        <span
          className="rounded bg-bg-subtle px-1.5 py-0.5 font-mono text-text-secondary"
          data-testid="agent-task-cli-model"
        >
          {[exec.cli, exec.model].filter(Boolean).join(' · ')}
        </span>
      )}
      {match.stable && match.slotIndex != null ? (
        <span className="rounded bg-status-blue-bg px-1.5 py-0.5 font-semibold uppercase tracking-wide text-status-blue-fg" data-testid="agent-task-slot">
          {t('agentRuntime.executorSlots.executorNumber', { index: match.slotIndex })}
        </span>
      ) : exec.executor_id ? (
        <span className="rounded bg-bg-subtle px-1.5 py-0.5 font-mono text-text-muted" data-testid="agent-task-executor">
          {t('agentRuntime.executorSlots.execShort', { id: shortExecutorId(exec.executor_id) })}
        </span>
      ) : null}
      {elapsed && (
        <span className="text-text-muted" data-testid="agent-task-elapsed">
          {elapsed}{state === 'starting' ? ` ${t('agentRuntime.executorSlots.state.starting')}` : ''}
        </span>
      )}
      {!stale && typeof snapshotAgeMs === 'number' && (
        <span className="inline-flex items-center gap-1 text-text-muted" data-testid="agent-task-heartbeat" title={t('agentRuntime.executorSlots.heartbeatTitle')}>
          <HeartIcon /> {formatAge(snapshotAgeMs)}
        </span>
      )}
      {state === 'orphan' && (
        <span className="rounded bg-status-amber-bg px-1.5 py-0.5 font-semibold uppercase tracking-wide text-status-amber-fg" data-testid="agent-task-orphan">
          {t('agentRuntime.executorSlots.orphan')}
        </span>
      )}
      {stale && (
        <span className="font-medium text-status-amber-fg" data-testid="agent-task-overlay-stale">
          {t('agentRuntime.executorSlots.overlayStale')}
        </span>
      )}
      {exec.current_activity && (
        <span
          className="min-w-0 basis-full truncate text-text-secondary"
          data-testid="agent-task-current-activity"
          title={exec.current_activity}
        >
          {t('agentRuntime.executorSlots.currentActivity', { activity: exec.current_activity })}
        </span>
      )}
    </div>
  );
}

export function AgentSlotMetricBadge({
  agent,
}: {
  agent: Pick<
    Agent,
    | 'active'
    | 'slot_count'
    | 'slot_stable'
    | 'stale'
    | 'reachable'
    | 'has_snapshot'
    | 'running_tasks'
    | 'effective_concurrency_cap'
  >;
}): React.ReactElement {
  const { t } = useTranslation('members');
  const hasLiveMetric = typeof agent.active === 'number' && typeof agent.slot_count === 'number';
  const slots = Math.max(0, agent.slot_count ?? agent.effective_concurrency_cap ?? 1);
  const active = Math.max(0, hasLiveMetric ? (agent.active as number) : agent.running_tasks ?? 0);
  const approximate = !hasLiveMetric;
  const offline = agent.reachable === false;
  const stale = agent.stale === true || offline || agent.has_snapshot === false;
  const label = `${approximate ? '~' : ''}${active}/${slots}`;
  return (
    <span
      className={[
        'inline-flex items-center whitespace-nowrap rounded bg-bg-subtle px-1.5 py-0.5 text-[0.6875rem] font-medium tabular-nums',
        stale ? 'text-warning' : 'text-text-secondary',
      ].join(' ')}
      data-testid="agent-slot-metric"
      data-approximate={approximate ? 'true' : 'false'}
      data-slot-stable={agent.slot_stable === true ? 'true' : 'false'}
      data-stale={stale ? 'true' : 'false'}
      title={
        approximate
          ? t('agentRuntime.executorSlots.metricFallbackTitle')
          : t('agentRuntime.executorSlots.metricTitle', { active, slots })
      }
    >
      {t('agentRuntime.executorSlots.metricLabel', { value: label })}
    </span>
  );
}

function panelClass(amber: boolean, extra: string): string {
  return [
    'rounded border px-3 py-2',
    amber ? 'border-warning/40 bg-status-amber-bg' : 'border-border-base bg-bg-subtle',
    extra,
  ].filter(Boolean).join(' ');
}

function normalizeState(state: string | undefined): 'idle' | 'running' | 'starting' | 'finishing' | 'orphan' | 'unknown' {
  const s = (state ?? '').toLowerCase();
  if (!s || s === 'idle') return 'idle';
  if (s.includes('orphan')) return 'orphan';
  if (s.includes('start') || s.includes('reserv')) return 'starting';
  if (s.includes('finish') || s.includes('stop') || s.includes('final')) return 'finishing';
  if (s.includes('run') || s.includes('active')) return 'running';
  return 'unknown';
}

function stateLabel(state: string | undefined, t: TFunction): string {
  return t(`agentRuntime.executorSlots.state.${normalizeState(state)}`);
}

function stateClass(state: string | undefined): string {
  const cls = normalizeState(state);
  const base = 'rounded px-1.5 py-0.5 text-[0.625rem] font-semibold uppercase tracking-wide';
  if (cls === 'running') return `${base} bg-status-blue-bg text-status-blue-fg`;
  if (cls === 'starting') return `${base} bg-bg-subtle text-text-secondary`;
  if (cls === 'finishing') return `${base} bg-status-teal-bg text-status-teal-fg`;
  if (cls === 'orphan') return `${base} bg-status-amber-bg text-status-amber-fg`;
  if (cls === 'idle') return `${base} bg-bg-subtle text-text-muted`;
  return `${base} bg-bg-subtle text-text-secondary`;
}

function slotChipClass(state: ReturnType<typeof normalizeState>, muted: boolean): string {
  if (muted && state === 'running') return 'border-warning/40 bg-warning';
  if (state === 'running') return 'border-brand bg-brand';
  if (state === 'starting') return 'border-status-blue-fg/30 bg-status-blue-bg';
  if (state === 'finishing') return 'border-status-teal-fg/30 bg-status-teal-bg';
  if (state === 'orphan') return 'border-warning/50 bg-warning';
  if (state === 'idle') return 'border-border-strong bg-border-strong';
  return 'border-border-strong bg-bg-subtle';
}

function HeartIcon(): React.ReactElement {
  return (
    <svg viewBox="0 0 16 16" className="h-3 w-3 shrink-0" fill="currentColor" aria-hidden="true">
      <path d="M8 14s-5-3.3-5-7a3 3 0 0 1 5-2.2A3 3 0 0 1 13 7c0 3.7-5 7-5 7z" />
    </svg>
  );
}

function WarnIcon(): React.ReactElement {
  return (
    <svg viewBox="0 0 16 16" className="h-3.5 w-3.5 shrink-0" fill="none" stroke="currentColor" strokeWidth="1.5" aria-hidden="true">
      <path d="M8 2.5 14.5 13.5H1.5z" strokeLinejoin="round" />
      <path d="M8 6.5v3.2M8 11.6v.01" strokeLinecap="round" />
    </svg>
  );
}
