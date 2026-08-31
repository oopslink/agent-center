// Team WebUI — the declarative role-card builder shared by New Team,
// Instantiate, and role-definition edits. Count is team composition; existing
// team role definition edits can hide it while keeping per-agent defaults.
import type React from 'react';
import { useTranslation } from 'react-i18next';
import { useRAMRoles } from '@/api/access';
import { EntityMultiSelect } from '@/components/EntityMultiSelect';
import type { EntityOption } from '@/components/EntitySelect';
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
    ram_role_keys: [],
    access_requirements: [],
  };
}

export function RoleBuilder({
  roles,
  onChange,
  showDescription,
  showCount = true,
  idPrefix,
  ramRoleMode = 'keys',
  showRuntime = true,
  showTags = true,
  showAccess = true,
  allowStructureEdit = true,
  onDuplicateRole,
  onRemoveRole,
  canRemoveRole,
}: {
  roles: RoleInput[];
  onChange: (next: RoleInput[]) => void;
  showDescription?: boolean;
  showCount?: boolean;
  idPrefix: string;
  ramRoleMode?: 'keys' | 'ids';
  showRuntime?: boolean;
  showTags?: boolean;
  showAccess?: boolean;
  allowStructureEdit?: boolean;
  onDuplicateRole?: (index: number) => void;
  onRemoveRole?: (index: number) => void;
  canRemoveRole?: (index: number) => boolean;
}): React.ReactElement {
  const { t } = useTranslation('teams');
  const runtimeCatalog = useRuntimeSelectorCatalog();
  const ramRolesQuery = useRAMRoles();
  const patch = (i: number, p: Partial<RoleInput>) => {
    onChange(roles.map((r, j) => (j === i ? { ...r, ...p } : r)));
  };
  const remove = (i: number) => onChange(roles.filter((_, j) => j !== i));
  const duplicate = (i: number) => {
    const original = roles[i];
    onChange([
      ...roles.slice(0, i + 1),
      { ...original, role: `${original.role}-copy` },
      ...roles.slice(i + 1),
    ]);
  };
  const add = () => onChange([...roles, newRole()]);

  return (
    <div data-testid={`${idPrefix}-rolebuilder`}>
      {roles.map((r, i) => {
        const runtimeInvalid =
          !runtimeCatalog.isLoading &&
          (Boolean(runtimeCatalog.error) || !isSelectableRuntimePair(runtimeCatalog.catalog, r.cli, r.model, 'model-key'));
        const selectedRamRoles = ramRoleMode === 'ids' ? r.ram_role_ids ?? [] : r.ram_role_keys ?? [];
        const ramRoles = ramRolesQuery.data?.roles ?? [];
        const selectedRAMRoles = ramRoles.filter((role) => selectedRamRoles.includes(ramRoleValue(role, ramRoleMode)));
        const ramRoleOptions: EntityOption[] = ramRoles.map((role) => ({
          value: ramRoleValue(role, ramRoleMode),
          label: `${role.name} v${role.version}`,
          hint: role.description,
        }));
        const selectedPermissions = uniqueSorted([
          ...(r.access_requirements ?? []),
          ...selectedRAMRoles.flatMap((role) => role.permissions),
        ]);
        const setRamRoles = (nextRefs: string[]) => {
          const nextRAMRoles = ramRoles.filter((role) => nextRefs.includes(ramRoleValue(role, ramRoleMode)));
          const nextPatch: Partial<RoleInput> = {
            access_requirements: uniqueSorted(nextRAMRoles.flatMap((role) => role.permissions)),
          };
          if (ramRoleMode === 'ids') {
            nextPatch.ram_role_ids = nextRefs;
            nextPatch.ram_role_keys = nextRAMRoles.map((role) => role.name);
          } else {
            nextPatch.ram_role_keys = nextRefs;
          }
          patch(i, nextPatch);
        };
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
              disabled={!allowStructureEdit}
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
            {allowStructureEdit && (
              <div className="ml-auto flex items-center gap-2">
                <button
                  type="button"
                  className="text-xs text-text-muted hover:text-brand"
                  data-testid={`${idPrefix}-role-${i}-duplicate`}
                  onClick={() => (onDuplicateRole ? onDuplicateRole(i) : duplicate(i))}
                >
                  {t('roleBuilder.duplicate')}
                </button>
                <button
                  type="button"
                  className="text-xs text-text-muted hover:text-danger disabled:cursor-not-allowed disabled:opacity-50"
                  data-testid={`${idPrefix}-role-${i}-remove`}
                  disabled={canRemoveRole ? !canRemoveRole(i) : false}
                  onClick={() => (onRemoveRole ? onRemoveRole(i) : remove(i))}
                >
                  {t('roleBuilder.remove')}
                </button>
              </div>
            )}
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

          {showRuntime && <div className="rounded border border-border-base bg-bg-base p-3" data-testid={`${idPrefix}-role-${i}-runtime-config`}>
            <div className="mb-2 flex items-center justify-between gap-2">
              <SmallLabel>{t('roleBuilder.runtimeSection')}</SmallLabel>
              <span className="text-[0.6875rem] text-text-muted">{t('roleBuilder.runtimeSource')}</span>
            </div>
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
          </div>}

          {showTags && <div className="mt-3">
            <SmallLabel>{t('roleBuilder.tagsLabel')}</SmallLabel>
            <input
              className={inputCls}
              value={r.tags}
              placeholder={t('roleBuilder.tagsPlaceholder')}
              data-testid={`${idPrefix}-role-${i}-tags`}
              onChange={(e) => patch(i, { tags: e.target.value })}
            />
          </div>}

          {showAccess && <div className="mt-3 rounded border border-border-base bg-bg-base p-3" data-testid={`${idPrefix}-role-${i}-access-editor`}>
            <div className="mb-2 flex items-center justify-between gap-2">
              <SmallLabel>{t('roleBuilder.permissionsSection')}</SmallLabel>
              <span className="text-[0.6875rem] text-text-muted">{t('roleBuilder.permissionsSource')}</span>
            </div>
            <div className="grid gap-2 md:grid-cols-[minmax(0,1fr)_10rem]">
              <div>
                <div className="rounded border border-border-base bg-bg-subtle p-2" data-testid={`${idPrefix}-role-${i}-ram-role-picker`}>
                  <div className="mb-1.5 flex items-center justify-between gap-2">
                    <span className="text-[0.6875rem] font-semibold text-text-secondary">{t('roleBuilder.ramRoleMultiLabel')}</span>
                    <span className="text-[0.6875rem] text-text-muted" data-testid={`${idPrefix}-role-${i}-ram-role-summary`}>
                      {t('roleBuilder.ramRoleSummary', { count: selectedRamRoles.length, permissions: selectedPermissions.length })}
                    </span>
                  </div>
                  {ramRolesQuery.isLoading && (
                    <p className="text-[0.6875rem] text-text-muted" data-testid={`${idPrefix}-role-${i}-ram-role-loading`}>
                      {t('roleBuilder.ramRoleLoading')}
                    </p>
                  )}
                  {ramRolesQuery.isError && (
                    <p className="text-[0.6875rem] text-danger" data-testid={`${idPrefix}-role-${i}-ram-role-error`}>
                      {t('roleBuilder.ramRoleError')}
                    </p>
                  )}
                  <EntityMultiSelect
                    testId={`${idPrefix}-role-${i}-ram-role`}
                    options={ramRoleOptions}
                    values={selectedRamRoles}
                    onChange={setRamRoles}
                    disabled={ramRolesQuery.isLoading || ramRolesQuery.isError}
                    placeholder={t('roleBuilder.ramRolePlaceholder')}
                    searchPlaceholder={t('roleBuilder.ramRoleSearch')}
                    emptyLabel={t('roleBuilder.ramRoleEmpty')}
                    ariaLabel={t('roleBuilder.ramRoleMultiLabel')}
                  />
                </div>
              </div>
              <div>
                <SmallLabel>{t('roleBuilder.accessRiskLabel')}</SmallLabel>
                <div className="rounded border border-border-base px-2 py-1.5 text-xs font-semibold text-text-secondary" data-testid={`${idPrefix}-role-${i}-access-count`}>
                  {t('roleBuilder.accessPermissionCount', { count: selectedPermissions.length })}
                </div>
              </div>
            </div>
            {selectedPermissions.length > 0 && (
              <div className="mt-2 flex flex-wrap gap-1" data-testid={`${idPrefix}-role-${i}-access-permissions`}>
                {selectedPermissions.map((permission) => (
                  <span key={permission} className="rounded border border-border-base bg-bg-subtle px-1.5 py-0.5 font-mono text-[0.625rem] text-text-secondary">
                    {permission}
                  </span>
                ))}
              </div>
            )}
            <p className="mt-2 text-[0.6875rem] leading-5 text-text-muted" data-testid={`${idPrefix}-role-${i}-access-scope`}>
              {t('roleBuilder.accessScopeHint', { role: r.role || t('roleBuilder.roleNamePlaceholder') })}
            </p>
            {(r.access_lint ?? []).map((lint) => (
              <p key={`${lint.permission ?? ''}:${lint.message}`} className="mt-1 text-[0.6875rem] text-danger" data-testid={`${idPrefix}-role-${i}-access-lint`}>
                {lint.permission ? `${lint.permission}: ${lint.message}` : lint.message}
              </p>
            ))}
          </div>}
        </div>
        );
      })}
      {allowStructureEdit && (
        <button
          type="button"
          className="flex w-full items-center justify-center gap-1.5 rounded-lg border border-dashed border-border-strong px-3 py-3 text-sm font-semibold text-text-muted hover:border-accent hover:bg-brand/5 hover:text-brand"
          data-testid={`${idPrefix}-add-role`}
          onClick={add}
        >
          <PlusIcon className="h-4 w-4" /> {t('roleBuilder.addRole')}
        </button>
      )}
    </div>
  );
}

export function totalSlots(roles: RoleInput[]): number {
  return roles.reduce((s, r) => s + r.count, 0);
}

function ramRoleValue(role: { id: string; name: string }, mode: 'keys' | 'ids'): string {
  return mode === 'ids' ? role.id : role.name;
}

function uniqueSorted(values: string[]): string[] {
  return Array.from(new Set(values.map((value) => value.trim()).filter(Boolean))).sort();
}
