import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query';
import { api, withOrgSlug } from './client';
import { qk } from './queryKeys';
import type { CodeRepo, Project, ProjectMember } from './types';

// Projects (v2.7 ProjectManager BC). Flat, org-scoped. Responses are
// now WRAPPED objects ({ projects: [...] }), not bare arrays.

// Re-export the canonical Project type so existing call-sites that
// previously imported it from `@/api/projects` keep working.
export type { Project };

// CreateProjectInput / UpdateProjectInput mirror the v2.7 backend shape.
// `tags` is gone; PATCH carries no version field in the request body.
export interface CreateProjectInput {
  name: string;
  description?: string;
}

export interface UpdateProjectInput {
  name?: string;
  description?: string;
}

export function useProjects() {
  return useQuery({
    queryKey: qk.projects(),
    queryFn: async () => {
      const resp = await api.get<{ projects: Project[] }>('/projects');
      return resp.projects;
    },
    staleTime: 5_000,
  });
}

export function useProject(id: string | undefined) {
  return useQuery({
    queryKey: qk.project(id ?? ''),
    queryFn: () => api.get<Project>(`/projects/${id}`),
    enabled: !!id,
  });
}

export function useCreateProject() {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: (input: CreateProjectInput) =>
      api.post<Project>('/projects', input),
    onSuccess: () => {
      void qc.invalidateQueries({ queryKey: qk.projects() });
    },
  });
}

export function useUpdateProject(id: string) {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: (input: UpdateProjectInput) =>
      api.patch<Project>(`/projects/${id}`, input),
    onSuccess: () => {
      void qc.invalidateQueries({ queryKey: qk.projects() });
      void qc.invalidateQueries({ queryKey: qk.project(id) });
    },
  });
}

// useDeleteProject — v2.7 ARCHIVES the project (soft); there is no
// force/cascade and no 409 mapping/count conflict body. Returns
// { ok:true, status:"archived" }.
export function useDeleteProject(id: string) {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: async () => {
      const resp = await fetch(
        withOrgSlug(`/api/projects/${encodeURIComponent(id)}`),
        { method: 'DELETE' },
      );
      if (!resp.ok && resp.status !== 204) {
        const body = (await resp.json().catch(() => ({}))) as {
          message?: string;
        };
        throw new Error(body?.message ?? `HTTP ${resp.status}`);
      }
    },
    onSuccess: () => {
      void qc.invalidateQueries({ queryKey: qk.projects() });
    },
  });
}

// useProjectCodeRepos — read-only project code repos (v2.7).
export function useProjectCodeRepos(id: string | undefined) {
  return useQuery({
    queryKey: qk.codeReposByProject(id ?? ''),
    queryFn: async () => {
      const resp = await api.get<{ code_repos: CodeRepo[] }>(
        `/projects/${id}/code-repos`,
      );
      return resp.code_repos;
    },
    enabled: !!id,
  });
}

// useProjectMembers — read-only project membership (v2.7 ProjectManager BC).
export function useProjectMembers(id: string | undefined) {
  return useQuery({
    queryKey: qk.membersByProject(id ?? ''),
    queryFn: async () => {
      const resp = await api.get<{ members: ProjectMember[] }>(
        `/projects/${id}/members`,
      );
      return resp.members;
    },
    enabled: !!id,
  });
}

// useAddProjectMember (v2.7 #207) — POST /api/projects/{id}/members. The
// identity ref is "<kind>:<id>" (user:/agent:); the backend gates the actor to
// project members. Invalidates the project's member list on success.
export function useAddProjectMember(projectId: string) {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: ({ identityId, role }: { identityId: string; role?: string }) =>
      api.post<{ ok: boolean }>(`/projects/${projectId}/members`, {
        identity_id: identityId,
        role: role ?? 'member',
      }),
    onSuccess: () => {
      void qc.invalidateQueries({ queryKey: qk.membersByProject(projectId) });
    },
  });
}

// useRemoveProjectMember (v2.7 #207) — DELETE /api/projects/{id}/members/{identity_id}.
// Owner-only on the backend; rejects removing the last owner (409
// cannot_remove_owner) and unknown members (404 not_member).
export function useRemoveProjectMember(projectId: string) {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: (identityId: string) =>
      api.del<void>(`/projects/${projectId}/members/${encodeURIComponent(identityId)}`),
    onSuccess: () => {
      void qc.invalidateQueries({ queryKey: qk.membersByProject(projectId) });
    },
  });
}
