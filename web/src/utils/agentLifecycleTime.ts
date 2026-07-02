// lifecycleTimeLabelKey maps an agent's lifecycle to the i18n key describing what
// its last_lifecycle_transition_at timestamp MEANS in that state:
//   running                    → "Started / Restarted" time
//   stopped / stopping         → "Stopped" time
//   error / failed / resetting / archived / unknown → generic "State changed" time
// A single stored timestamp (the last transition) is rendered under a label that
// matches the current state, per the product spec.
export function lifecycleTimeLabelKey(lifecycle: string | undefined): string {
  switch (lifecycle) {
    case 'running':
      return 'agents.lifecycleTime.started';
    case 'stopped':
    case 'stopping':
      return 'agents.lifecycleTime.stopped';
    default:
      return 'agents.lifecycleTime.changed';
  }
}
