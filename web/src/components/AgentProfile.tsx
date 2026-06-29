import type React from 'react';
import { useState } from 'react';
import { OrgLink } from '@/OrgContext';
import { EntityRef } from '@/components/EntityRef';
import { AgentConfigEditModal } from '@/components/AgentConfigEditModal';
import { normalizeIdentityRef } from '@/api/members';
import type { Agent } from '@/api/types';
import { executorBadgeClass } from '@/components/executorProfiles';

// AgentProfile (v2.7.1 #228 PR(b)) — the Profile tab body. Three blocks:
//   1. Info card — Computer (bound worker name + connected status), Created,
//      Creator (#120 enrichment), Description.
//   2. Runtime config tags — CLI / Model / Reasoning / Mode / Provider are ALL
//      real persisted config now (T236); an empty field renders its value as
//      "Default" with the default badge. Editable via the "Edit" affordance →
//      AgentConfigEditModal (save + restart-to-apply).
//   3. Skills name cards (no global/local badge or path — that origin metadata
//      is v2.8 #230) + Created-agents list (#120) + a Message button that opens
//      a DM with the agent.
export function AgentProfile({ agent }: { agent: Agent }): React.ReactElement {
  const skills = agent.skills ?? [];
  const createdAgents = agent.created_agents ?? [];
  const computer = agent.computer;
  // T236: the Edit-config modal (model/cli/reasoning/mode/provider + restart).
  const [editingConfig, setEditingConfig] = useState(false);

  return (
    <div className="space-y-4" data-testid="agent-tabpanel-profile">
      {/* design1: two columns — left identity/config, right relationships.
          (v2.7.1 #240: the Message action moved to the page header.) */}
      <div className="grid gap-x-8 gap-y-4 md:grid-cols-2">
        {/* LEFT */}
        <div className="space-y-4">
          <Section label="Display name">
            <p className="text-sm font-semibold text-text-primary">{agent.name}</p>
          </Section>

          <Section label="Description">
            <p className="text-sm text-text-primary" data-testid="agent-profile-description">
              {agent.description || <span className="italic text-text-muted">none</span>}
            </p>
          </Section>

          <Section label="Info">
            <dl className="grid grid-cols-[5.5rem_1fr] gap-x-3 gap-y-1.5 text-sm text-text-primary">
              <dt className="text-text-muted">Computer</dt>
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
                      {computer.status}
                    </span>
                  </span>
                ) : (
                  <span className="italic text-text-muted">no worker</span>
                )}
              </dd>

              <dt className="text-text-muted">Created</dt>
              <dd data-testid="agent-profile-created" title={agent.created_at}>
                {formatDate(agent.created_at)}
              </dd>

              <dt className="text-text-muted">Creator</dt>
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

          <Section label="Runtime config">
            <div className="mb-2 flex justify-end">
              <button
                type="button"
                className="rounded border border-border-base px-2 py-0.5 text-xs text-text-secondary hover:bg-bg-subtle hover:text-text-primary"
                onClick={() => setEditingConfig(true)}
                data-testid="agent-profile-edit-config"
              >
                Edit
              </button>
            </div>
            <div className="flex flex-wrap gap-2" data-testid="agent-profile-runtime">
              <ConfigTag label="CLI" value={agent.cli || '—'} testId="agent-profile-tag-cli" />
              <ConfigTag label="Model" value={agent.model || '—'} testId="agent-profile-tag-model" />
              {/* T236: real persisted values; empty → "Default" + default badge. */}
              <ConfigTag label="Reasoning" value={agent.reasoning || 'Default'} testId="agent-profile-tag-reasoning" isDefault={!agent.reasoning} />
              <ConfigTag label="Mode" value={agent.mode || 'Default'} testId="agent-profile-tag-mode" isDefault={!agent.mode} />
              <ConfigTag label="Provider" value={agent.provider || 'Default'} testId="agent-profile-tag-provider" isDefault={!agent.provider} />
              {/* T566 (issue-577a7b0e): auto-assign opt-out (default on). */}
              <ConfigTag
                label="Auto-assign"
                value={(agent.auto_assignable ?? true) ? 'On' : 'Off'}
                testId="agent-profile-tag-auto-assignable"
              />
            </div>

            {/* v2.18.1 (issue-8746a5b9): executor concurrency, read-only. */}
            <ConcurrencyTags agent={agent} />
          </Section>
        </div>

        {/* RIGHT */}
        <div className="space-y-4">
          <Section label="Created agents" count={createdAgents.length} testId="agent-profile-created-agents">
            {createdAgents.length > 0 ? (
              <ul className="flex flex-wrap gap-2">
                {createdAgents.map((c) => (
                  <li key={c.id} data-testid="agent-profile-created-agent">
                    <OrgLink
                      to={`/agents/${encodeURIComponent(c.id)}`}
                      className="rounded border border-border-base px-2.5 py-1 text-xs text-text-secondary hover:text-accent"
                      title={c.id}
                    >
                      {c.name || c.id}
                    </OrgLink>
                  </li>
                ))}
              </ul>
            ) : (
              <p className="text-xs italic text-text-muted" data-testid="agent-profile-created-agents-empty">
                No created agents.
              </p>
            )}
          </Section>

          <Section label="Skills" count={skills.length} testId="agent-profile-skills">
            {skills.length > 0 ? (
              // design1: a card grid of skill names. Skill source/description
              // (global/local · path) is v2.8 #230 → name-only cards here.
              <ul className="grid grid-cols-2 gap-2">
                {skills.map((s) => (
                  <li
                    key={s}
                    className="truncate rounded border border-border-base bg-bg-subtle px-2.5 py-1.5 text-xs font-medium text-text-secondary"
                    data-testid="agent-profile-skill"
                    title={s}
                  >
                    {s}
                  </li>
                ))}
              </ul>
            ) : (
              <p className="text-xs italic text-text-muted" data-testid="agent-profile-skills-empty">
                No skills.
              </p>
            )}
          </Section>
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

// v2.18.1: executor concurrency, read-only. Renders the cap, the {cli·model}
// executor chips, and a concurrency-enabled badge. The "enabled" wording follows
// the "truly parallel" rule (effective cap ≥ 2) — a default agent (no executors)
// shows "single-active · cap 1".
function ConcurrencyTags({ agent }: { agent: Agent }): React.ReactElement {
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
          {parallel ? `concurrency · cap ${cap}` : 'single-active · cap 1'}
        </span>
        <ConfigTag label="Max concurrent" value={String(maxConcurrent)} testId="agent-profile-tag-max-concurrent" />
      </div>
      <div className="flex flex-wrap gap-2" data-testid="agent-profile-executors">
        <span className="self-center text-xs text-text-muted">Allowed executors</span>
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
            none
          </span>
        )}
      </div>
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
  return (
    <span
      className="inline-flex items-center gap-1.5 rounded border border-border-base bg-bg-subtle px-2 py-1 text-xs"
      data-testid={testId}
    >
      <span className="text-text-muted">{label}</span>
      <span className="font-mono text-text-primary">{value}</span>
      {isDefault && (
        <span className="rounded bg-bg-elevated px-1 py-0.5 text-[0.5625rem] uppercase tracking-wide text-text-muted">
          default
        </span>
      )}
    </span>
  );
}

function formatDate(iso: string): string {
  const d = new Date(iso);
  return Number.isNaN(d.getTime()) ? iso : d.toLocaleString();
}
