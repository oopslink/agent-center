import type { AgentActivityEvent } from '@/api/types';
import { isCheckingEvent, executorProgressKey } from './AgentActivityRow';

// v2.8 #274 / v2.31.1: a rendered item in the Activity timeline — a single event,
// a folded run of consecutive "Checking" events, or a folded run of consecutive
// executor.progress heartbeats from the SAME executor.
export type ActivityItem =
  | { kind: 'event'; event: AgentActivityEvent }
  | { kind: 'checking-group'; events: AgentActivityEvent[] }
  | { kind: 'executor-progress-group'; events: AgentActivityEvent[] };

// groupActivity folds two kinds of CONSECUTIVE runs into single rows: CAT_CHECKING
// events → a "Checking messages × N" group, and executor.progress heartbeats that
// share an executor_id → an "Executor … × N" group (v2.31.1, oopslink DM
// 2026-07-04: the heartbeats otherwise flood the timeline). It runs over the FULL
// accumulated event list (all loaded cursor pages concatenated) — so a run that
// spans a "Load older" page boundary merges into one group rather than fragmenting
// into "× 30" + "× 20". A lone event of either kind renders as a normal row (no
// awkward "× 1"). The two run types are mutually exclusive (an executor.progress
// event is CAT_EXECUTOR, never CAT_CHECKING), and any other event flushes both.
export function groupActivity(events: AgentActivityEvent[]): ActivityItem[] {
  const items: ActivityItem[] = [];
  let checkingRun: AgentActivityEvent[] = [];
  let progressRun: AgentActivityEvent[] = [];
  let progressKey: string | null = null;

  const flushChecking = () => {
    if (checkingRun.length >= 2) items.push({ kind: 'checking-group', events: checkingRun });
    else if (checkingRun.length === 1) items.push({ kind: 'event', event: checkingRun[0] });
    checkingRun = [];
  };
  const flushProgress = () => {
    if (progressRun.length >= 2) items.push({ kind: 'executor-progress-group', events: progressRun });
    else if (progressRun.length === 1) items.push({ kind: 'event', event: progressRun[0] });
    progressRun = [];
    progressKey = null;
  };

  for (const ev of events) {
    const pKey = executorProgressKey(ev);
    if (pKey !== null) {
      flushChecking();
      // A different executor's heartbeat starts a fresh group.
      if (progressKey !== null && pKey !== progressKey) flushProgress();
      progressKey = pKey;
      progressRun.push(ev);
    } else if (isCheckingEvent(ev)) {
      flushProgress();
      checkingRun.push(ev);
    } else {
      flushChecking();
      flushProgress();
      items.push({ kind: 'event', event: ev });
    }
  }
  flushChecking();
  flushProgress();
  return items;
}
