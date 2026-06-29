import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query';
import { api } from './client';
import { qk } from './queryKeys';
import type { CodeRepo, RepoBranch, RepoCommit, WorkspaceRepo } from './types';

// Workspace code-repo registry (T575, issue-f980c8de). Repos are workspace/org
// top-level entities; projects only REFERENCE them. All paths are org-scoped —
// the api client injects /api/orgs/{slug} via withOrgSlug. Credentials are
// write-only (configured here, never returned).

// ── Workspace Repo CRUD ──────────────────────────────────────────────────────
export function useWorkspaceRepos() {
  return useQuery({
    queryKey: qk.workspaceRepos(),
    queryFn: async () => {
      const resp = await api.get<{ repos: WorkspaceRepo[] }>('/code-repos');
      return resp.repos;
    },
  });
}

export interface CreateWorkspaceRepoInput {
  label: string;
  description?: string;
  url: string;
  provider: string;
  default_branch?: string;
  credential?: string;
}

// credential semantics on update mirror the backend *string: undefined =
// unchanged, "" = clear, non-empty = replace.
export interface UpdateWorkspaceRepoInput {
  label?: string;
  description?: string;
  url?: string;
  provider?: string;
  default_branch?: string;
  credential?: string;
}

export function useCreateWorkspaceRepo() {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: (input: CreateWorkspaceRepoInput) =>
      api.post<WorkspaceRepo>('/code-repos', input),
    onSuccess: () => void qc.invalidateQueries({ queryKey: qk.workspaceRepos() }),
  });
}

export function useUpdateWorkspaceRepo(id: string) {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: (input: UpdateWorkspaceRepoInput) =>
      api.patch<WorkspaceRepo>(`/code-repos/${id}`, input),
    onSuccess: () => void qc.invalidateQueries({ queryKey: qk.workspaceRepos() }),
  });
}

export function useDeleteWorkspaceRepo() {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: (id: string) => api.del<void>(`/code-repos/${id}`),
    onSuccess: () => void qc.invalidateQueries({ queryKey: qk.workspaceRepos() }),
  });
}

// ── Project ↔ Repo references ────────────────────────────────────────────────
// The project-side referencer: add/remove a reference to a workspace Repo and
// pick the per-project primary. No url/credential here (those live on the repo).
export interface AddProjectRepoRefInput {
  repo_id: string;
  url?: string;
  label?: string;
  is_primary?: boolean;
}

export function useAddProjectRepoRef(projectId: string) {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: (input: AddProjectRepoRefInput) =>
      api.post<CodeRepo>(`/projects/${projectId}/code-repos`, input),
    onSuccess: () => void qc.invalidateQueries({ queryKey: qk.codeReposByProject(projectId) }),
  });
}

export function useRemoveProjectRepoRef(projectId: string) {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: (refId: string) =>
      api.del<void>(`/projects/${projectId}/code-repos/${refId}`),
    onSuccess: () => void qc.invalidateQueries({ queryKey: qk.codeReposByProject(projectId) }),
  });
}

export function useSetPrimaryProjectRepo(projectId: string) {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: (refId: string) =>
      api.post<CodeRepo>(`/projects/${projectId}/code-repos/${refId}/primary`),
    onSuccess: () => void qc.invalidateQueries({ queryKey: qk.codeReposByProject(projectId) }),
  });
}

// ── Remote viewing (BE-2; provisional contract aligned with PD) ──────────────
// Live, read-only: the backend reads the remote (go-github / git ls-remote),
// never clones. Endpoint shape may be adjusted to BE-2's final API.
export function useRepoBranches(repoId: string | undefined, enabled = true) {
  return useQuery({
    queryKey: qk.repoBranches(repoId ?? ''),
    queryFn: async () => {
      const resp = await api.get<{ branches: RepoBranch[]; source?: string }>(
        `/code-repos/${repoId}/branches`,
      );
      return resp;
    },
    enabled: !!repoId && enabled,
    retry: false,
  });
}

export function useRepoCommits(repoId: string | undefined, branch: string, enabled = true) {
  return useQuery({
    queryKey: qk.repoCommits(repoId ?? '', branch),
    queryFn: async () => {
      const q = branch ? `?branch=${encodeURIComponent(branch)}` : '';
      const resp = await api.get<{ commits: RepoCommit[]; branch?: string; source?: string }>(
        `/code-repos/${repoId}/commits${q}`,
      );
      return resp;
    },
    enabled: !!repoId && enabled,
    retry: false,
  });
}
