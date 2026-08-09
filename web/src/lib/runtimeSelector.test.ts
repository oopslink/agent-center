import { describe, expect, it } from 'vitest';
import {
  buildRuntimeSelectorModel,
  defaultModelForCLI,
  invalidRuntimeExecutorProfiles,
  isRuntimeCLISelectable,
  isRuntimeModelSelectable,
  normalizeExecutorProfiles,
  runtimeModelChoicesForCLI,
  searchRuntimeCLIChoices,
  type RuntimeSelectorCatalog,
} from './runtimeSelector';

function catalog(overrides: Partial<RuntimeSelectorCatalog> = {}): RuntimeSelectorCatalog {
  return {
    revision: 7,
    clis: [
      { key: 'codex', display_name: 'Codex', enabled: true },
      { key: 'codex', display_name: 'Codex duplicate', enabled: false },
      { key: 'claude-code', display_name: 'Claude Code', enabled: true },
    ],
    models: [
      { key: 'gpt', model_key: 'gpt-5', display_name: 'GPT-5', compatible_cli_keys: ['codex'], enabled: true },
      { key: 'opus', model_key: 'claude-opus-4-8', display_name: 'Opus', compatible_cli_keys: ['claude-code'], enabled: true },
      { key: 'old', model_key: 'old-disabled', display_name: 'Old', compatible_cli_keys: ['codex'], enabled: false },
    ],
    ...overrides,
  };
}

describe('runtime selector model', () => {
  it('dedupes CLI keys and resolves deterministic defaults from CLI/Model catalog order', () => {
    const model = buildRuntimeSelectorModel(catalog());
    expect(model.cliChoices.map((choice) => choice.key)).toEqual(['codex', 'claude-code']);
    expect(model.defaultCLI).toBe('codex');
    expect(model.defaultModel).toBe('gpt-5');
    expect(defaultModelForCLI(model, 'codex')).toBe('gpt-5');
    expect(model.selectablePairCount).toBe(2);
  });

  it('does not consult Runtime Profile fields even if an older API response still includes them', () => {
    const cat = {
      ...catalog({
        models: [
          { key: 'fast', model_key: 'gpt-5-mini', display_name: 'GPT-5 Mini', compatible_cli_keys: ['codex'], enabled: true },
          { key: 'default', model_key: 'gpt-5-pro', display_name: 'GPT-5 Pro', compatible_cli_keys: ['codex'], enabled: true },
        ],
      }),
      default_runtime_profile_id: 'profile-default',
      profiles: [
        { id: 'profile-default', cli_key: 'codex', model_key: 'default', enabled: true },
      ],
    };
    const model = buildRuntimeSelectorModel(cat);
    expect(runtimeModelChoicesForCLI(model, 'codex').map((choice) => choice.value)).toEqual(['gpt-5-mini', 'gpt-5-pro']);
    expect(model.defaultModel).toBe('gpt-5-mini');
    expect(defaultModelForCLI(model, 'codex')).toBe('gpt-5-mini');
  });

  it('filters models by CLI compatibility and search text', () => {
    const model = buildRuntimeSelectorModel(catalog());
    expect(runtimeModelChoicesForCLI(model, 'codex').map((choice) => choice.value)).toEqual(['gpt-5']);
    expect(runtimeModelChoicesForCLI(model, 'claude-code').map((choice) => choice.value)).toEqual(['claude-opus-4-8']);
    expect(runtimeModelChoicesForCLI(model, 'claude-code', undefined, { search: 'opus' }).map((choice) => choice.value)).toEqual(['claude-opus-4-8']);
    expect(runtimeModelChoicesForCLI(model, 'claude-code', undefined, { search: 'gpt' })).toEqual([]);
    expect(searchRuntimeCLIChoices(model, 'claude').map((choice) => choice.key)).toEqual(['claude-code']);
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
    expect(invalidRuntimeExecutorProfiles(model, [{ cli: 'codex', model: 'executor-gone' }])).toEqual([
      { cli: 'codex', model: 'executor-gone' },
    ]);
    expect(isRuntimeCLISelectable(model, 'missing-cli')).toBe(false);
  });

  it('marks an empty catalog as empty and normalizes executor pairs', () => {
    const model = buildRuntimeSelectorModel(catalog({ clis: [], models: [] }));
    expect(model.isEmpty).toBe(true);
    expect(model.selectablePairCount).toBe(0);
    expect(normalizeExecutorProfiles([
      { cli: ' codex ', model: ' gpt-5 ' },
      { cli: 'codex', model: 'gpt-5' },
      { cli: '', model: 'ignored' },
    ])).toEqual([{ cli: 'codex', model: 'gpt-5' }]);
  });
});
