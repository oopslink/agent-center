import { useQuery, useMutation, useQueryClient } from '@tanstack/react-query';
import { api } from './client';

export interface SignupPayload {
  display_name: string;
  // v2.7.1 #193: required for new signups (stored, not verified — no email sent).
  email: string;
  passcode: string;
  organization_name: string;
  // T237: the org slug is auto-generated server-side ("org-<hex>"); the client no
  // longer supplies one.
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
  description?: string;
  created_at: string;
  // I41 (T470): true when the org is disabled (login gate active for non-owners).
  // Only the org's owner ever sees a disabled org in their list, so the Danger
  // Zone can render the Enable toggle. Absent → enabled.
  disabled?: boolean;
}

export interface CreateOrgPayload {
  name: string;
  slug: string;
}

export const orgApi = {
  list: () => api.get<OrgResult[]>('/orgs'),
  create: (payload: CreateOrgPayload) => api.post<OrgResult>('/orgs', payload),
  update: (id: string, payload: { name?: string; slug?: string; description?: string }) =>
    api.patch<void>(`/orgs/${id}`, payload),
  delete: (id: string) => api.del<void>(`/orgs/${id}`),
  // I41 (T470): reversible org disable (owner-only) — distinct from delete.
  disable: (id: string) => api.post<void>(`/orgs/${id}/disable`),
  enable: (id: string) => api.post<void>(`/orgs/${id}/enable`),
};

// useOrgs lists the caller's organizations. OrgGuard / OrgRedirect gate the
// app's org routing on this, so a transient startup failure (GET /api/orgs can
// be CPU-starved into a transient 401 under parallel load at boot) must not be
// surfaced as a hard error before React Query has had a fair chance to recover —
// otherwise OrgGuard sees an undefined/errored result and (pre-fix) redirected
// to /signup prematurely. We allow a few retries with backoff so the transient
// case recovers (spinner → retry → success). A GENUINE 401 does not spin
// forever: the api client (client.ts request()) fires redirectUnauthenticated()
// on a real 401, navigating the whole window to /signin or /signup, which tears
// this query down. Retries here only widen the recovery window for the
// transient case; they cannot trap an authenticated-but-no-orgs user (that path
// settles to success with []) nor a genuinely-unauthenticated one (fetch-layer
// redirect).
export function useOrgs() {
  return useQuery({
    queryKey: ['orgs'],
    queryFn: () => orgApi.list(),
    staleTime: 30_000,
    retry: 3,
    retryDelay: (attempt) => Math.min(250 * 2 ** attempt, 2_000),
  });
}
