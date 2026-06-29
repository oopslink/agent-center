import { useMemo } from 'react';
import { useQuery, useMutation, useQueryClient } from '@tanstack/react-query';
import { api } from './client';
import { currentOrgScope, qk } from './queryKeys';

// membersKey is org-scoped so switching orgs / tabs doesn't reuse cached members.
const membersKey = () => ['org', currentOrgScope(), 'members'] as const;

export interface MemberResult {
  id: string;
  organization_id: string;
  identity_id: string;
  kind: 'user' | 'agent';
  role: 'owner' | 'admin' | 'member';
  status: 'joined' | 'disabled';
  joined_at: string;
  worker_id?: string; // present for agent members bound to an AgentInstance
  // v2.7 #160: resolved from the Identity so the UI can show a human name
  // instead of the raw identity ref. May be absent for legacy/unresolvable rows.
  display_name?: string;
  // v2.7.1 #193: Humans list columns. email/last_session_at nullable (v2.7.0
  // upgrade users have no email; a user who never signed in has no session).
  email?: string;
  created_at?: string;
  last_session_at?: string;
}

// normalizeIdentityRef strips the "user:"/"agent:" prefix so a prefixed ref
// (message sender_identity_id / conversation participant, e.g. "user:user-ab12")
// keys to the same value as a bare member identity_id ("user-ab12"). v2.7 #160.
export function normalizeIdentityRef(ref: string): string {
  if (ref.startsWith('user:')) return ref.slice('user:'.length);
  if (ref.startsWith('agent:')) return ref.slice('agent:'.length);
  return ref;
}

// v2.10.0 [T75] — the canonical system/scheduler sender. The Plan orchestrator
// speaks for the Plan when it posts dispatch/advance notifications: the backend
// PlanDispatchAdapter posts them with SenderIdentityID "system" (content_kind=
// text — a real, visible @mention that wakes the assignee, NOT a collapsed
// system row). "system" is NOT an org member, so without an explicit mapping the
// display-name resolver misses → call sites render the unresolved "(deleted)"
// branch (the owner-reported bug). Resolve it to a stable "System" author.
export const SYSTEM_SENDER_REF = 'system';
export const SYSTEM_DISPLAY_NAME = 'System';

// isSystemSender reports whether an identity ref is the system/scheduler sender
// (bare or, defensively, prefix-stripped to the same sentinel).
export function isSystemSender(ref: string): boolean {
  return normalizeIdentityRef(ref) === SYSTEM_SENDER_REF;
}

// identityRefOf builds the prefixed identity ref ("agent:<id>" / "user:<id>")
// from a member-like value — the inverse of normalizeIdentityRef. The id is
// normalized first so an already-prefixed identity_id is not double-prefixed.
// Consolidates the per-component `refOf` duplication (DMStartModal /
// MemberInviteModal / ProjectMemberAddModal / AppLayout / MentionText). v2.9 #254.
export function identityRefOf(m: { kind: 'user' | 'agent'; identity_id: string }): string {
  return `${m.kind === 'agent' ? 'agent:' : 'user:'}${normalizeIdentityRef(m.identity_id)}`;
}

// refKind reads the kind from an identity ref. Matches MemberResult.kind. v2.9
// #254. NOTE: UI sites that map to the 'agent' | 'human' avatar-kind contract
// (ParticipantsPanel / MessageList) intentionally keep their own mapping —
// different return type.
//
// T346: also recognize the BARE agent id form. An agent member's bare identity_id
// IS `agent-<hex>` (the "agent-" is part of the id, not a strippable kind prefix);
// the prefixed form is `agent:agent-<id>`. A caller that passes the bare ref (e.g.
// the activity sidebar) was misclassified as 'user' → the SenderDetailSidebar hit
// useUser → "This user is unavailable (deleted)" for a perfectly live agent
// (@oopslink). Bare user ids are `user-<id>`, so this never misreads a user.
export function refKind(ref: string): 'user' | 'agent' {
  return ref.startsWith('agent:') || ref.startsWith('agent-') ? 'agent' : 'user';
}

export interface AddUserResult extends MemberResult {
  temp_passcode?: string;
  display_name?: string;
  // v2.7 #157: present when Members→Add Agent did the unified one-step create
  // (also created the execution Agent). The UI navigates to its AgentDetail.
  agent_id?: string;
}

// AddAgentMemberPayload — v2.7 #157: when model/cli/worker_id are present the
// backend does the UNIFIED one-step create (agent identity-member + execution
// Agent, atomically). worker_id is required for the execution agent.
export interface AddAgentMemberPayload {
  display_name: string;
  description?: string;
  role?: string;
  model?: string;
  cli?: string;
  worker_id?: string;
  skills?: string[];
}

export const membersApi = {
  list: () => api.get<MemberResult[]>('/members'),
  add: (payload: { display_name: string; role?: string; reuse?: boolean }) =>
    api.post<AddUserResult>('/members', payload),
  addAgent: (payload: AddAgentMemberPayload) =>
    api.post<AddUserResult>('/members/agent', payload),
  changeRole: (id: string, role: string) =>
    api.patch<void>(`/members/${id}/role`, { role }),
  disable: (id: string, reason?: string) =>
    api.post<void>(`/members/${id}/disable`, { reason: reason ?? '' }),
  reEnable: (id: string) => api.post<void>(`/members/${id}/reenable`),
  drop: (id: string) => api.del<void>(`/members/${id}`),
};

export function useMembers() {
  return useQuery({
    queryKey: membersKey(),
    queryFn: () => membersApi.list(),
    staleTime: 30_000,
  });
}

// displayNameFallback produces a CLEAN handle for an identity ref that does NOT
// resolve to a known member (e.g. a force-deleted agent whose member row is
// gone but whose messages are soft-ref retained). Per the #192 chrome rule /
// [[id-chrome-vs-content-and-prefixed-refs]] lesson, an id-as-content must
// display as a clean tail handle, NEVER the raw `prefix:id` form — so we strip
// the user:/agent: scheme prefix. The raw ref is kept by call sites on title=
// for debugging. F1 (v2.8.1): call sites use this when they want a clean handle
// instead of the muted "(deleted)" label.
export function displayNameFallback(ref: string): string {
  return normalizeIdentityRef(ref);
}

// isResolvedName reports whether `name` came from a real member lookup. The
// resolver returns the RAW ref unchanged on a miss (the codebase-wide #192/#215
// "resolver(ref) === ref" sentinel that ParticipantsPanel / DMDetail / EntityRef
// callers rely on), so a name that equals the ref is unresolved. F1 (v2.8.1):
// MessageList + the sidebar header use this to render a muted "(deleted)"
// affordance for an unresolved/deleted sender instead of the raw prefixed ref.
export function isResolvedName(ref: string, name: string): boolean {
  if (!ref) return false;
  return name !== ref;
}

// useDisplayNameResolver returns a function that maps an identity ref (bare or
// "user:"/"agent:"-prefixed) to the member's display name, falling back to the
// RAW ref when unknown. v2.7 #160: used to render message senders + participant
// lists with human names. The raw-ref-on-miss is an intentional sentinel: call
// sites detect an unresolved ref via `resolver(ref) === ref` (#192/#215) and
// render "(deleted)" / a clean handle THEMSELVES — they must NEVER paint the
// raw return value into the UI when it equals the ref (see isResolvedName +
// displayNameFallback, and the EntityRef component).
export function useDisplayNameResolver(): (ref: string) => string {
  const members = useMembers();
  const byId = useMemo(() => {
    const m = new Map<string, string>();
    for (const mem of members.data ?? []) {
      if (mem.display_name) m.set(normalizeIdentityRef(mem.identity_id), mem.display_name);
    }
    return m;
  }, [members.data]);
  return (ref: string) => {
    if (!ref) return ref;
    // v2.10.0 [T75]: the system/scheduler sender resolves to a stable "System"
    // author (it is not a member row) — never the unresolved "(deleted)" branch.
    if (isSystemSender(ref)) return SYSTEM_DISPLAY_NAME;
    return byId.get(normalizeIdentityRef(ref)) ?? ref;
  };
}

// useCreatorLabel returns a function that maps a creator / author identity ref
// to the best HUMAN-FACING label: the member's display NAME when it resolves,
// else a clean id-handle (scheme prefix stripped — never the raw "agent:id"),
// and "—" for an empty ref. Owner ask: every page that surfaces a creator /
// author must PRIORITIZE the name over the id. This is the shared
// resolver+fallback so the plan / work-item tables all behave identically
// (mirrors PlanDetail's inline resolveName + normalizeIdentityRef fallback).
export function useCreatorLabel(): (ref: string | undefined | null) => string {
  const resolve = useDisplayNameResolver();
  return (ref) => {
    if (!ref) return '—';
    const name = resolve(ref);
    return isResolvedName(ref, name) ? name : displayNameFallback(ref);
  };
}

export function useAddMember() {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: (payload: { display_name: string; role?: string; reuse?: boolean }) =>
      membersApi.add(payload),
    onSuccess: () => qc.invalidateQueries({ queryKey: membersKey() }),
  });
}

export function useAddAgentMember() {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: (payload: AddAgentMemberPayload) => membersApi.addAgent(payload),
    // v2.9 #300: the unified create writes BOTH a member row AND an execution
    // Agent, so it must invalidate the agents list (qk.agents()) as well as the
    // members list. Without the agents invalidation the new agent never appears
    // in Agents / Home / MembersAgents / WorkerManagement / BoundAgents /
    // Environment (all read useAgents → qk.agents()) until a manual reload.
    onSuccess: () => {
      void qc.invalidateQueries({ queryKey: membersKey() });
      void qc.invalidateQueries({ queryKey: qk.agents() });
    },
  });
}

export function useChangeMemberRole() {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: ({ id, role }: { id: string; role: string }) =>
      membersApi.changeRole(id, role),
    onSuccess: () => qc.invalidateQueries({ queryKey: membersKey() }),
  });
}

export function useDisableMember() {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: ({ id, reason }: { id: string; reason?: string }) =>
      membersApi.disable(id, reason),
    onSuccess: () => qc.invalidateQueries({ queryKey: membersKey() }),
  });
}

export function useReEnableMember() {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: (id: string) => membersApi.reEnable(id),
    onSuccess: () => qc.invalidateQueries({ queryKey: membersKey() }),
  });
}

export function useDropMember() {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: (id: string) => membersApi.drop(id),
    onSuccess: () => qc.invalidateQueries({ queryKey: membersKey() }),
  });
}
