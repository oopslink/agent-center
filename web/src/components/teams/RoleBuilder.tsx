// Team WebUI — the declarative role-card builder shared by New Team and
// Instantiate. Each card configures one role slot: name, count, CLI, model,
// concurrency, tags, and (optionally) a description. Total slots = agents built.
import type React from 'react';
import { useTranslation } from 'react-i18next';
import { CLIS, MODELS, roleColor, ROLE_DESC, type RoleInput } from '@/api/teams';
import { inputCls, SmallLabel } from './kit';
import { PlusIcon } from './teamsUi';

export function newRole(role = ''): RoleInput {
  return {
    role,
    cli: 'claude-code',
    model: 'sonnet-5',
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
  idPrefix,
}: {
  roles: RoleInput[];
  onChange: (next: RoleInput[]) => void;
  showDescription?: boolean;
  idPrefix: string;
}): React.ReactElement {
  const { t } = useTranslation('teams');
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

          <div className="grid grid-cols-[1fr_1fr_5rem] gap-2.5">
            <div>
              <SmallLabel>{t('roleBuilder.cliLabel')}</SmallLabel>
              <Select value={r.cli} options={CLIS} testId={`${idPrefix}-role-${i}-cli`} onChange={(v) => patch(i, { cli: v })} />
            </div>
            <div>
              <SmallLabel>{t('roleBuilder.modelLabel')}</SmallLabel>
              <Select value={r.model} options={MODELS} testId={`${idPrefix}-role-${i}-model`} onChange={(v) => patch(i, { model: v })} />
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
