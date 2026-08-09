import type {
  AIRuntimeCatalog,
  RuntimeCLI,
  RuntimeModel,
  RuntimeProfile,
} from '@/api/aiRuntime';
import type { ExecutorProfile } from '@/api/types';

export type RuntimeChoiceSource = 'catalog' | 'current';

export interface RuntimeCLIChoice {
  key: string;
  label: string;
  enabled: boolean;
  selectable: boolean;
  source: RuntimeChoiceSource;
}

export interface RuntimeModelChoice {
  value: string;
  catalogKey?: string;
  label: string;
  compatibleCLIKeys: string[];
  enabled: boolean;
  selectable: boolean;
  source: RuntimeChoiceSource;
}

export interface RuntimeSelectorCurrent {
  cli?: string;
  model?: string;
  executors?: ExecutorProfile[];
}

export interface RuntimeSelectorModel {
  revision?: number;
  defaultCLI: string;
  defaultModel: string;
  cliChoices: RuntimeCLIChoice[];
  modelChoices: RuntimeModelChoice[];
  hasCatalog: boolean;
  isEmpty: boolean;
}

export function buildRuntimeSelectorModel(
  catalog: AIRuntimeCatalog | undefined,
  current: RuntimeSelectorCurrent = {},
): RuntimeSelectorModel {
  const cliChoices = dedupeCLIs(catalog?.clis ?? []);
  const modelChoices = dedupeModels(catalog?.models ?? []);

  addCurrentCLI(cliChoices, current.cli);
  for (const exec of current.executors ?? []) {
    addCurrentCLI(cliChoices, exec.cli);
  }
  addCurrentModel(modelChoices, current.model);
  for (const exec of current.executors ?? []) {
    addCurrentModel(modelChoices, exec.model);
  }

  const defaultCLI = defaultCLIForCatalog(catalog, cliChoices);
  const defaultModel = defaultModelForCLI({ modelChoices }, defaultCLI, catalog);
  return {
    revision: catalog?.revision,
    defaultCLI,
    defaultModel,
    cliChoices,
    modelChoices,
    hasCatalog: !!catalog,
    isEmpty: !hasSelectableCLI(cliChoices) || modelChoices.every((m) => !m.selectable),
  };
}

export function runtimeModelChoicesForCLI(
  model: Pick<RuntimeSelectorModel, 'modelChoices'>,
  cli: string,
  currentModel?: string,
): RuntimeModelChoice[] {
  const normalizedCLI = cli.trim();
  const normalizedCurrent = currentModel?.trim() ?? '';
  const out = model.modelChoices.filter((choice) =>
    choice.selectable && choice.compatibleCLIKeys.includes(normalizedCLI),
  );
  if (normalizedCurrent && !out.some((choice) => choice.value === normalizedCurrent)) {
    const current = model.modelChoices.find((choice) => choice.value === normalizedCurrent);
    if (current) {
      out.push({ ...current, selectable: false });
    }
  }
  return out;
}

export function isRuntimeCLISelectable(
  model: Pick<RuntimeSelectorModel, 'cliChoices'>,
  cli: string,
): boolean {
  const normalized = cli.trim();
  return model.cliChoices.some((choice) => choice.key === normalized && choice.selectable);
}

export function isRuntimeModelSelectable(
  model: Pick<RuntimeSelectorModel, 'modelChoices'>,
  cli: string,
  runtimeModel: string,
): boolean {
  const normalizedCLI = cli.trim();
  const normalizedModel = runtimeModel.trim();
  return model.modelChoices.some((choice) =>
    choice.value === normalizedModel &&
    choice.selectable &&
    choice.compatibleCLIKeys.includes(normalizedCLI),
  );
}

export function defaultModelForCLI(
  model: Pick<RuntimeSelectorModel, 'modelChoices'>,
  cli: string,
  catalog?: AIRuntimeCatalog,
): string {
  const profile = defaultProfile(catalog);
  if (profile?.cli_key === cli) {
    const profileModel = catalog?.models.find((item) => item.key === profile.model_key);
    if (
      profileModel?.enabled &&
      (profileModel.compatible_cli_keys ?? []).includes(cli) &&
      profileModel.model_key.trim()
    ) {
      return profileModel.model_key.trim();
    }
  }
  return runtimeModelChoicesForCLI(model, cli)[0]?.value ?? '';
}

export function normalizeExecutorProfiles(executors: ExecutorProfile[]): ExecutorProfile[] {
  const seen = new Set<string>();
  const out: ExecutorProfile[] = [];
  for (const exec of executors) {
    const cli = exec.cli.trim();
    const model = exec.model.trim();
    if (!cli || !model) continue;
    const key = `${cli}\u0000${model}`;
    if (seen.has(key)) continue;
    seen.add(key);
    out.push({ cli, model });
  }
  return out;
}

function dedupeCLIs(clis: RuntimeCLI[]): RuntimeCLIChoice[] {
  const byKey = new Map<string, RuntimeCLIChoice>();
  for (const cli of clis) {
    const key = cli.key.trim();
    if (!key) continue;
    const choice: RuntimeCLIChoice = {
      key,
      label: cli.display_name ? `${cli.display_name} (${key})` : key,
      enabled: cli.enabled,
      selectable: cli.enabled,
      source: 'catalog',
    };
    const previous = byKey.get(key);
    if (!previous || (!previous.selectable && choice.selectable)) {
      byKey.set(key, choice);
    }
  }
  return [...byKey.values()];
}

function dedupeModels(models: RuntimeModel[]): RuntimeModelChoice[] {
  const byValue = new Map<string, RuntimeModelChoice>();
  for (const model of models) {
    const value = model.model_key.trim();
    if (!value) continue;
    const compatibleCLIKeys = uniqueStrings(model.compatible_cli_keys ?? []);
    const choice: RuntimeModelChoice = {
      value,
      catalogKey: model.key,
      label: model.display_name && model.display_name !== value
        ? `${model.display_name} (${value})`
        : value,
      compatibleCLIKeys,
      enabled: model.enabled,
      selectable: model.enabled && compatibleCLIKeys.length > 0,
      source: 'catalog',
    };
    const previous = byValue.get(value);
    if (!previous) {
      byValue.set(value, choice);
      continue;
    }
    byValue.set(value, {
      ...previous,
      enabled: previous.enabled || choice.enabled,
      selectable: previous.selectable || choice.selectable,
      compatibleCLIKeys: uniqueStrings([...previous.compatibleCLIKeys, ...choice.compatibleCLIKeys]),
    });
  }
  return [...byValue.values()];
}

function addCurrentCLI(choices: RuntimeCLIChoice[], raw: string | undefined): void {
  const key = raw?.trim() ?? '';
  if (!key || choices.some((choice) => choice.key === key)) return;
  choices.push({ key, label: key, enabled: false, selectable: false, source: 'current' });
}

function addCurrentModel(choices: RuntimeModelChoice[], raw: string | undefined): void {
  const value = raw?.trim() ?? '';
  if (!value || choices.some((choice) => choice.value === value)) return;
  choices.push({
    value,
    label: value,
    compatibleCLIKeys: [],
    enabled: false,
    selectable: false,
    source: 'current',
  });
}

function defaultCLIForCatalog(
  catalog: AIRuntimeCatalog | undefined,
  choices: RuntimeCLIChoice[],
): string {
  const profile = defaultProfile(catalog);
  if (profile && choices.some((choice) => choice.key === profile.cli_key && choice.selectable)) {
    return profile.cli_key;
  }
  return choices.find((choice) => choice.selectable)?.key ?? '';
}

function defaultProfile(catalog: AIRuntimeCatalog | undefined): RuntimeProfile | undefined {
  const id = catalog?.default_runtime_profile_id;
  if (!id) return undefined;
  return catalog?.profiles.find((profile) => profile.id === id && profile.enabled);
}

function hasSelectableCLI(choices: RuntimeCLIChoice[]): boolean {
  return choices.some((choice) => choice.selectable);
}

function uniqueStrings(items: string[]): string[] {
  const seen = new Set<string>();
  const out: string[] = [];
  for (const raw of items) {
    const value = raw.trim();
    if (!value || seen.has(value)) continue;
    seen.add(value);
    out.push(value);
  }
  return out;
}
