// Team WebUI — the declarative role-card builder shared by New Team,
// Instantiate, and role-definition edits. Count is team composition; existing
// team role definition edits can hide it while keeping per-agent defaults.
import React, { useEffect } from 'react';
import { useTranslation } from 'react-i18next';
import { useAIRuntimeCatalog } from '@/api/aiRuntime';
import { roleColor, ROLE_DESC, type RoleInput } from '@/api/teams';
import {
  enabledRuntimeCLIs,
  normalizeRuntimeChoice,
  runtimeCLIName,
  runtimeDefaultChoice,
  runtimeModelName,
  runtimeModelsForCLI,
} from '@/utils/runtimeCatalog';
import { inputCls, SmallLabel } from './kit';
import { PlusIcon } from './teamsUi';

export function newRole(role = ''): RoleInput {
  return {
    role,
    cli: '',
    model: '',
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
  disabled,
}: {
  value: string;
  options: readonly { value: string; label: string }[];
  onChange: (v: string) => void;
  testId?: string;
  disabled?: boolean;
}): React.ReactElement {
  return (
    <select className={inputCls} value={value} data-testid={testId} disabled={disabled} onChange={(e) => onChange(e.target.value)}>
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
  const runtimeCatalog = useAIRuntimeCatalog();
  const cliOptions = enabledRuntimeCLIs(runtimeCatalog.data).map((cli) => ({
    value: cli.key,
    label: runtimeCLIName(cli),
  }));
  const defaultChoice = runtimeDefaultChoice(runtimeCatalog.data);

  useEffect(() => {
    if (!runtimeCatalog.data) return;
    let changed = false;
    const next = roles.map((role) => {
      const choice = normalizeRuntimeChoice(runtimeCatalog.data, { cli: role.cli, model: role.model }) ?? defaultChoice;
      if (!choice) return role;
      if (choice.cli === role.cli && choice.model === role.model) return role;
      changed = true;
      return { ...role, cli: choice.cli, model: choice.model };
    });
    if (changed) onChange(next);
  }, [defaultChoice, onChange, roles, runtimeCatalog.data]);

  const patch = (i: number, p: Partial<RoleInput>) => {
    onChange(roles.map((r, j) => (j === i ? { ...r, ...p } : r)));
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

          <div className="grid grid-cols-1 gap-2.5 md:grid-cols-[1fr_1fr_10rem]">
            <div>
              <SmallLabel>{t('roleBuilder.cliLabel')}</SmallLabel>
              <Select
                value={r.cli}
                options={withCurrentOption(cliOptions, r.cli)}
                disabled={cliOptions.length === 0}
                testId={`${idPrefix}-role-${i}-cli`}
                onChange={(v) => {
                  const nextModel = runtimeModelsForCLI(runtimeCatalog.data, v)[0]?.model_key ?? '';
                  patch(i, { cli: v, model: nextModel });
                }}
              />
            </div>
            <div>
              <SmallLabel>{t('roleBuilder.modelLabel')}</SmallLabel>
              <Select
                value={r.model}
                options={withCurrentOption(
                  runtimeModelsForCLI(runtimeCatalog.data, r.cli).map((model) => ({
                    value: model.model_key,
                    label: runtimeModelName(model),
                  })),
                  r.model,
                )}
                disabled={runtimeModelsForCLI(runtimeCatalog.data, r.cli).length === 0}
                testId={`${idPrefix}-role-${i}-model`}
                onChange={(v) => patch(i, { model: v })}
              />
            </div>
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

function withCurrentOption(
  options: readonly { value: string; label: string }[],
  current: string,
): { value: string; label: string }[] {
  if (!current || options.some((option) => option.value === current)) {
    return [...options];
  }
  return [{ value: current, label: current }, ...options];
}
