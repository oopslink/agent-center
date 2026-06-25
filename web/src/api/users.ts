import { useQuery } from '@tanstack/react-query';
import { api } from './client';
import { currentOrgScope } from './queryKeys';

// v2.7.1 #193: a user's org membership with their role there.
// T478 #1: org_name + org_slug come from the server (authoritative) so the row
// can show a stable "name + id" for every org — including orgs the viewer is not
// a member of (where the old useOrgs lookup fell back to the raw id).
export interface UserOrgMembership {
  org_id: string;
  org_name?: string;
  org_slug?: string;
  role: string;
}

// UserDetailResult — GET /api/users/{user-id} (user-id = member-id `user-<8hex>`).
export interface UserDetailResult {
  user_id: string;
  display_name: string;
  email?: string;
  created_at: string;
  last_session_at?: string;
  orgs: UserOrgMembership[];
}

// useUser fetches a single user's profile by member-id (org-scoped cache key).
export function useUser(userId: string | undefined) {
  return useQuery({
    queryKey: ['org', currentOrgScope(), 'user', userId ?? ''] as const,
    queryFn: () => api.get<UserDetailResult>(`/users/${encodeURIComponent(userId as string)}`),
    enabled: !!userId,
  });
}
