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
