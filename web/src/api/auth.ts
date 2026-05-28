import { useQuery, useMutation, useQueryClient } from '@tanstack/react-query';
import { api } from './client';

export interface SignupPayload {
  display_name: string;
  passcode: string;
  organization_name: string;
  organization_slug: string;
}

export interface SignupResult {
  identity_id: string;
  organization_id: string;
  display_name: string;
}

export interface SigninPayload {
  display_name: string;
  passcode: string;
}

export interface SigninResult {
  identity_id: string;
}

export interface MeResult {
  identity_id: string;
  display_name: string;
  kind: 'user' | 'agent';
}

export interface ChangePasscodePayload {
  current_passcode: string;
  new_passcode: string;
}

export const authApi = {
  signup: (payload: SignupPayload) => api.post<SignupResult>('/auth/signup', payload),
  signin: (payload: SigninPayload) => api.post<SigninResult>('/auth/signin', payload),
  signout: () => api.post<void>('/auth/signout'),
  me: () => api.get<MeResult>('/auth/me'),
  changePasscode: (payload: ChangePasscodePayload) =>
    api.patch<void>('/auth/me/passcode', payload),
};

export function useMe() {
  return useQuery({
    queryKey: ['me'],
    queryFn: () => authApi.me(),
    retry: false,
    staleTime: 5 * 60 * 1000,
  });
}

export function useSignout() {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: () => authApi.signout(),
    onSuccess: () => {
      qc.clear();
      window.location.href = '/signin';
    },
  });
}

// ===== Organization API =====

export interface OrgResult {
  id: string;
  slug: string;
  name: string;
  created_at: string;
}

export interface CreateOrgPayload {
  name: string;
  slug: string;
}

export const orgApi = {
  list: () => api.get<OrgResult[]>('/orgs'),
  create: (payload: CreateOrgPayload) => api.post<OrgResult>('/orgs', payload),
  delete: (id: string) => api.del<void>(`/orgs/${id}`),
};

export function useOrgs() {
  return useQuery({
    queryKey: ['orgs'],
    queryFn: () => orgApi.list(),
    staleTime: 30_000,
  });
}
