import type { AIRuntimeCatalog, RuntimeCLI, RuntimeModel } from '@/api/aiRuntime';

export interface RuntimeChoice {
  cli: string;
  model: string;
}

export function enabledRuntimeCLIs(catalog: AIRuntimeCatalog | undefined): RuntimeCLI[] {
  return (catalog?.clis ?? []).filter((cli) => cli.enabled);
}

export function enabledRuntimeModels(catalog: AIRuntimeCatalog | undefined): RuntimeModel[] {
  return (catalog?.models ?? []).filter((model) => model.enabled);
}

export function runtimeModelsForCLI(catalog: AIRuntimeCatalog | undefined, cliKey: string): RuntimeModel[] {
  return enabledRuntimeModels(catalog).filter((model) => runtimeModelAllowsCLI(model, cliKey));
}

export function runtimeDefaultChoice(catalog: AIRuntimeCatalog | undefined): RuntimeChoice | null {
	if (!catalog) return null;
	for (const cli of enabledRuntimeCLIs(catalog)) {
		const model = runtimeModelsForCLI(catalog, cli.key)[0];
		if (model) return { cli: cli.key, model: model.model_key };
  }
  return null;
}

export function normalizeRuntimeChoice(
  catalog: AIRuntimeCatalog | undefined,
  choice: Partial<RuntimeChoice>,
): RuntimeChoice | null {
  if (!catalog) {
    return choice.cli && choice.model ? { cli: choice.cli, model: choice.model } : null;
  }
  const cli = findRuntimeCLI(catalog, choice.cli ?? '');
  const model = findRuntimeModel(catalog, choice.model ?? '');
  if (cli?.enabled && model?.enabled && runtimeModelAllowsCLI(model, cli.key)) {
    return { cli: cli.key, model: model.model_key };
  }
  if (cli?.enabled) {
    const next = runtimeModelsForCLI(catalog, cli.key)[0];
    if (next) return { cli: cli.key, model: next.model_key };
  }
  return runtimeDefaultChoice(catalog);
}

export function findRuntimeCLI(catalog: AIRuntimeCatalog | undefined, value: string): RuntimeCLI | undefined {
  const key = value.trim();
  return (catalog?.clis ?? []).find((cli) => cli.id === key || cli.key === key || cli.executable === key);
}

export function findRuntimeModel(catalog: AIRuntimeCatalog | undefined, value: string): RuntimeModel | undefined {
  const key = value.trim();
  return (catalog?.models ?? []).find((model) => model.id === key || model.key === key || model.model_key === key);
}

export function runtimeModelAllowsCLI(model: RuntimeModel, cliKey: string): boolean {
  return (model.compatible_cli_keys ?? []).includes(cliKey);
}

export function runtimeCLIName(cli: RuntimeCLI | undefined): string {
  if (!cli) return '';
  return cli.display_name ? `${cli.display_name} (${cli.key})` : cli.key;
}

export function runtimeModelName(model: RuntimeModel | undefined): string {
  if (!model) return '';
  return model.display_name ? `${model.display_name} (${model.model_key})` : model.model_key;
}

export function runtimeModelDescription(model: RuntimeModel | undefined): string {
  if (!model) return '';
  const parts: string[] = [];
  if (model.tier) parts.push(model.tier);
  if (model.context_window) parts.push(`${formatCompactNumber(model.context_window)} context`);
  if (typeof model.input_cost_per_mtok === 'number' && typeof model.output_cost_per_mtok === 'number') {
    parts.push(`$${formatCost(model.input_cost_per_mtok)}/$${formatCost(model.output_cost_per_mtok)} per MTok`);
  }
  return parts.join(' · ');
}

function formatCompactNumber(value: number): string {
  if (value >= 1000) return `${Math.round(value / 1000)}k`;
  return String(value);
}

function formatCost(value: number): string {
  return Number.isInteger(value) ? String(value) : value.toFixed(2).replace(/0+$/, '').replace(/\.$/, '');
}
