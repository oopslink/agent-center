import type React from 'react';
import { useAgent } from '@/api/agents';
import { useUser } from '@/api/users';
import {
  normalizeIdentityRef,
  useDisplayNameResolver,
  useMembers,
} from '@/api/members';
import { useModalA11y } from './useModalA11y';
import { Avatar } from './Avatar';
import { LifecycleBadge, AvailabilityBadge } from './AgentBadges';

// SenderDetailSidebar (v2.8.1 7th DM redesign, increment 2). A right slide-in
// panel that surfaces a message sender's detail when their avatar/name is
// clicked in MessageList. Kind is dispatched off the identity-ref prefix
// (agent:/user:) — agents resolve via useAgent, humans via useUser.
//
// a11y: mounts useModalA11y (Escape close + Tab focus-trap + focus-restore),
// role="dialog" + aria-modal + aria-label, a real <button> close affordance
// (ASCII × glyph, NOT an emoji per the no-emoji-icon guardrail). The overlay
// is dimmed-but-visible and click-to-close. The slide-in transition is
// disabled under motion-reduce.

interface Props {
  open: boolean;
  /** an identity ref like `agent:<id>` or `user:<id>`; null when closed. */
  senderRef: string | null;
  onClose: () => void;
}

// senderKind classifies the ref by its prefix. Bare/unknown refs (no prefix)
// fall through to 'user' — the common human case — so the panel still resolves
// a name rather than rendering an empty shell.
function senderKind(ref: string): 'agent' | 'user' {
  return ref.startsWith('agent:') ? 'agent' : 'user';
}

export function SenderDetailSidebar({
  open,
  senderRef,
  onClose,
}: Props): React.ReactElement | null {
  const containerRef = useModalA11y({ open, onClose });
  const displayName = useDisplayNameResolver();

  const ref = senderRef ?? '';
  const kind = senderKind(ref);
  const id = ref ? normalizeIdentityRef(ref) : '';

  // Both queries are gated on `open` + kind so only the relevant one fires.
  const agentQuery = useAgent(open && kind === 'agent' && id ? id : undefined);
  const userQuery = useUser(open && kind === 'user' && id ? id : undefined);

  if (!open || !senderRef) return null;

  const name = displayName(senderRef);

  return (
    <>
      {/* Dimmed-but-visible overlay — click to close. */}
      <div
        className="fixed inset-0 z-30 bg-black/30"
        data-testid="sender-sidebar-overlay"
        onClick={onClose}
        aria-hidden="true"
      />
      <div
        ref={containerRef}
        role="dialog"
        aria-modal="true"
        aria-label={`${name} detail`}
        data-testid="sender-sidebar"
        className="fixed inset-y-0 right-0 z-40 flex h-full w-80 translate-x-0 transform flex-col border-l border-border-base bg-bg-elevated text-text-primary shadow-2 transition-transform duration-200 ease-out motion-reduce:transition-none sm:w-96"
      >
        {/* Header: avatar + resolved display name + close button. */}
        <div className="flex items-start gap-3 border-b border-border-base p-4">
          <Avatar name={name} kind={kind === 'agent' ? 'agent' : 'human'} size="lg" />
          <div className="min-w-0 flex-1">
            <div className="truncate text-base font-semibold" title={senderRef}>
              {name}
            </div>
            <div className="text-xs uppercase tracking-wide text-text-muted">
              {kind === 'agent' ? 'Agent' : 'User'}
            </div>
          </div>
          <button
            type="button"
            onClick={onClose}
            data-testid="sender-sidebar-close"
            aria-label="Close sender detail"
            className="rounded p-1 text-text-muted hover:bg-bg-subtle hover:text-text-primary focus-visible:ring-2 focus-visible:ring-accent"
          >
            {/* plain ASCII "X" close glyph (per the #208 lesson — not ✕/U+2715,
                which is in the a11y guardrail's pictograph range). aria-hidden;
                the button's aria-label carries the accessible name. */}
            <span aria-hidden="true">X</span>
          </button>
        </div>

        {/* Body: kind-dispatched detail. */}
        <div className="flex-1 overflow-y-auto p-4">
          {kind === 'agent' ? (
            <AgentDetailBody query={agentQuery} />
          ) : (
            <UserDetailBody query={userQuery} memberRef={senderRef} />
          )}
        </div>
      </div>
    </>
  );
}

// LabelRow — a small label/value pair used across both detail bodies.
function LabelRow({
  label,
  value,
}: {
  label: string;
  value: React.ReactNode;
}): React.ReactElement {
  return (
    <div className="flex flex-col gap-0.5">
      <span className="text-xs uppercase tracking-wide text-text-muted">{label}</span>
      <span className="text-sm text-text-primary">{value}</span>
    </div>
  );
}

function StateMessage({ children }: { children: React.ReactNode }): React.ReactElement {
  return <div className="text-sm text-text-muted">{children}</div>;
}

function AgentDetailBody({
  query,
}: {
  query: ReturnType<typeof useAgent>;
}): React.ReactElement {
  if (query.isLoading) return <StateMessage>Loading agent…</StateMessage>;
  if (query.isError) return <StateMessage>Couldn&apos;t load this agent.</StateMessage>;
  const agent = query.data;
  if (!agent) return <StateMessage>Agent not found.</StateMessage>;

  return (
    <div className="flex flex-col gap-4" data-testid="sender-sidebar-agent">
      <div className="flex flex-wrap items-center gap-2">
        <LifecycleBadge lifecycle={agent.lifecycle} />
        <AvailabilityBadge availability={agent.availability} />
      </div>
      <LabelRow label="Name" value={agent.name} />
      <LabelRow label="Model" value={agent.model || '—'} />
      <LabelRow label="CLI" value={agent.cli || '—'} />
      <LabelRow
        label="Worker"
        value={agent.computer?.name || agent.worker_id || '—'}
      />
      {agent.description && <LabelRow label="Description" value={agent.description} />}
    </div>
  );
}

function UserDetailBody({
  query,
  memberRef,
}: {
  query: ReturnType<typeof useUser>;
  memberRef: string;
}): React.ReactElement {
  // Role is enrichment-only: looked up from the org members list (cheap, already
  // cached) keyed by the normalized identity ref. Absent => omitted.
  const members = useMembers();
  const key = normalizeIdentityRef(memberRef);
  const role = (members.data ?? []).find(
    (m) => normalizeIdentityRef(m.identity_id) === key,
  )?.role;

  if (query.isLoading) return <StateMessage>Loading user…</StateMessage>;
  if (query.isError) return <StateMessage>Couldn&apos;t load this user.</StateMessage>;
  const user = query.data;
  if (!user) return <StateMessage>User not found.</StateMessage>;

  return (
    <div className="flex flex-col gap-4" data-testid="sender-sidebar-user">
      <LabelRow label="Name" value={user.display_name} />
      <LabelRow label="Kind" value="User" />
      {role && <LabelRow label="Role" value={role} />}
      {user.email && <LabelRow label="Email" value={user.email} />}
    </div>
  );
}
