import type React from 'react';
import { OrgLink } from '@/OrgContext';
import { EntityRef } from '@/components/EntityRef';
import { normalizeIdentityRef } from '@/api/members';
import type { Agent } from '@/api/types';

// AgentProfile (v2.7.1 #228 PR(b)) — the Profile tab body. Three blocks:
//   1. Info card — Computer (bound worker name + connected status), Created,
//      Creator (#120 enrichment), Description.
//   2. Runtime config tags — CLI + Model are real; reasoning/mode/provider are
//      static v2.7.1 fallbacks ("Medium"/"Default"/"Default") because the schema
//      doesn't model them yet (real values = v2.8 #229). We never fabricate them
//      as if they were stored — they're labelled as defaults.
//   3. Skills name cards (no global/local badge or path — that origin metadata
//      is v2.8 #230) + Created-agents list (#120) + a Message button that opens
//      a DM with the agent.
export function AgentProfile({ agent }: { agent: Agent }): React.ReactElement {
  const skills = agent.skills ?? [];
  const createdAgents = agent.created_agents ?? [];
  const computer = agent.computer;

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
            <div className="flex flex-wrap gap-2" data-testid="agent-profile-runtime">
              <ConfigTag label="CLI" value={agent.cli || '—'} testId="agent-profile-tag-cli" />
              <ConfigTag label="Model" value={agent.model || '—'} testId="agent-profile-tag-model" />
              {/* v2.7.1 static fallbacks — real values are v2.8 #229 schema work. */}
              <ConfigTag label="Reasoning" value="Medium" testId="agent-profile-tag-reasoning" isDefault />
              <ConfigTag label="Mode" value="Default" testId="agent-profile-tag-mode" isDefault />
              <ConfigTag label="Provider" value="Default" testId="agent-profile-tag-provider" isDefault />
            </div>
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
