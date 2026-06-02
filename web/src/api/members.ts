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
}

// normalizeIdentityRef strips the "user:"/"agent:" prefix so a prefixed ref
// (message sender_identity_id / conversation participant, e.g. "user:user-ab12")
// keys to the same value as a bare member identity_id ("user-ab12"). v2.7 #160.
export function normalizeIdentityRef(ref: string): string {
  if (ref.startsWith('user:')) return ref.slice('user:'.length);
  if (ref.startsWith('agent:')) return ref.slice('agent:'.length);
  return ref;
}

export interface AddUserResult extends MemberResult {
  temp_passcode?: string;
  display_name?: string;
}

export const membersApi = {
  list: () => api.get<MemberResult[]>('/members'),
  add: (payload: { display_name: string; role?: string; reuse?: boolean }) =>
    api.post<AddUserResult>('/members', payload),
  addAgent: (payload: { display_name: string; description?: string; role?: string }) =>
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

// useDisplayNameResolver returns a function that maps an identity ref (bare or
// "user:"/"agent:"-prefixed) to the member's display name, falling back to the
// raw ref when unknown. v2.7 #160: used to render message senders + participant
// lists with human names instead of "user:user-ab12".
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
    mutationFn: (payload: { display_name: string; description?: string; role?: string }) =>
      membersApi.addAgent(payload),
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
