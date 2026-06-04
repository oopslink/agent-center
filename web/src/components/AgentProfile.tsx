import type React from 'react';
import { useNavigate } from 'react-router-dom';
import { OrgLink, useOptionalOrgContext } from '@/OrgContext';
import { useCreateConversation } from '@/api/conversations';
import { EntityRef } from '@/components/EntityRef';
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
  const navigate = useNavigate();
  const org = useOptionalOrgContext();
  const createDm = useCreateConversation();

  const messageAgent = async () => {
    if (createDm.isPending) return;
    try {
      const res = await createDm.mutateAsync({ kind: 'dm', members: [agent.id] });
      const slug = org?.slug;
      navigate(slug ? `/organizations/${slug}/dms/${res.conversation_id}` : `/dms/${res.conversation_id}`);
    } catch {
      // error surfaces via createDm.error below
    }
  };

  const skills = agent.skills ?? [];
  const createdAgents = agent.created_agents ?? [];
  const computer = agent.computer;

  return (
    <div className="space-y-4" data-testid="agent-tabpanel-profile">
      {/* Info card */}
      <dl className="grid grid-cols-[8rem_1fr] gap-x-4 gap-y-2 rounded border border-border-base bg-bg-elevated p-4 text-sm text-text-primary">
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

        <dt className="text-text-muted">Creator</dt>
        <dd data-testid="agent-profile-creator">
          <EntityRef
            id={agent.created_by}
            name={agent.created_by_display_name || undefined}
            fallback={agent.created_by}
            testId="agent-profile-creator-ref"
          />
        </dd>

        <dt className="text-text-muted">Created</dt>
        <dd data-testid="agent-profile-created" title={agent.created_at}>
          {formatDate(agent.created_at)}
        </dd>

        <dt className="text-text-muted">Description</dt>
        <dd data-testid="agent-profile-description">
          {agent.description || <span className="italic text-text-muted">none</span>}
        </dd>
      </dl>

      {/* Runtime config */}
      <section className="rounded border border-border-base bg-bg-elevated p-4" data-testid="agent-profile-runtime">
        <h3 className="mb-3 text-sm font-semibold text-text-primary">Runtime config</h3>
        <dl className="flex flex-wrap gap-x-6 gap-y-3 text-sm">
          <ConfigTag label="CLI" value={agent.cli || '—'} testId="agent-profile-tag-cli" />
          <ConfigTag label="Model" value={agent.model || '—'} testId="agent-profile-tag-model" />
          {/* v2.7.1 static fallbacks — real values are v2.8 #229 schema work. */}
          <ConfigTag label="Reasoning" value="Medium" testId="agent-profile-tag-reasoning" isDefault />
          <ConfigTag label="Mode" value="Default" testId="agent-profile-tag-mode" isDefault />
          <ConfigTag label="Provider" value="Default" testId="agent-profile-tag-provider" isDefault />
        </dl>
      </section>

      {/* Skills */}
      <section className="rounded border border-border-base bg-bg-elevated p-4" data-testid="agent-profile-skills">
        <h3 className="mb-3 text-sm font-semibold text-text-primary">Skills</h3>
        {skills.length > 0 ? (
          <ul className="flex flex-wrap gap-2">
            {skills.map((s) => (
              <li
                key={s}
                className="rounded border border-border-base bg-bg-subtle px-2.5 py-1 text-xs text-text-secondary"
                data-testid="agent-profile-skill"
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
      </section>

      {/* Created agents */}
      <section className="rounded border border-border-base bg-bg-elevated p-4" data-testid="agent-profile-created-agents">
        <h3 className="mb-3 text-sm font-semibold text-text-primary">Created agents</h3>
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
      </section>

      {/* Message */}
      <div className="flex items-center gap-3">
        <button
          type="button"
          onClick={() => void messageAgent()}
          disabled={createDm.isPending}
          className="rounded bg-brand px-3 py-1.5 text-sm font-medium text-white hover:bg-brand-hover disabled:opacity-50"
          data-testid="agent-profile-message-btn"
        >
          {createDm.isPending ? 'Opening…' : 'Message'}
        </button>
        {createDm.isError && (
          <span className="text-xs text-danger" data-testid="agent-profile-message-error">
            {(createDm.error as Error).message}
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
    <div className="flex flex-col gap-1" data-testid={testId}>
      <dt className="text-[0.6875rem] uppercase tracking-wide text-text-muted">{label}</dt>
      <dd className="flex items-center gap-1.5">
        <span className="font-mono text-xs text-text-primary">{value}</span>
        {isDefault && (
          <span className="rounded bg-bg-subtle px-1 py-0.5 text-[0.5625rem] uppercase tracking-wide text-text-muted">
            default
          </span>
        )}
      </dd>
    </div>
  );
}

function formatDate(iso: string): string {
  const d = new Date(iso);
  return Number.isNaN(d.getTime()) ? iso : d.toLocaleString();
}
