import type { AIRuntimeCatalog, RuntimeCLI, RuntimeModel } from '@/api/aiRuntime';
import type { WorkerCapability } from '@/api/types';

export interface RuntimeWorkerLike {
  capabilities?: WorkerCapability[];
}

export interface RuntimePair {
  cli: string;
  model: string;
}

export type RuntimeSelectionIssue =
  | 'catalog_unavailable'
  | 'worker_unavailable'
  | 'cli_required'
  | 'model_required'
  | 'cli_not_found'
  | 'cli_disabled'
  | 'worker_cli_unsupported'
  | 'model_not_found'
  | 'model_disabled'
  | 'model_incompatible';

export type RuntimePairValidation =
  | { ok: true; pair: RuntimePair; cli: RuntimeCLI; model: RuntimeModel }
  | { ok: false; issue: RuntimeSelectionIssue };

export function runtimeCLIOptions(
  catalog: AIRuntimeCatalog | undefined,
  worker: RuntimeWorkerLike | undefined,
): RuntimeCLI[] {
  if (!catalog || !worker) return [];
  return catalog.clis.filter((cli) => cli.enabled && workerSupportsRuntimeCLI(worker, cli.key));
}

export function runtimeModelOptions(
  catalog: AIRuntimeCatalog | undefined,
  worker: RuntimeWorkerLike | undefined,
  cliKey: string,
): RuntimeModel[] {
  if (!catalog || !worker || !workerSupportsRuntimeCLI(worker, cliKey)) return [];
  const cli = catalog.clis.find((entry) => entry.key === cliKey);
  if (!cli?.enabled) return [];
  return catalog.models.filter((model) =>
    model.enabled && model.compatible_cli_keys?.includes(cliKey),
  );
}

export function validateRuntimePair(
  catalog: AIRuntimeCatalog | undefined,
  worker: RuntimeWorkerLike | undefined,
  pair: RuntimePair,
): RuntimePairValidation {
  if (!catalog) return { ok: false, issue: 'catalog_unavailable' };
  if (!worker) return { ok: false, issue: 'worker_unavailable' };
  const cliKey = pair.cli.trim();
  const modelValue = pair.model.trim();
  if (!cliKey) return { ok: false, issue: 'cli_required' };
  if (!modelValue) return { ok: false, issue: 'model_required' };

  const cli = catalog.clis.find((entry) => entry.key === cliKey);
  if (!cli) return { ok: false, issue: 'cli_not_found' };
  if (!cli.enabled) return { ok: false, issue: 'cli_disabled' };
  if (!workerSupportsRuntimeCLI(worker, cli.key)) {
    return { ok: false, issue: 'worker_cli_unsupported' };
  }

  const model = findRuntimeModelByLegacy(catalog, modelValue);
  if (!model) return { ok: false, issue: 'model_not_found' };
  if (!model.enabled) return { ok: false, issue: 'model_disabled' };
  if (!model.compatible_cli_keys?.includes(cli.key)) {
    return { ok: false, issue: 'model_incompatible' };
  }

  return {
    ok: true,
    cli,
    model,
    pair: { cli: cli.key, model: model.model_key },
  };
}

export function defaultSupportedRuntimePair(
  catalog: AIRuntimeCatalog | undefined,
  worker: RuntimeWorkerLike | undefined,
): RuntimePair | null {
  if (!catalog || !worker) return null;
  const defaultProfile = catalog.profiles.find(
    (profile) => profile.id === catalog.default_runtime_profile_id && profile.enabled,
  );
  if (defaultProfile) {
    const resolved = validateRuntimePair(catalog, worker, {
      cli: defaultProfile.cli_key,
      model: defaultProfile.model_key,
    });
    if (resolved.ok) return resolved.pair;
  }
  for (const cli of runtimeCLIOptions(catalog, worker)) {
    const model = runtimeModelOptions(catalog, worker, cli.key)[0];
    if (model) return { cli: cli.key, model: model.model_key };
  }
  return null;
}

export function coerceRuntimePair(
  catalog: AIRuntimeCatalog | undefined,
  worker: RuntimeWorkerLike | undefined,
  pair: RuntimePair,
): RuntimePair | null {
  const current = validateRuntimePair(catalog, worker, pair);
  if (current.ok) return current.pair;
  if (!catalog || !worker) return null;
  const cli = catalog.clis.find((entry) => entry.key === pair.cli.trim());
  if (cli?.enabled && workerSupportsRuntimeCLI(worker, cli.key)) {
    const model = runtimeModelOptions(catalog, worker, cli.key)[0];
    if (model) return { cli: cli.key, model: model.model_key };
  }
  return defaultSupportedRuntimePair(catalog, worker);
}

export function normalizeRuntimePairs(
  catalog: AIRuntimeCatalog | undefined,
  worker: RuntimeWorkerLike | undefined,
  pairs: RuntimePair[],
): { ok: true; pairs: RuntimePair[] } | { ok: false; issue: RuntimeSelectionIssue; pair: RuntimePair } {
  const normalized: RuntimePair[] = [];
  const seen = new Set<string>();
  for (const pair of pairs) {
    const validation = validateRuntimePair(catalog, worker, pair);
    if (!validation.ok) return { ok: false, issue: validation.issue, pair };
    const key = `${validation.pair.cli}::${validation.pair.model}`;
    if (seen.has(key)) continue;
    seen.add(key);
    normalized.push(validation.pair);
  }
  return { ok: true, pairs: normalized };
}

export function findRuntimeModelByLegacy(
  catalog: AIRuntimeCatalog | undefined,
  value: string,
): RuntimeModel | undefined {
  const raw = value.trim();
  if (!catalog || !raw) return undefined;
  return catalog.models.find((model) => model.key === raw || model.model_key === raw);
}

export function workerSupportsRuntimeCLI(worker: RuntimeWorkerLike | undefined, cliKey: string): boolean {
  return !!worker?.capabilities?.some(
    (cap) => cap.agent_cli === cliKey && cap.detected && cap.enabled,
  );
}
