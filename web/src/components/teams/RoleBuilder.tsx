// Team WebUI — the declarative role-card builder shared by New Team,
// Instantiate, and role-definition edits. Count is team composition; existing
// team role definition edits can hide it while keeping per-agent defaults.
import type React from 'react';
import { useTranslation } from 'react-i18next';
import { useAccessProfiles } from '@/api/access';
import { roleColor, ROLE_DESC, type RoleInput } from '@/api/teams';
import { inputCls, SmallLabel } from './kit';
import { PlusIcon } from './teamsUi';
import {
  firstRuntimeModelValue,
  isSelectableRuntimePair,
  RuntimeCLISelector,
  RuntimeModelCombobox,
  useRuntimeSelectorCatalog,
} from '@/components/RuntimeSelectors';

export function newRole(role = ''): RoleInput {
  return {
    role,
    cli: 'claude-code',
    model: 'sonnet-5',
    max_concurrency: 1,
    count: 1,
    tags: '',
    description: role ? ROLE_DESC[role] || '' : '',
    access_requirements: [],
  };
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
  const runtimeCatalog = useRuntimeSelectorCatalog();
  const accessProfiles = useAccessProfiles();
  const patch = (i: number, p: Partial<RoleInput>) => {
    onChange(roles.map((r, j) => (j === i ? { ...r, ...p } : r)));
  };
  const remove = (i: number) => onChange(roles.filter((_, j) => j !== i));
  const add = () => onChange([...roles, newRole()]);

  return (
    <div data-testid={`${idPrefix}-rolebuilder`}>
      {roles.map((r, i) => {
        const runtimeInvalid =
          !runtimeCatalog.isLoading &&
          (Boolean(runtimeCatalog.error) || !isSelectableRuntimePair(runtimeCatalog.catalog, r.cli, r.model, 'model-key'));
        return (
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
              <RuntimeCLISelector
                value={r.cli}
                onChange={(nextCli) => patch(i, {
                  cli: nextCli,
                  model: firstRuntimeModelValue(runtimeCatalog.catalog, nextCli, r.model, 'model-key'),
                })}
                testId={`${idPrefix}-role-${i}-cli`}
                ariaLabel={t('roleBuilder.cliLabel')}
                {...runtimeCatalog}
              />
            </div>
            <div>
              <SmallLabel>{t('roleBuilder.modelLabel')}</SmallLabel>
              <RuntimeModelCombobox
                value={r.model}
                onChange={(model) => patch(i, { model })}
                cliKey={r.cli}
                valueMode="model-key"
                testId={`${idPrefix}-role-${i}-model`}
                ariaLabel={t('roleBuilder.modelLabel')}
                {...runtimeCatalog}
              />
              {runtimeInvalid && (
                <p className="mt-1 text-[0.6875rem] text-danger" data-testid={`${idPrefix}-role-${i}-runtime-error`}>
                  {Boolean(runtimeCatalog.error)
                    ? t('roleBuilder.runtimeCatalogUnavailable')
                    : t('roleBuilder.runtimeSelectionRequired')}
                </p>
              )}
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

          <div className="mt-3 rounded border border-border-base bg-bg-base p-3" data-testid={`${idPrefix}-role-${i}-access-editor`}>
            <div className="grid gap-2 md:grid-cols-[minmax(0,1fr)_10rem]">
              <div>
                <SmallLabel>{t('roleBuilder.accessProfileLabel')}</SmallLabel>
                <select
                  className={inputCls}
                  value={profileValue(r.access_requirements ?? [], accessProfiles.data?.profiles ?? [])}
                  data-testid={`${idPrefix}-role-${i}-access-profile`}
                  onChange={(e) => {
                    const profile = accessProfiles.data?.profiles.find((p) => `${p.id}@${p.version}` === e.target.value);
                    patch(i, { access_requirements: profile ? profile.permissions : [] });
                  }}
                  disabled={accessProfiles.isLoading}
                >
                  <option value="">{t('roleBuilder.accessProfileNone')}</option>
                  {(accessProfiles.data?.profiles ?? []).map((profile) => (
                    <option key={`${profile.id}@${profile.version}`} value={`${profile.id}@${profile.version}`}>
                      {t('roleBuilder.accessProfileOption', { name: profile.name, version: profile.version })}
                    </option>
                  ))}
                </select>
              </div>
              <div>
                <SmallLabel>{t('roleBuilder.accessRiskLabel')}</SmallLabel>
                <div className="rounded border border-border-base px-2 py-1.5 text-xs font-semibold text-text-secondary" data-testid={`${idPrefix}-role-${i}-access-count`}>
                  {t('roleBuilder.accessPermissionCount', { count: (r.access_requirements ?? []).length })}
                </div>
              </div>
            </div>
            {(r.access_requirements ?? []).length > 0 && (
              <div className="mt-2 flex flex-wrap gap-1" data-testid={`${idPrefix}-role-${i}-access-permissions`}>
                {(r.access_requirements ?? []).map((permission) => (
                  <span key={permission} className="rounded border border-border-base bg-bg-subtle px-1.5 py-0.5 font-mono text-[0.625rem] text-text-secondary">
                    {permission}
                  </span>
                ))}
              </div>
            )}
            {(r.access_lint ?? []).map((lint) => (
              <p key={`${lint.permission ?? ''}:${lint.message}`} className="mt-1 text-[0.6875rem] text-danger" data-testid={`${idPrefix}-role-${i}-access-lint`}>
                {lint.permission ? `${lint.permission}: ${lint.message}` : lint.message}
              </p>
            ))}
          </div>
        </div>
        );
      })}
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

function profileValue(requirements: string[], profiles: Array<{ id: string; version: number; permissions: string[] }>): string {
  const normalized = requirements.slice().sort().join('\n');
  const profile = profiles.find((p) => p.permissions.slice().sort().join('\n') === normalized);
  return profile ? `${profile.id}@${profile.version}` : '';
}
