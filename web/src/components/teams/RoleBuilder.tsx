// Team WebUI — the declarative role-card builder shared by New Team,
// Instantiate, and role-definition edits. Count is team composition; existing
// team role definition edits can hide it while keeping per-agent defaults.
import type React from 'react';
import { useTranslation } from 'react-i18next';
import { useRuntimeCatalog, type RuntimeSelection, type RuntimeSelectionMode } from '@/api/aiRuntime';
import { CLIS, MODELS, roleColor, ROLE_DESC, type RoleInput } from '@/api/teams';
import { inputCls, SmallLabel } from './kit';
import { PlusIcon } from './teamsUi';

export function newRole(role = ''): RoleInput {
  return {
    role,
    cli: 'claude-code',
    model: 'sonnet-5',
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
  options: readonly string[];
  onChange: (v: string) => void;
  testId?: string;
}): React.ReactElement {
  return (
    <select className={inputCls} value={value} data-testid={testId} onChange={(e) => onChange(e.target.value)}>
      {options.map((o) => (
        <option key={o} value={o}>
          {o}
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
  const catalog = useRuntimeCatalog();
  const cliOptions = catalog.data?.clis.filter((c) => c.enabled).map((c) => c.key) ?? [...CLIS];
  const allModelOptions = catalog.data?.models.filter((m) => m.enabled) ?? [];
  const profileOptions = catalog.data?.profiles.filter((p) => p.enabled) ?? [];
  const patch = (i: number, p: Partial<RoleInput>) => {
    onChange(roles.map((r, j) => (j === i ? { ...r, ...p } : r)));
  };
  const patchSelection = (i: number, selection: RuntimeSelection) => {
    const role = roles[i];
    const next: Partial<RoleInput> = { runtime_selection: selection };
    if (selection.mode === 'override') {
      next.cli = selection.cli_id || role.cli;
      next.model = selection.model_id || role.model;
    }
    patch(i, next);
  };
  const remove = (i: number) => onChange(roles.filter((_, j) => j !== i));
  const add = () => onChange([...roles, newRole()]);

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

          <div className="grid grid-cols-1 gap-2.5 md:grid-cols-[11rem_1fr_1fr_10rem]">
            <div>
              <SmallLabel>{t('roleBuilder.runtimeMode')}</SmallLabel>
              <Select
                value={(r.runtime_selection?.mode ?? 'inherit') as RuntimeSelectionMode}
                options={['inherit', 'profile', 'override']}
                testId={`${idPrefix}-role-${i}-runtime-mode`}
                onChange={(mode) => {
                  if (mode === 'profile') {
                    patchSelection(i, { mode, profile_id: profileOptions[0]?.id ?? r.runtime_selection?.profile_id ?? '' });
                  } else if (mode === 'override') {
                    const cliID = cliOptions.includes(r.cli) ? r.cli : cliOptions[0] || r.cli || '';
                    const modelID = allModelOptions.find((m) => m.compatible_cli_keys.includes(cliID))?.key ?? r.model ?? MODELS[0];
                    patchSelection(i, { mode, cli_id: cliID, model_id: modelID });
                  } else {
                    patchSelection(i, { mode: 'inherit' });
                  }
                }}
              />
            </div>
            {(r.runtime_selection?.mode ?? 'inherit') === 'inherit' ? (
              <div className="md:col-span-2">
                <SmallLabel>{t('roleBuilder.inheritedRuntime')}</SmallLabel>
                <div
                  className="rounded border border-border-base bg-bg-subtle px-2 py-1.5 text-sm text-text-muted"
                  data-testid={`${idPrefix}-role-${i}-runtime-inherit`}
                >
                  {t('roleBuilder.inheritDefault')}
                </div>
              </div>
            ) : (r.runtime_selection?.mode ?? 'inherit') === 'profile' ? (
              <div className="md:col-span-2">
                <SmallLabel>{t('roleBuilder.profileLabel')}</SmallLabel>
                <Select
                  value={r.runtime_selection?.profile_id ?? profileOptions[0]?.id ?? ''}
                  options={profileOptions.length > 0 ? profileOptions.map((p) => p.id) : ['']}
                  testId={`${idPrefix}-role-${i}-runtime-profile`}
                  onChange={(v) => patchSelection(i, { mode: 'profile', profile_id: v })}
                />
              </div>
            ) : (
              <>
                <div>
                  <SmallLabel>{t('roleBuilder.cliLabel')}</SmallLabel>
                  <Select
                    value={r.runtime_selection?.mode === 'override' ? r.runtime_selection.cli_id ?? r.cli : r.cli}
                    options={cliOptions}
                    testId={`${idPrefix}-role-${i}-cli`}
                    onChange={(v) => patchSelection(i, { mode: 'override', cli_id: v, model_id: r.model })}
                  />
                </div>
                <div>
                  <SmallLabel>{t('roleBuilder.modelLabel')}</SmallLabel>
                  <Select
                    value={r.runtime_selection?.mode === 'override' ? r.runtime_selection.model_id ?? r.model : r.model}
                    options={
                      allModelOptions.length > 0
                        ? allModelOptions
                            .filter((m) => m.compatible_cli_keys.includes(r.runtime_selection?.cli_id ?? r.cli))
                            .map((m) => m.key)
                        : MODELS
                    }
                    testId={`${idPrefix}-role-${i}-model`}
                    onChange={(v) => patchSelection(i, { mode: 'override', cli_id: r.runtime_selection?.cli_id ?? r.cli, model_id: v })}
                  />
                </div>
              </>
            )}
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
