import { describe, expect, it } from 'vitest';
import type { AIRuntimeCatalog } from '@/api/aiRuntime';
import {
  buildRuntimeSelectorModel,
  defaultModelForCLI,
  isRuntimeCLISelectable,
  isRuntimeModelSelectable,
  normalizeExecutorProfiles,
  runtimeModelChoicesForCLI,
} from './runtimeSelector';

function catalog(overrides: Partial<AIRuntimeCatalog> = {}): AIRuntimeCatalog {
  return {
    org_id: 'org',
    revision: 7,
    default_runtime_profile_id: 'profile-default',
    clis: [
      { id: 'cli-codex', key: 'codex', display_name: 'Codex', executable: 'codex', enabled: true },
      { id: 'cli-codex-dup', key: 'codex', display_name: 'Codex duplicate', executable: 'codex', enabled: false },
      { id: 'cli-claude', key: 'claude-code', display_name: 'Claude Code', executable: 'claude', enabled: true },
    ],
    models: [
      { id: 'model-gpt', key: 'gpt', model_key: 'gpt-5', display_name: 'GPT-5', compatible_cli_keys: ['codex'], enabled: true },
      { id: 'model-opus', key: 'opus', model_key: 'claude-opus-4-8', display_name: 'Opus', compatible_cli_keys: ['claude-code'], enabled: true },
      { id: 'model-old', key: 'old', model_key: 'old-disabled', display_name: 'Old', compatible_cli_keys: ['codex'], enabled: false },
    ],
    profiles: [
      { id: 'profile-default', key: 'default-coding', name: 'Default', cli_key: 'codex', model_key: 'gpt', enabled: true, parameters: {} },
    ],
    ...overrides,
  };
}

describe('runtime selector model', () => {
  it('dedupes CLI keys and resolves the default profile to the legacy model string', () => {
    const model = buildRuntimeSelectorModel(catalog());
    expect(model.cliChoices.map((choice) => choice.key)).toEqual(['codex', 'claude-code']);
    expect(model.defaultCLI).toBe('codex');
    expect(model.defaultModel).toBe('gpt-5');
    expect(defaultModelForCLI(model, 'codex')).toBe('gpt-5');
  });

  it('uses the default profile model even when it is not the first compatible model', () => {
    const cat = catalog({
      models: [
        { id: 'model-fast', key: 'fast', model_key: 'gpt-5-mini', display_name: 'GPT-5 Mini', compatible_cli_keys: ['codex'], enabled: true },
        { id: 'model-default', key: 'default', model_key: 'gpt-5-pro', display_name: 'GPT-5 Pro', compatible_cli_keys: ['codex'], enabled: true },
      ],
      profiles: [
        { id: 'profile-default', key: 'default-coding', name: 'Default', cli_key: 'codex', model_key: 'default', enabled: true, parameters: {} },
      ],
    });
    const model = buildRuntimeSelectorModel(cat);
    expect(runtimeModelChoicesForCLI(model, 'codex').map((choice) => choice.value)).toEqual(['gpt-5-mini', 'gpt-5-pro']);
    expect(model.defaultModel).toBe('gpt-5-pro');
    expect(defaultModelForCLI(model, 'codex', cat)).toBe('gpt-5-pro');
  });

  it('filters models by CLI compatibility', () => {
    const model = buildRuntimeSelectorModel(catalog());
    expect(runtimeModelChoicesForCLI(model, 'codex').map((choice) => choice.value)).toEqual(['gpt-5']);
    expect(runtimeModelChoicesForCLI(model, 'claude-code').map((choice) => choice.value)).toEqual(['claude-opus-4-8']);
    expect(isRuntimeModelSelectable(model, 'codex', 'claude-opus-4-8')).toBe(false);
    expect(isRuntimeModelSelectable(model, 'codex', 'gpt-5')).toBe(true);
  });

  it('keeps current deleted values visible but not selectable after catalog refresh', () => {
    const model = buildRuntimeSelectorModel(catalog(), {
      cli: 'missing-cli',
      model: 'missing-model',
      executors: [{ cli: 'codex', model: 'executor-gone' }],
    });
    expect(model.cliChoices.find((choice) => choice.key === 'missing-cli')).toMatchObject({
      source: 'current',
      selectable: false,
    });
    expect(runtimeModelChoicesForCLI(model, 'missing-cli', 'missing-model')).toEqual([
      expect.objectContaining({ value: 'missing-model', selectable: false }),
    ]);
    expect(runtimeModelChoicesForCLI(model, 'codex', 'executor-gone')).toEqual([
      expect.objectContaining({ value: 'gpt-5', selectable: true }),
      expect.objectContaining({ value: 'executor-gone', selectable: false }),
    ]);
    expect(isRuntimeCLISelectable(model, 'missing-cli')).toBe(false);
  });

  it('marks an empty catalog as empty and normalizes executor pairs', () => {
    const model = buildRuntimeSelectorModel(catalog({ clis: [], models: [], profiles: [] }));
    expect(model.isEmpty).toBe(true);
    expect(normalizeExecutorProfiles([
      { cli: ' codex ', model: ' gpt-5 ' },
      { cli: 'codex', model: 'gpt-5' },
      { cli: '', model: 'ignored' },
    ])).toEqual([{ cli: 'codex', model: 'gpt-5' }]);
  });
});
