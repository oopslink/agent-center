import type { RuntimeCatalog, RuntimeModel } from '@/api/aiRuntime';

export function enabledRuntimeCLIs(catalog: RuntimeCatalog | undefined): RuntimeCatalog['clis'] {
  return catalog?.clis.filter((cli) => cli.enabled) ?? [];
}

export function compatibleRuntimeModels(catalog: RuntimeCatalog | undefined, cliKey: string): RuntimeModel[] {
  if (!cliKey) return [];
  return catalog?.models.filter((model) => model.enabled && model.compatible_cli_keys.includes(cliKey)) ?? [];
}

export function firstEnabledRuntimeCLI(catalog: RuntimeCatalog | undefined): string {
  return enabledRuntimeCLIs(catalog)[0]?.key ?? '';
}

export function firstCompatibleRuntimeModelKey(catalog: RuntimeCatalog | undefined, cliKey: string): string {
  return compatibleRuntimeModels(catalog, cliKey)[0]?.model_key ?? '';
}

