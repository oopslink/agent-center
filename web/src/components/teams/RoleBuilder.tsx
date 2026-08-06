// Team WebUI — the declarative role-card builder shared by New Team,
// Instantiate, and role-definition edits. Count is team composition; existing
// team role definition edits can hide it while keeping per-agent defaults.
import type React from 'react';
import { useTranslation } from 'react-i18next';
import { useAIRuntimeCatalog, type RuntimeCatalog, type RuntimeModel, type RuntimeProfile, type RuntimeSelectionMode } from '@/api/aiRuntime';
import { roleColor, ROLE_DESC, type RoleInput } from '@/api/teams';
import { inputCls, SmallLabel } from './kit';
import { PlusIcon } from './teamsUi';

export function newRole(role = ''): RoleInput {
  return {
    role,
    cli: '',
    model: '',
    runtime_selection: { mode: 'inherit' },
    max_concurrency: 1,
    count: 1,
    tags: '',
    description: role ? ROLE_DESC[role] || '' : '',
  };
}

function Select({
  value,
  options,
  onChange,
  testId,
}: {
  value: string;
  options: readonly { value: string; label: string }[];
  onChange: (v: string) => void;
  testId?: string;
}): React.ReactElement {
  return (
    <select className={inputCls} value={value} data-testid={testId} onChange={(e) => onChange(e.target.value)}>
      {options.length === 0 && <option value="">Unavailable</option>}
      {options.map((o) => (
        <option key={o.value} value={o.value}>
          {o.label}
        </option>
      ))}
    </select>
  );
}

export function RoleBuilder({
  roles,
  onChange,
  showDescription,
  showCount = true,
  idPrefix,
}: {
  roles: RoleInput[];
  onChange: (next: RoleInput[]) => void;
  showDescription?: boolean;
  showCount?: boolean;
  idPrefix: string;
}): React.ReactElement {
  const { t } = useTranslation('teams');
  const catalog = useAIRuntimeCatalog();
  const runtime = catalog.data;
  const enabledProfiles = runtime?.profiles.filter((p) => p.enabled) ?? [];
  const enabledCLIs = runtime?.clis.filter((c) => c.enabled) ?? [];
  const patch = (i: number, p: Partial<RoleInput>) => {
    onChange(roles.map((r, j) => (j === i ? { ...r, ...p } : r)));
  };
  const remove = (i: number) => onChange(roles.filter((_, j) => j !== i));
  const add = () => onChange([...roles, newRole()]);
  const patchInherit = (i: number) => patch(i, { cli: '', model: '', runtime_selection: { mode: 'inherit' } });
  const patchProfile = (i: number, profileID: string) => {
    const profile = enabledProfiles.find((p) => p.id === profileID);
    patch(i, {
      cli: profile?.cli_key ?? '',
      model: modelRuntimeKey(runtime, profile?.model_key ?? ''),
      runtime_selection: { mode: 'profile', profile_id: profileID },
    });
  };
  const patchOverride = (i: number, cliKey: string, modelKey: string) => {
    const model = runtime?.models.find((m) => m.key === modelKey);
    patch(i, {
      cli: cliKey,
      model: model?.model_key ?? modelKey,
      runtime_selection: { mode: 'override', cli_id: cliKey, model_id: modelKey, parameters: {} },
    });
  };

  return (
    <div data-testid={`${idPrefix}-rolebuilder`}>
      {roles.map((r, i) => (
        <div
          key={i}
          data-testid={`${idPrefix}-role-${i}`}
          className="mb-3 rounded-lg border border-border-base bg-bg-subtle p-3.5"
        >
          <div className="mb-3 flex items-center gap-2.5">
            <span className="h-2.5 w-2.5 rounded-sm" style={{ background: roleColor(r.role) }} aria-hidden="true" />
            <input
              className="w-32 border-b border-dashed border-border-strong bg-transparent px-0.5 pb-0.5 text-sm font-semibold text-text-primary focus-visible:border-accent focus-visible:outline-none"
              value={r.role}
              placeholder={t('roleBuilder.roleNamePlaceholder')}
              data-testid={`${idPrefix}-role-${i}-name`}
              onChange={(e) => patch(i, { role: e.target.value })}
            />
            {showCount && (
              <>
                <span className="ml-1 inline-flex items-center overflow-hidden rounded border border-border-base">
                  <button
                    type="button"
                    className="h-6 w-6 bg-bg-elevated text-sm font-semibold text-text-secondary hover:bg-brand/10 hover:text-brand"
                    data-testid={`${idPrefix}-role-${i}-dec`}
                    aria-label={t('roleBuilder.decreaseCount')}
                    onClick={() => patch(i, { count: Math.max(1, r.count - 1) })}
                  >
                    -
                  </button>
                  <span className="h-6 w-8 border-x border-border-base text-center text-sm font-semibold leading-6" data-testid={`${idPrefix}-role-${i}-count`}>
                    {r.count}
                  </span>
                  <button
                    type="button"
                    className="h-6 w-6 bg-bg-elevated text-sm font-semibold text-text-secondary hover:bg-brand/10 hover:text-brand"
                    data-testid={`${idPrefix}-role-${i}-inc`}
                    aria-label={t('roleBuilder.increaseCount')}
                    onClick={() => patch(i, { count: r.count + 1 })}
                  >
                    +
                  </button>
                </span>
                <span className="text-[0.6875rem] text-text-muted">{t('roleBuilder.agentCount')}</span>
              </>
            )}
            <button
              type="button"
              className="ml-auto text-xs text-text-muted hover:text-danger"
              data-testid={`${idPrefix}-role-${i}-remove`}
              onClick={() => remove(i)}
            >
              {t('roleBuilder.remove')}
            </button>
          </div>

          {showDescription && (
            <div className="mb-3">
              <SmallLabel>{t('roleBuilder.descriptionLabel')}</SmallLabel>
              <input
                className={inputCls}
                value={r.description || ''}
                placeholder={t('roleBuilder.descriptionPlaceholder')}
                data-testid={`${idPrefix}-role-${i}-desc`}
                onChange={(e) => patch(i, { description: e.target.value })}
              />
            </div>
          )}

          <div className="grid grid-cols-1 gap-2.5 md:grid-cols-[1fr_1fr_10rem]">
            <RuntimeRoleControls
              catalog={runtime}
              loading={catalog.isLoading}
              role={r}
              idPrefix={idPrefix}
              index={i}
              onMode={(mode) => {
                if (mode === 'inherit') {
                  patchInherit(i);
                } else if (mode === 'profile') {
                  patchProfile(i, enabledProfiles[0]?.id ?? '');
                } else {
                  const cliKey = enabledCLIs[0]?.key ?? '';
                  const modelKey = firstCompatibleModel(runtime, cliKey)?.key ?? '';
                  patchOverride(i, cliKey, modelKey);
                }
              }}
              onProfile={(profileID) => patchProfile(i, profileID)}
              onOverride={(cliKey, modelKey) => patchOverride(i, cliKey, modelKey)}
            />
            <div>
              <SmallLabel>{t('roleBuilder.concurrencyLabel')}</SmallLabel>
              <input
                className={inputCls}
                type="number"
                min={1}
                value={r.max_concurrency}
                data-testid={`${idPrefix}-role-${i}-conc`}
                onChange={(e) => patch(i, { max_concurrency: Math.max(1, Number(e.target.value) || 1) })}
              />
              <p className="mt-1 text-[0.6875rem] leading-tight text-text-muted">{t('roleBuilder.concurrencyHint')}</p>
            </div>
          </div>

          <div className="mt-3">
            <SmallLabel>{t('roleBuilder.tagsLabel')}</SmallLabel>
            <input
              className={inputCls}
              value={r.tags}
              placeholder={t('roleBuilder.tagsPlaceholder')}
              data-testid={`${idPrefix}-role-${i}-tags`}
              onChange={(e) => patch(i, { tags: e.target.value })}
            />
          </div>
        </div>
      ))}
      <button
        type="button"
        className="flex w-full items-center justify-center gap-1.5 rounded-lg border border-dashed border-border-strong px-3 py-3 text-sm font-semibold text-text-muted hover:border-accent hover:bg-brand/5 hover:text-brand"
        data-testid={`${idPrefix}-add-role`}
        onClick={add}
      >
        <PlusIcon className="h-4 w-4" /> {t('roleBuilder.addRole')}
      </button>
    </div>
  );
}

export function totalSlots(roles: RoleInput[]): number {
  return roles.reduce((s, r) => s + r.count, 0);
}

function RuntimeRoleControls({
  catalog,
  loading,
  role,
  idPrefix,
  index,
  onMode,
  onProfile,
  onOverride,
}: {
  catalog?: RuntimeCatalog;
  loading: boolean;
  role: RoleInput;
  idPrefix: string;
  index: number;
  onMode: (mode: RuntimeSelectionMode) => void;
  onProfile: (profileID: string) => void;
  onOverride: (cliKey: string, modelKey: string) => void;
}): React.ReactElement {
  const mode = roleRuntimeMode(role);
  const profiles = catalog?.profiles.filter((p) => p.enabled) ?? [];
  const clis = catalog?.clis.filter((c) => c.enabled) ?? [];
  const cliKey = role.runtime_selection?.cli_id || role.cli || clis[0]?.key || '';
  const models = (catalog?.models ?? []).filter((m) => m.enabled && m.compatible_cli_keys.includes(cliKey));
  const modelKey = selectedModelKey(role, models);
  const profileID = role.runtime_selection?.profile_id || profiles[0]?.id || '';

  if (loading) {
    return (
      <div className="md:col-span-2">
        <SmallLabel>AI Runtime</SmallLabel>
        <div className="mt-1 h-9 rounded border border-border-base bg-bg-elevated" data-testid={`${idPrefix}-role-${index}-runtime-loading`} />
      </div>
    );
  }

  return (
    <>
      <div>
        <SmallLabel>AI Runtime</SmallLabel>
        <Select
          value={mode}
          testId={`${idPrefix}-role-${index}-runtime-mode`}
          options={[
            { value: 'inherit', label: 'Inherit default' },
            { value: 'profile', label: 'Profile' },
            { value: 'override', label: 'Override' },
          ]}
          onChange={(v) => onMode(v as RuntimeSelectionMode)}
        />
      </div>
      <div>
        {mode === 'inherit' && (
          <>
            <SmallLabel>Resolved profile</SmallLabel>
            <div className={`${inputCls} truncate text-text-muted`} data-testid={`${idPrefix}-role-${index}-runtime-inherit`}>
              {defaultProfileName(catalog)}
            </div>
          </>
        )}
        {mode === 'profile' && (
          <>
            <SmallLabel>Profile</SmallLabel>
            <Select
              value={profileID}
              options={profiles.map(profileOption)}
              testId={`${idPrefix}-role-${index}-runtime-profile`}
              onChange={onProfile}
            />
          </>
        )}
        {mode === 'override' && (
          <div className="grid grid-cols-2 gap-2">
            <div>
              <SmallLabel>CLI</SmallLabel>
              <Select
                value={cliKey}
                options={clis.map((c) => ({ value: c.key, label: c.display_name }))}
                testId={`${idPrefix}-role-${index}-cli`}
                onChange={(nextCLI) => onOverride(nextCLI, firstCompatibleModel(catalog, nextCLI)?.key ?? '')}
              />
            </div>
            <div>
              <SmallLabel>Model</SmallLabel>
              <Select
                value={modelKey}
                options={models.map(modelOption)}
                testId={`${idPrefix}-role-${index}-model`}
                onChange={(nextModel) => onOverride(cliKey, nextModel)}
              />
            </div>
          </div>
        )}
      </div>
    </>
  );
}

function roleRuntimeMode(role: RoleInput): RuntimeSelectionMode {
  if (role.runtime_selection?.mode) return role.runtime_selection.mode;
  return role.cli || role.model ? 'override' : 'inherit';
}

function profileOption(profile: RuntimeProfile): { value: string; label: string } {
  return { value: profile.id, label: profile.name };
}

function modelOption(model: RuntimeModel): { value: string; label: string } {
  return { value: model.key, label: model.display_name };
}

function defaultProfileName(catalog?: RuntimeCatalog): string {
  if (!catalog?.default_runtime_profile_id) return 'No default profile';
  return catalog.profiles.find((p) => p.id === catalog.default_runtime_profile_id)?.name ?? 'Missing default profile';
}

function firstCompatibleModel(catalog: RuntimeCatalog | undefined, cliKey: string): RuntimeModel | undefined {
  return catalog?.models.find((m) => m.enabled && m.compatible_cli_keys.includes(cliKey));
}

function selectedModelKey(role: RoleInput, models: RuntimeModel[]): string {
  if (role.runtime_selection?.model_id) return role.runtime_selection.model_id;
  const legacy = models.find((m) => m.key === role.model || m.model_key === role.model);
  return legacy?.key ?? models[0]?.key ?? '';
}

function modelRuntimeKey(catalog: RuntimeCatalog | undefined, profileModelKey: string): string {
  return catalog?.models.find((m) => m.key === profileModelKey)?.model_key ?? profileModelKey;
}
