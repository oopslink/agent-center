import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query';
import { api } from './client';
import { qk } from './queryKeys';
import { currentOrgSlug } from './client';

export interface InvitationResult {
  id: string;
  organization_id: string;
  invitee_user_id: string;
  invitee_display_name?: string;
  role: 'owner' | 'admin' | 'member';
  invited_by_identity_id: string;
  invited_by_display_name?: string;
  status: 'pending' | 'accepted' | 'cancelled' | 'expired';
  token: string;
  created_at: string;
  expires_at: string;
  accepted_by_identity_id?: string;
  accepted_at?: string;
}

export const invitationsApi = {
  list: () => api.get<InvitationResult[]>('/invitations'),
  create: (payload: { invitee_user_id: string; role: string }) =>
    api.post<InvitationResult>('/invitations', payload),
  cancel: (id: string) => api.post<InvitationResult>(`/invitations/${id}/cancel`),
  delete: (id: string) => api.del<void>(`/invitations/${id}`),
  accept: (token: string) =>
    api.post<InvitationResult>(`/invitations/${encodeURIComponent(token)}/accept`),
};

export function invitationAcceptUrl(token: string): string {
  const slug = currentOrgSlug();
  const path = slug
    ? `/organizations/${encodeURIComponent(slug)}/invitations/${encodeURIComponent(token)}/accept`
    : `/invitations/${encodeURIComponent(token)}/accept`;
  return `${window.location.origin}${path}`;
}

export function useInvitations() {
  return useQuery({
    queryKey: qk.invitations(),
    queryFn: () => invitationsApi.list(),
    staleTime: 30_000,
  });
}

export function useCreateInvitation() {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: (payload: { invitee_user_id: string; role: string }) =>
      invitationsApi.create(payload),
    onSuccess: () => qc.invalidateQueries({ queryKey: qk.invitations() }),
  });
}

export function useCancelInvitation() {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: (id: string) => invitationsApi.cancel(id),
    onSuccess: () => qc.invalidateQueries({ queryKey: qk.invitations() }),
  });
}

export function useDeleteInvitation() {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: (id: string) => invitationsApi.delete(id),
    onSuccess: () => qc.invalidateQueries({ queryKey: qk.invitations() }),
  });
}
