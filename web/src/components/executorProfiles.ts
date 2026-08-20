// Shared executor-profile UI helpers (v2.18.1, issue-8746a5b9). An executor
// profile is a {cli, model} pair the daemon may fork as a task executor. These
// helpers are consumed by both the editor (AgentConfigEditModal) and the
// read-only Runtime config view (AgentProfile) so the CLI color coding +
// "truly parallel" status wording stay in one place.
import type { AIRuntimeCatalog } from '@/api/aiRuntime';
import type { ExecutorProfile, FleetWorkerRow } from '@/api/types';

export interface RuntimeOption {
  value: string;
  label: string;
}

export interface RuntimeChoices {
  cliOptions: RuntimeOption[];
  modelOptionsByCli: Record<string, RuntimeOption[]>;
  defaultCli: string;
  defaultModel: string;
}

export interface RuntimeValidationMessages {
  catalogLoading: string;
  catalogMissing: string;
  workerMissing: string;
  cliUnavailable: (cli: string) => string;
  modelUnavailable: (model: string, cli: string) => string;
  executorUnavailable: (cli: string, model: string) => string;
  workerCapabilityWarning?: (cli: string) => string;
}

export function deriveRuntimeChoices(
  catalog: AIRuntimeCatalog | undefined,
  _worker?: Pick<FleetWorkerRow, 'capabilities'>,
): RuntimeChoices {
  const enabledCatalogCLIs = (catalog?.clis ?? [])
    .filter((cli) => cli.enabled);
  const cliOptions = enabledCatalogCLIs.map((cli) => ({
    value: cli.key,
    label: cli.display_name ? `${cli.display_name} (${cli.key})` : cli.key,
  }));

  const modelOptionsByCli: Record<string, RuntimeOption[]> = {};
  for (const cli of enabledCatalogCLIs) {
    modelOptionsByCli[cli.key] = (catalog?.models ?? [])
      .filter((model) => model.enabled)
      .filter((model) => (model.compatible_cli_keys ?? []).includes(cli.key))
      .map((model) => ({
        value: model.model_key || model.key,
        label: model.display_name ? `${model.display_name} (${model.model_key || model.key})` : model.model_key || model.key,
      }));
  }

  // Defaults come directly from the first enabled catalog entries; worker
  // capability mismatches are warnings.
  const defaultCli = cliOptions[0]?.value ?? '';
  const defaultModel = modelOptionsByCli[defaultCli]?.[0]?.value ?? '';

  return { cliOptions, modelOptionsByCli, defaultCli, defaultModel };
}

export function validateRuntimePair(
  choices: RuntimeChoices,
  cli: string,
  model: string,
  messages: RuntimeValidationMessages,
  state: { catalogLoading: boolean; catalogReady: boolean; workerSelected: boolean },
): string | null {
  if (state.catalogLoading) return messages.catalogLoading;
  if (!state.catalogReady) return messages.catalogMissing;
  if (!choices.cliOptions.some((option) => option.value === cli)) {
    return messages.cliUnavailable(cli);
  }
  if (!choices.modelOptionsByCli[cli]?.some((option) => option.value === model.trim())) {
    return messages.modelUnavailable(model.trim(), cli);
  }
  return null;
}

export function runtimeWorkerWarning(
  worker: Pick<FleetWorkerRow, 'capabilities'> | undefined,
  cli: string,
  messages: RuntimeValidationMessages,
): string | null {
  if (!worker || !cli || !messages.workerCapabilityWarning) return null;
  const canRunCLI = (worker.capabilities ?? []).some(
    (cap) => cap.agent_cli === cli && cap.detected && cap.enabled,
  );
  return canRunCLI ? null : messages.workerCapabilityWarning(cli);
}

export function validateExecutorProfiles(
  choices: RuntimeChoices,
  executors: ExecutorProfile[],
  messages: RuntimeValidationMessages,
): string | null {
  for (const executor of executors) {
    const model = executor.model.trim();
    if (
      !choices.cliOptions.some((option) => option.value === executor.cli) ||
      !choices.modelOptionsByCli[executor.cli]?.some((option) => option.value === model)
    ) {
      return messages.executorUnavailable(executor.cli, model);
    }
  }
  return null;
}

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
