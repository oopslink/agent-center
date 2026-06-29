// Shared executor-profile UI helpers (v2.18.1, issue-8746a5b9). An executor
// profile is a {cli, model} pair the daemon may fork as a task executor. These
// helpers are consumed by both the editor (AgentConfigEditModal) and the
// read-only Runtime config view (AgentProfile) so the CLI color coding +
// "truly parallel" status wording stay in one place.
import type { ExecutorProfile } from '@/api/types';

// Per-CLI model suggestions (datalist). model is FREE TEXT server-side — these
// are hints only, the operator may type a custom value.
export const MODEL_SUGGESTIONS: Record<string, string[]> = {
  'claude-code': ['opus-4-8', 'sonnet-4-6', 'haiku-4-5', 'fable-5'],
  codex: ['gpt-5.5', 'gpt-5.4', 'gpt-5.4-mini', 'gpt-5.3-codex-spark'],
};

// Color-code a profile chip's badge by CLI so codex vs claude-code are visually
// distinct (per mockup2). Uses the shared status-chip palette (light/dark aware,
// a11y-token approved). Unknown CLIs fall back to a neutral tone.
export function executorBadgeClass(cli: string): string {
  switch (cli) {
    case 'claude-code':
      return 'bg-status-violet-bg text-status-violet-fg';
    case 'codex':
      return 'bg-status-cyan-bg text-status-cyan-fg';
    default:
      return 'bg-status-slate-bg text-status-slate-fg';
  }
}

// The opt-in gate is server-side max_concurrent>0 && executors non-empty, but
// the UI speaks in terms of "truly parallel" (mockup note 4): a cap of 1 is
// single-active even when technically enabled. trulyParallel ⇔ effective cap ≥ 2
// ⇔ max ≥ 2 && executors non-empty.
export function isTrulyParallel(maxConcurrent: number, executors: ExecutorProfile[]): boolean {
  return maxConcurrent >= 2 && executors.length > 0;
}
