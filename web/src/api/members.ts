import { useMemo } from 'react';
import { useQuery, useMutation, useQueryClient } from '@tanstack/react-query';
import { api } from './client';
import { currentOrgScope } from './queryKeys';

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

// identityRefOf builds the prefixed identity ref ("agent:<id>" / "user:<id>")
// from a member-like value — the inverse of normalizeIdentityRef. The id is
// normalized first so an already-prefixed identity_id is not double-prefixed.
// Consolidates the per-component `refOf` duplication (DMStartModal /
// MemberInviteModal / ProjectMemberAddModal / AppLayout / MentionText). v2.9 #254.
export function identityRefOf(m: { kind: 'user' | 'agent'; identity_id: string }): string {
  return `${m.kind === 'agent' ? 'agent:' : 'user:'}${normalizeIdentityRef(m.identity_id)}`;
}

// refKind reads the kind from a prefixed identity ref ("agent:" → 'agent',
// otherwise 'user'). Matches MemberResult.kind. v2.9 #254. NOTE: UI sites that
// map to the 'agent' | 'human' avatar-kind contract (ParticipantsPanel /
// MessageList) intentionally keep their own mapping — different return type.
export function refKind(ref: string): 'user' | 'agent' {
  return ref.startsWith('agent:') ? 'agent' : 'user';
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
  return (ref: string) => (ref ? byId.get(normalizeIdentityRef(ref)) ?? ref : ref);
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
    onSuccess: () => qc.invalidateQueries({ queryKey: membersKey() }),
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
