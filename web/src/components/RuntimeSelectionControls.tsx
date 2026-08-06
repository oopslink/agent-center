import type React from 'react';
import {
  defaultRuntimeSelection,
  enabledRuntimeCLIs,
  enabledRuntimeModelsForCLI,
  enabledRuntimeProfiles,
  normalizeRuntimeSelection,
  resolveRuntimeSelection,
  type RuntimeCatalog,
  type RuntimeSelection,
  type RuntimeSelectionMode,
} from '@/api/aiRuntime';

const inputClass =
  'block w-full rounded border border-border-base bg-bg-elevated px-2.5 py-1.5 text-sm text-text-primary placeholder:text-text-muted focus:border-accent';

export function RuntimeSelectionControls({
  catalog,
  selection,
  onChange,
  idPrefix,
  label = 'AI Runtime',
  compact = false,
}: {
  catalog?: RuntimeCatalog;
  selection?: RuntimeSelection;
  onChange: (next: RuntimeSelection) => void;
  idPrefix: string;
  label?: string;
  compact?: boolean;
}): React.ReactElement {
  const normalized = normalizeRuntimeSelection(selection);
  const profiles = enabledRuntimeProfiles(catalog);
  const clis = enabledRuntimeCLIs(catalog);
  const selectedCLIKey =
    normalized.mode === 'override'
      ? clis.find((cli) => cli.id === normalized.cli_id || cli.key === normalized.cli_id)?.key || normalized.cli_id || clis[0]?.key || ''
      : '';
  const models = enabledRuntimeModelsForCLI(catalog, selectedCLIKey);
  const selectedModelKey =
    normalized.mode === 'override'
      ? models.find((model) => model.id === normalized.model_id || model.key === normalized.model_id || model.model_key === normalized.model_id)?.key || normalized.model_id || models[0]?.key || ''
      : '';
  const resolved = resolveRuntimeSelection(catalog, normalized);

  const changeMode = (mode: RuntimeSelectionMode) => {
    onChange(defaultSelectionForMode(mode, catalog));
  };
  const changeCLI = (cliKey: string) => {
    const nextModels = enabledRuntimeModelsForCLI(catalog, cliKey);
    onChange({ mode: 'override', cli_id: cliKey, model_id: nextModels[0]?.key ?? '' });
  };

  return (
    <div className={compact ? 'space-y-2' : 'space-y-2.5'} data-testid={`${idPrefix}-runtime-selection`}>
      <div className="grid grid-cols-1 gap-2 md:grid-cols-[9rem_1fr]">
        <div>
          <label className="mb-1 block text-[0.6875rem] font-semibold uppercase tracking-wide text-text-muted" htmlFor={`${idPrefix}-runtime-mode`}>
            {label}
          </label>
          <select
            id={`${idPrefix}-runtime-mode`}
            className={inputClass}
            value={normalized.mode || 'inherit'}
            onChange={(e) => changeMode(e.target.value as RuntimeSelectionMode)}
            data-testid={`${idPrefix}-runtime-mode`}
          >
            <option value="inherit">Inherit</option>
            <option value="profile" disabled={profiles.length === 0}>Profile</option>
            <option value="override" disabled={clis.length === 0}>Override</option>
          </select>
        </div>

        {normalized.mode === 'profile' ? (
          <div>
            <label className="mb-1 block text-[0.6875rem] font-semibold uppercase tracking-wide text-text-muted" htmlFor={`${idPrefix}-runtime-profile`}>
              Profile
            </label>
            <select
              id={`${idPrefix}-runtime-profile`}
              className={inputClass}
              value={normalized.profile_id ?? ''}
              onChange={(e) => onChange({ mode: 'profile', profile_id: e.target.value })}
              data-testid={`${idPrefix}-runtime-profile`}
            >
              {profiles.map((profile) => (
                <option key={profile.id} value={profile.id}>
                  {profile.name || profile.key}
                </option>
              ))}
            </select>
          </div>
        ) : normalized.mode === 'override' ? (
          <div className="grid grid-cols-1 gap-2 md:grid-cols-2">
            <div>
              <label className="mb-1 block text-[0.6875rem] font-semibold uppercase tracking-wide text-text-muted" htmlFor={`${idPrefix}-runtime-cli`}>
                CLI
              </label>
              <select
                id={`${idPrefix}-runtime-cli`}
                className={inputClass}
                value={selectedCLIKey}
                onChange={(e) => changeCLI(e.target.value)}
                data-testid={`${idPrefix}-runtime-cli`}
              >
                {clis.map((cli) => (
                  <option key={cli.id} value={cli.key}>
                    {cli.display_name || cli.key}
                  </option>
                ))}
              </select>
            </div>
            <div>
              <label className="mb-1 block text-[0.6875rem] font-semibold uppercase tracking-wide text-text-muted" htmlFor={`${idPrefix}-runtime-model`}>
                Model
              </label>
              <select
                id={`${idPrefix}-runtime-model`}
                className={inputClass}
                value={selectedModelKey}
                onChange={(e) => onChange({ mode: 'override', cli_id: selectedCLIKey, model_id: e.target.value })}
                data-testid={`${idPrefix}-runtime-model`}
              >
                {models.map((model) => (
                  <option key={model.id} value={model.key}>
                    {model.display_name || model.model_key}
                  </option>
                ))}
              </select>
            </div>
          </div>
        ) : (
          <div className="rounded border border-border-base bg-bg-subtle px-2.5 py-2 text-xs text-text-secondary" data-testid={`${idPrefix}-runtime-inherit`}>
            {resolved?.label ?? 'No organization default profile'}
          </div>
        )}
      </div>

      {normalized.mode !== 'inherit' && (
        <div className="rounded border border-border-base bg-bg-subtle px-2.5 py-2 text-xs text-text-secondary" data-testid={`${idPrefix}-runtime-resolved`}>
          {resolved?.label ?? 'Selection cannot be resolved by the current catalog'}
        </div>
      )}
      {!catalog && (
        <p className="text-[0.6875rem] text-status-amber-fg" data-testid={`${idPrefix}-runtime-loading`}>
          AI Runtime catalog unavailable
        </p>
      )}
    </div>
  );
}

function defaultSelectionForMode(mode: RuntimeSelectionMode, catalog?: RuntimeCatalog): RuntimeSelection {
  if (mode === 'inherit') return defaultRuntimeSelection();
  if (mode === 'profile') {
    const profile = enabledRuntimeProfiles(catalog)[0];
    return { mode: 'profile', profile_id: profile?.id ?? '' };
  }
  const cli = enabledRuntimeCLIs(catalog)[0];
  const model = enabledRuntimeModelsForCLI(catalog, cli?.key ?? '')[0];
  return { mode: 'override', cli_id: cli?.key ?? '', model_id: model?.key ?? '' };
}
