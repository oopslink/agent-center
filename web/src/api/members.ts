import { useQuery, useMutation, useQueryClient } from '@tanstack/react-query';
import { api } from './client';

export interface MemberResult {
  id: string;
  organization_id: string;
  identity_id: string;
  kind: 'user' | 'agent';
  role: 'owner' | 'admin' | 'member';
  status: 'joined' | 'disabled';
  joined_at: string;
}

export const membersApi = {
  list: () => api.get<MemberResult[]>('/members'),
  add: (payload: { display_name: string; role?: string }) =>
    api.post<MemberResult>('/members', payload),
  changeRole: (id: string, role: string) =>
    api.patch<void>(`/members/${id}/role`, { role }),
  disable: (id: string, reason?: string) =>
    api.post<void>(`/members/${id}/disable`, { reason: reason ?? '' }),
  reEnable: (id: string) => api.post<void>(`/members/${id}/reenable`),
};

export function useMembers() {
  return useQuery({
    queryKey: ['members'],
    queryFn: () => membersApi.list(),
    staleTime: 30_000,
  });
}

export function useAddMember() {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: (payload: { display_name: string; role?: string }) =>
      membersApi.add(payload),
    onSuccess: () => qc.invalidateQueries({ queryKey: ['members'] }),
  });
}

export function useChangeMemberRole() {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: ({ id, role }: { id: string; role: string }) =>
      membersApi.changeRole(id, role),
    onSuccess: () => qc.invalidateQueries({ queryKey: ['members'] }),
  });
}

export function useDisableMember() {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: ({ id, reason }: { id: string; reason?: string }) =>
      membersApi.disable(id, reason),
    onSuccess: () => qc.invalidateQueries({ queryKey: ['members'] }),
  });
}

export function useReEnableMember() {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: (id: string) => membersApi.reEnable(id),
    onSuccess: () => qc.invalidateQueries({ queryKey: ['members'] }),
  });
}
