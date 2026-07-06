import type React from 'react';
import { useState } from 'react';
import { useTranslation } from 'react-i18next';
import { EntityRef } from '@/components/EntityRef';
import { AgentConfigEditModal } from '@/components/AgentConfigEditModal';
import { normalizeIdentityRef } from '@/api/members';
import type { Agent, InstalledSkill, SkillLayer } from '@/api/types';
import { executorBadgeClass } from '@/components/executorProfiles';
import { lifecycleTimeLabelKey } from '@/utils/agentLifecycleTime';

// AgentProfile (v2.7.1 #228 PR(b)) — the Profile tab body. Three blocks:
//   1. Info card — Computer (bound worker name + connected status), Created,
//      Creator (#120 enrichment), Description.
//   2. Runtime config tags — CLI / Model / Reasoning / Mode / Provider are ALL
//      real persisted config now (T236); an empty field renders its value as
//      "Default" with the default badge. Editable via the "Edit" affordance →
//      AgentConfigEditModal (save + restart-to-apply).
//   3. Installed skills (issue-4a45e9cc) — the OBSERVED effective skill set the
//      agent-runtime reported, grouped by the four precedence layers (built-in /
//      plugin / user / project). Each skill shows its description and a "shadowed"
//      marker when a higher layer overrides the same name; when the agent's computer
//      is offline the panel shows when the set was last collected.
export function AgentProfile({ agent }: { agent: Agent }): React.ReactElement {
  const { t } = useTranslation('members');
  const installedSkills = agent.installed_skills ?? [];
  const computer = agent.computer;
  // T236: the Edit-config modal (model/cli/reasoning/mode/provider + restart).
  const [editingConfig, setEditingConfig] = useState(false);

  return (
    <div className="space-y-4" data-testid="agent-tabpanel-profile">
      {/* Edit config button — top-right icon. */}
      <div className="flex justify-end">
        <button
          type="button"
          className="flex items-center rounded border border-border-base px-2 py-1.5 text-text-secondary hover:bg-bg-subtle hover:text-text-primary"
          onClick={() => setEditingConfig(true)}
          data-testid="agent-profile-edit-config"
          title={t('agents.profile.edit')}
          aria-label={t('agents.profile.edit')}
        >
          <PencilIcon />
        </button>
      </div>
      {/* design1: two columns — left identity/config, right relationships.
          (v2.7.1 #240: the Message action moved to the page header.) */}
      <div className="grid gap-x-8 gap-y-4 md:grid-cols-2">
        {/* LEFT */}
        <div className="space-y-4">
          <Section label={t('agents.profile.displayName')}>
            <p className="text-sm font-semibold text-text-primary">{agent.name}</p>
          </Section>

          <Section label={t('agents.profile.description')}>
            <p className="text-sm text-text-primary" data-testid="agent-profile-description">
              {agent.description || <span className="italic text-text-muted">{t('agents.profile.descriptionNone')}</span>}
            </p>
          </Section>

          <Section label={t('agents.profile.info')}>
            <dl className="grid grid-cols-[5.5rem_1fr] gap-x-3 gap-y-1.5 text-sm text-text-primary">
              <dt className="text-text-muted">{t('agents.profile.computer')}</dt>
              <dd data-testid="agent-profile-computer">
                {computer ? (
                  <span className="inline-flex items-center gap-2">
                    <EntityRef id={computer.worker_id} name={computer.name || undefined} fallback={computer.worker_id} testId="agent-profile-computer-name" />
                    <span
                      className={`rounded px-1.5 py-0.5 text-[0.625rem] font-medium uppercase tracking-wide ${
                        computer.connected ? 'bg-success/10 text-success' : 'bg-bg-subtle text-text-muted'
                      }`}
                      data-testid="agent-profile-computer-status"
                      data-connected={computer.connected}
                    >
                      {t(`workers.detail.status.${computer.status}`, { defaultValue: computer.status })}
                    </span>
                  </span>
                ) : (
                  <span className="italic text-text-muted">{t('agents.profile.noWorker')}</span>
                )}
              </dd>

              <dt className="text-text-muted">{t('agents.profile.created')}</dt>
              <dd data-testid="agent-profile-created" title={agent.created_at}>
                {formatDate(agent.created_at)}
              </dd>

              {/* Lifecycle time: label follows state (started/restarted vs stopped). */}
              <dt className="text-text-muted">{t(lifecycleTimeLabelKey(agent.lifecycle))}</dt>
              <dd
                data-testid="agent-profile-lifecycle-time"
                title={agent.last_lifecycle_transition_at}
              >
                {agent.last_lifecycle_transition_at
                  ? formatDate(agent.last_lifecycle_transition_at)
                  : '—'}
              </dd>

              <dt className="text-text-muted">{t('agents.profile.creator')}</dt>
              <dd data-testid="agent-profile-creator">
                <EntityRef
                  id={agent.created_by}
                  name={agent.created_by_display_name || undefined}
                  fallback={normalizeIdentityRef(agent.created_by)}
                  testId="agent-profile-creator-ref"
                />
              </dd>
            </dl>
          </Section>

          <Section label={t('agents.profile.runtimeConfig')}>
            <div className="flex flex-wrap gap-2" data-testid="agent-profile-runtime">
              <ConfigTag label={t('agents.profile.tag.cli')} value={agent.cli || '—'} testId="agent-profile-tag-cli" />
              <ConfigTag label={t('agents.profile.tag.model')} value={agent.model || '—'} testId="agent-profile-tag-model" />
              {/* T236: real persisted values; empty → "Default" + default badge. */}
              <ConfigTag label={t('agents.profile.tag.reasoning')} value={agent.reasoning || t('agents.profile.valueDefault')} testId="agent-profile-tag-reasoning" isDefault={!agent.reasoning} />
              <ConfigTag label={t('agents.profile.tag.mode')} value={agent.mode || t('agents.profile.valueDefault')} testId="agent-profile-tag-mode" isDefault={!agent.mode} />
              <ConfigTag label={t('agents.profile.tag.provider')} value={agent.provider || t('agents.profile.valueDefault')} testId="agent-profile-tag-provider" isDefault={!agent.provider} />
              {/* T566 (issue-577a7b0e): auto-assign opt-out (default on). */}
              <ConfigTag
                label={t('agents.profile.tag.autoAssign')}
                value={(agent.auto_assignable ?? true) ? t('agents.profile.valueOn') : t('agents.profile.valueOff')}
                testId="agent-profile-tag-auto-assignable"
              />
            </div>

            {/* v2.18.1 (issue-8746a5b9): executor concurrency, read-only. */}
            <ConcurrencyTags agent={agent} />
            <EnvVarsView agent={agent} />
          </Section>
        </div>

        {/* RIGHT */}
        <div className="space-y-4">
          <InstalledSkillsPanel
            skills={installedSkills}
            offline={!computer || !computer.connected}
          />
        </div>
      </div>

      {editingConfig && (
        <AgentConfigEditModal agent={agent} onClose={() => setEditingConfig(false)} />
      )}
    </div>
  );
}

// Section — an uppercase label (with optional count) over its content, matching
// the design1 profile layout (DISPLAY NAME / INFO / SKILLS …).
function Section({
  label,
  count,
  testId,
  children,
}: {
  label: string;
  count?: number;
  testId?: string;
  children: React.ReactNode;
}): React.ReactElement {
  return (
    <section data-testid={testId}>
      <h3 className="mb-1.5 text-[0.6875rem] font-semibold uppercase tracking-wide text-text-muted">
        {label}
        {count !== undefined && <span className="ml-1 text-text-muted">({count})</span>}
      </h3>
      {children}
    </section>
  );
}

// SKILL_LAYERS is the fixed render order (precedence low→high) so the panel groups
// consistently regardless of the row order the API returns.
const SKILL_LAYERS: SkillLayer[] = ['built-in', 'plugin', 'user', 'project'];

// InstalledSkillsPanel renders the OBSERVED installed-skill set (issue-4a45e9cc)
// grouped by layer, each skill with its description and a "shadowed/overridden" marker.
// When the agent's computer is offline it surfaces the last-collected time so the
// operator knows the data may be stale.
function InstalledSkillsPanel({
  skills,
  offline,
}: {
  skills: InstalledSkill[];
  offline: boolean;
}): React.ReactElement {
  const { t } = useTranslation('members');
  // Most-recent collection time across the reported set (all rows share the batch
  // stamp, but Max is robust to any drift).
  const collectedAt = skills.reduce<string>((acc, s) => (s.collected_at > acc ? s.collected_at : acc), '');
  const byLayer = SKILL_LAYERS.map((layer) => ({
    layer,
    items: skills.filter((s) => s.layer === layer),
  })).filter((g) => g.items.length > 0);

  return (
    <Section label={t('agents.profile.skills')} count={skills.length} testId="agent-profile-skills">
      {offline && collectedAt && (
        <p className="mb-2 text-[0.6875rem] text-text-muted" data-testid="agent-profile-skills-collected">
          {t('agents.profile.skillsCollectedAt', { time: formatDate(collectedAt) })}
        </p>
      )}
      {byLayer.length > 0 ? (
        <div className="space-y-3">
          {byLayer.map((group) => (
            <div key={group.layer} data-testid={`agent-profile-skill-layer-${group.layer}`}>
              <h4 className="mb-1 flex items-center gap-1.5 text-[0.625rem] font-semibold uppercase tracking-wide text-text-muted">
                {t(`agents.profile.skillLayer.${group.layer}`)}
                <span className="text-text-muted">({group.items.length})</span>
              </h4>
              <ul className="space-y-1.5">
                {group.items.map((s) => (
                  <li
                    key={`${group.layer}:${s.name}`}
                    className={`rounded border px-2.5 py-1.5 ${
                      s.shadowed
                        ? 'border-dashed border-border-base bg-transparent opacity-60'
                        : 'border-border-base bg-bg-subtle'
                    }`}
                    data-testid="agent-profile-skill"
                    data-shadowed={s.shadowed}
                  >
                    <div className="flex items-center gap-1.5">
                      <span
                        className={`text-xs font-medium ${s.shadowed ? 'text-text-muted line-through' : 'text-text-primary'}`}
                        title={s.name}
                      >
                        {s.name}
                      </span>
                      {s.shadowed && (
                        <span
                          className="rounded bg-bg-elevated px-1 py-0.5 text-[0.5625rem] uppercase tracking-wide text-text-muted"
                          data-testid="agent-profile-skill-shadowed"
                        >
                          {t('agents.profile.skillShadowed')}
                        </span>
                      )}
                    </div>
                    {s.description && (
                      <p className="mt-0.5 text-[0.6875rem] leading-snug text-text-secondary" data-testid="agent-profile-skill-desc">
                        {s.description}
                      </p>
                    )}
                  </li>
                ))}
              </ul>
            </div>
          ))}
        </div>
      ) : (
        <p className="text-xs italic text-text-muted" data-testid="agent-profile-skills-empty">
          {t('agents.profile.skillsEmpty')}
        </p>
      )}
    </Section>
  );
}

// v2.18.1: executor concurrency, read-only. Renders the cap, the {cli·model}
// executor chips, and a concurrency-enabled badge. The "enabled" wording follows
// the "truly parallel" rule (effective cap ≥ 2) — a default agent (no executors)
// shows "single-active · cap 1".
function ConcurrencyTags({ agent }: { agent: Agent }): React.ReactElement {
  const { t } = useTranslation('members');
  const cap = agent.effective_concurrency_cap ?? 1;
  const maxConcurrent = agent.max_concurrent_tasks ?? 0;
  const executors = agent.allowed_executors ?? [];
  const parallel = cap >= 2;
  return (
    <div className="mt-3 border-t border-border-base pt-3" data-testid="agent-profile-concurrency">
      <div className="mb-2 flex flex-wrap items-center gap-2">
        <span
          className={`inline-flex items-center gap-1.5 rounded px-2 py-1 text-xs font-medium ${parallel ? 'bg-status-green-bg text-status-green-fg' : 'bg-bg-subtle text-text-muted'}`}
          data-testid="agent-profile-concurrency-badge"
          data-enabled={parallel}
        >
          {parallel ? t('agents.profile.concurrencyEnabled', { cap }) : t('agents.profile.concurrencySingle')}
        </span>
        <ConfigTag label={t('agents.profile.tag.maxConcurrent')} value={String(maxConcurrent)} testId="agent-profile-tag-max-concurrent" />
      </div>
      <div className="flex flex-wrap gap-2" data-testid="agent-profile-executors">
        <span className="self-center text-xs text-text-muted">{t('agents.profile.allowedExecutors')}</span>
        {executors.length > 0 ? (
          executors.map((e) => (
            <span
              key={`${e.cli}::${e.model}`}
              className="inline-flex items-center gap-1.5 rounded border border-border-base bg-bg-subtle px-2 py-1 text-xs"
              data-testid="agent-profile-executor-chip"
            >
              <span className={`rounded px-1 py-0.5 text-[0.5625rem] font-medium uppercase tracking-wide ${executorBadgeClass(e.cli)}`}>
                {e.cli}
              </span>
              <span className="font-mono text-text-primary">{e.model}</span>
            </span>
          ))
        ) : (
          <span className="self-center text-xs italic text-text-muted" data-testid="agent-profile-executors-empty">
            {t('agents.profile.executorsNone')}
          </span>
        )}
      </div>
    </div>
  );
}

function EnvVarsView({ agent }: { agent: Agent }): React.ReactElement {
  const { t } = useTranslation('members');
  const env = agent.env_vars ?? {};
  const keys = Object.keys(env).sort();
  return (
    <div className="mt-3 border-t border-border-base pt-3" data-testid="agent-profile-env-vars">
      <div className="mb-2 flex items-center gap-2">
        <span className="text-xs text-text-muted">{t('agents.profile.envVars')}</span>
        <span className="rounded bg-bg-subtle px-1.5 py-0.5 text-[0.625rem] font-medium text-text-muted" data-testid="agent-profile-env-count">
          {keys.length}
        </span>
      </div>
      {keys.length > 0 ? (
        <div className="flex flex-wrap gap-2">
          {keys.map((k) => (
            <span
              key={k}
              className="rounded border border-border-base bg-bg-subtle px-2 py-1 font-mono text-xs text-text-secondary"
              data-testid="agent-profile-env-key"
              title={k}
            >
              {k}
            </span>
          ))}
        </div>
      ) : (
        <span className="text-xs italic text-text-muted" data-testid="agent-profile-env-empty">
          {t('agents.profile.envVarsNone')}
        </span>
      )}
    </div>
  );
}

function ConfigTag({
  label,
  value,
  testId,
  isDefault,
}: {
  label: string;
  value: string;
  testId: string;
  isDefault?: boolean;
}): React.ReactElement {
  const { t } = useTranslation('members');
  return (
    <span
      className="inline-flex items-center gap-1.5 rounded border border-border-base bg-bg-subtle px-2 py-1 text-xs"
      data-testid={testId}
    >
      <span className="text-text-muted">{label}</span>
      <span className="font-mono text-text-primary">{value}</span>
      {isDefault && (
        <span className="rounded bg-bg-elevated px-1 py-0.5 text-[0.5625rem] uppercase tracking-wide text-text-muted">
          {t('agents.profile.defaultBadge')}
        </span>
      )}
    </span>
  );
}

function PencilIcon(): React.ReactElement {
  return (
    <svg viewBox="0 0 20 20" fill="none" className="h-4 w-4 stroke-current" strokeWidth="1.5" aria-hidden="true">
      <path d="M13.5 3.5l3 3L7 16H4v-3L13.5 3.5z" strokeLinecap="round" strokeLinejoin="round" />
    </svg>
  );
}

function formatDate(iso: string): string {
  const d = new Date(iso);
  return Number.isNaN(d.getTime()) ? iso : d.toLocaleString();
}
