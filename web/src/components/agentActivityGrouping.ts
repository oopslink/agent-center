import type { AgentActivityEvent } from '@/api/types';
import { isCheckingEvent } from './AgentActivityRow';

// v2.8 #274: a rendered item in the Activity timeline — either a single event or
// a folded run of consecutive "Checking" events.
export type ActivityItem =
  | { kind: 'event'; event: AgentActivityEvent }
  | { kind: 'checking-group'; events: AgentActivityEvent[] };

// groupActivity folds CONSECUTIVE CAT_CHECKING events into one group. It runs over
// the FULL accumulated event list (all loaded cursor pages concatenated) — so a
// Checking run that spans a "Load older" page boundary merges into one group
// rather than fragmenting into "× 30" + "× 20". A lone checking event renders as a
// normal row (no awkward "× 1").
export function groupActivity(events: AgentActivityEvent[]): ActivityItem[] {
  const items: ActivityItem[] = [];
  let run: AgentActivityEvent[] = [];
  const flush = () => {
    if (run.length >= 2) items.push({ kind: 'checking-group', events: run });
    else if (run.length === 1) items.push({ kind: 'event', event: run[0] });
    run = [];
  };
  for (const ev of events) {
    if (isCheckingEvent(ev)) {
      run.push(ev);
    } else {
      flush();
      items.push({ kind: 'event', event: ev });
    }
  }
  flush();
  return items;
}
