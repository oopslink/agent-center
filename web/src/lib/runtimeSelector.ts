import type { ExecutorProfile } from '@/api/types';

export type RuntimeChoiceSource = 'catalog' | 'current';

export interface RuntimeSelectorCatalogCLI {
  key?: string;
  display_name?: string;
  enabled?: boolean;
}

export interface RuntimeSelectorCatalogModel {
  key?: string;
  model_key?: string;
  display_name?: string;
  compatible_cli_keys?: string[];
  enabled?: boolean;
}

// The shared selector only accepts the CLI/Model catalog slice it owns.
export interface RuntimeSelectorCatalog {
  revision?: number;
  clis?: RuntimeSelectorCatalogCLI[];
  models?: RuntimeSelectorCatalogModel[];
}

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

export interface RuntimeChoiceFilter {
  search?: string;
}

export interface RuntimePairState {
  cliSelectable: boolean;
  modelSelectable: boolean;
  selectable: boolean;
}

export interface RuntimeSelectorModel {
  revision?: number;
  defaultCLI: string;
  defaultModel: string;
  cliChoices: RuntimeCLIChoice[];
  modelChoices: RuntimeModelChoice[];
  hasCatalog: boolean;
  isEmpty: boolean;
  selectablePairCount: number;
}

export function buildRuntimeSelectorModel(
  catalog: RuntimeSelectorCatalog | undefined,
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

  const selectablePairCount = countSelectablePairs(cliChoices, modelChoices);
  const defaultCLI = defaultCLIForChoices(cliChoices, modelChoices);
  const defaultModel = defaultModelForCLI({ cliChoices, modelChoices }, defaultCLI);
  return {
    revision: catalog?.revision,
    defaultCLI,
    defaultModel,
    cliChoices,
    modelChoices,
    hasCatalog: !!catalog,
    isEmpty: selectablePairCount === 0,
    selectablePairCount,
  };
}

export function searchRuntimeCLIChoices(
  model: Pick<RuntimeSelectorModel, 'cliChoices'>,
  search?: string,
): RuntimeCLIChoice[] {
  const query = normalizeSearch(search);
  return model.cliChoices.filter((choice) => choiceMatches(query, [choice.key, choice.label]));
}

export function runtimeModelChoicesForCLI(
  model: Pick<RuntimeSelectorModel, 'cliChoices' | 'modelChoices'>,
  cli: string,
  currentModel?: string,
  filter: RuntimeChoiceFilter = {},
): RuntimeModelChoice[] {
  const normalizedCLI = cli.trim();
  const normalizedCurrent = currentModel?.trim() ?? '';
  const query = normalizeSearch(filter.search);
  const cliSelectable = isRuntimeCLISelectable(model, normalizedCLI);
  const out = cliSelectable
    ? model.modelChoices.filter((choice) =>
      choice.selectable &&
      choice.compatibleCLIKeys.includes(normalizedCLI) &&
      choiceMatches(query, [choice.value, choice.catalogKey ?? '', choice.label]),
    )
    : [];
  if (normalizedCurrent && !out.some((choice) => choice.value === normalizedCurrent)) {
    const current = model.modelChoices.find((choice) => choice.value === normalizedCurrent);
    if (current) {
      out.push({ ...current, selectable: false });
    }
  }
  return out;
}

export function defaultModelForCLI(
  model: Pick<RuntimeSelectorModel, 'cliChoices' | 'modelChoices'>,
  cli: string,
): string {
  return runtimeModelChoicesForCLI(model, cli)[0]?.value ?? '';
}

export function isRuntimeCLISelectable(
  model: Pick<RuntimeSelectorModel, 'cliChoices'>,
  cli: string,
): boolean {
  const normalized = cli.trim();
  return model.cliChoices.some((choice) => choice.key === normalized && choice.selectable);
}

export function isRuntimeModelSelectable(
  model: Pick<RuntimeSelectorModel, 'cliChoices' | 'modelChoices'>,
  cli: string,
  runtimeModel: string,
): boolean {
  const normalizedCLI = cli.trim();
  const normalizedModel = runtimeModel.trim();
  if (!isRuntimeCLISelectable(model, normalizedCLI)) return false;
  return model.modelChoices.some((choice) =>
    choice.value === normalizedModel &&
    choice.selectable &&
    choice.compatibleCLIKeys.includes(normalizedCLI),
  );
}

export function runtimePairState(
  model: Pick<RuntimeSelectorModel, 'cliChoices' | 'modelChoices'>,
  cli: string,
  runtimeModel: string,
): RuntimePairState {
  const cliSelectable = isRuntimeCLISelectable(model, cli);
  const modelSelectable = isRuntimeModelSelectable(model, cli, runtimeModel);
  return {
    cliSelectable,
    modelSelectable,
    selectable: cliSelectable && modelSelectable,
  };
}

export function invalidRuntimeExecutorProfiles(
  model: Pick<RuntimeSelectorModel, 'cliChoices' | 'modelChoices'>,
  executors: ExecutorProfile[],
): ExecutorProfile[] {
  return normalizeExecutorProfiles(executors).filter((exec) =>
    !runtimePairState(model, exec.cli, exec.model).selectable,
  );
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

function dedupeCLIs(clis: RuntimeSelectorCatalogCLI[]): RuntimeCLIChoice[] {
  const byKey = new Map<string, RuntimeCLIChoice>();
  for (const cli of clis) {
    const key = cli.key?.trim() ?? '';
    if (!key) continue;
    const choice: RuntimeCLIChoice = {
      key,
      label: cli.display_name ? `${cli.display_name} (${key})` : key,
      enabled: cli.enabled === true,
      selectable: cli.enabled === true,
      source: 'catalog',
    };
    const previous = byKey.get(key);
    if (!previous || (!previous.selectable && choice.selectable)) {
      byKey.set(key, choice);
    }
  }
  return [...byKey.values()];
}

function dedupeModels(models: RuntimeSelectorCatalogModel[]): RuntimeModelChoice[] {
  const byValue = new Map<string, RuntimeModelChoice>();
  for (const model of models) {
    const value = model.model_key?.trim() ?? '';
    if (!value) continue;
    const compatibleCLIKeys = uniqueStrings(model.compatible_cli_keys ?? []);
    const enabled = model.enabled === true;
    const choice: RuntimeModelChoice = {
      value,
      catalogKey: model.key?.trim() || undefined,
      label: model.display_name && model.display_name !== value
        ? `${model.display_name} (${value})`
        : value,
      compatibleCLIKeys,
      enabled,
      selectable: enabled && compatibleCLIKeys.length > 0,
      source: 'catalog',
    };
    const previous = byValue.get(value);
    if (!previous) {
      byValue.set(value, choice);
      continue;
    }
    const selectable = previous.selectable || choice.selectable;
    byValue.set(value, {
      ...previous,
      label: !previous.selectable && choice.selectable ? choice.label : previous.label,
      catalogKey: previous.catalogKey ?? choice.catalogKey,
      enabled: previous.enabled || choice.enabled,
      selectable,
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

function defaultCLIForChoices(
  cliChoices: RuntimeCLIChoice[],
  modelChoices: RuntimeModelChoice[],
): string {
  return cliChoices.find((choice) =>
    choice.selectable && hasSelectableModelForCLI(modelChoices, choice.key),
  )?.key ?? cliChoices.find((choice) => choice.selectable)?.key ?? '';
}

function hasSelectableModelForCLI(modelChoices: RuntimeModelChoice[], cli: string): boolean {
  return modelChoices.some((choice) =>
    choice.selectable && choice.compatibleCLIKeys.includes(cli),
  );
}

function countSelectablePairs(
  cliChoices: RuntimeCLIChoice[],
  modelChoices: RuntimeModelChoice[],
): number {
  const selectableCLIs = new Set(cliChoices.filter((choice) => choice.selectable).map((choice) => choice.key));
  let count = 0;
  for (const model of modelChoices) {
    if (!model.selectable) continue;
    for (const cli of model.compatibleCLIKeys) {
      if (selectableCLIs.has(cli)) count += 1;
    }
  }
  return count;
}

function normalizeSearch(search: string | undefined): string {
  return search?.trim().toLowerCase() ?? '';
}

function choiceMatches(query: string, values: string[]): boolean {
  if (!query) return true;
  return values.some((value) => value.toLowerCase().includes(query));
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
