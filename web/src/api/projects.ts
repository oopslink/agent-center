import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query';
import { api, withOrgSlug } from './client';
import { qk } from './queryKeys';
import type { Project } from './types';

// Projects (v2.1-A picker + v2.3-4 list/detail surface). v2.5.3 (#58)
// adds the CRUD mutations + worker-mapping affordances so the Web
// Console no longer needs the CLI for managing projects.

// Re-export the canonical Project type so existing call-sites that
// previously imported it from `@/api/projects` keep working.
export type { Project };

// CreateProjectInput / UpdateProjectInput mirror the v2.5.5 backend
// shape (handlers.go createProjectReq / updateProjectReq). id is
// server-generated, tags is free-text (UI surfaces a small builtin
// suggestion pool).
export interface CreateProjectInput {
  name: string;
  description?: string;
  tags?: string[];
}

export interface UpdateProjectInput {
  version: number;
  name?: string;
  description?: string;
  tags?: string[];
}

export interface WorkerMapping {
  id: string;
  worker_id: string;
  project_id: string;
  path: string;
  status: string;
  added_at: string;
}

export function useProjects() {
  return useQuery({
    queryKey: qk.projects(),
    queryFn: () => api.get<Project[]>('/projects'),
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

export interface DeleteProjectError {
  error: string;
  message: string;
  mapping_count?: number;
  task_count?: number;
  issue_count?: number;
}

// useDeleteProject returns the same hook shape as the others. On
// 409 the server response body carries mapping_count + task_count +
// issue_count so the SPA can render a "really continue?" confirm.
// Pass `force=true` to cascade-delete after the operator confirms.
export function useDeleteProject(id: string) {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: async (opts?: { force?: boolean }) => {
      const force = opts?.force ?? false;
      const resp = await fetch(
        withOrgSlug(`/api/projects/${encodeURIComponent(id)}${force ? '?force=true' : ''}`),
        { method: 'DELETE' },
      );
      if (resp.status === 204) return null;
      const body = (await resp.json().catch(() => ({}))) as DeleteProjectError;
      throw body;
    },
    onSuccess: () => {
      void qc.invalidateQueries({ queryKey: qk.projects() });
    },
  });
}

export function useProjectMappings(id: string | undefined) {
  return useQuery({
    queryKey: ['project-mappings', id ?? ''],
    queryFn: () => api.get<WorkerMapping[]>(`/projects/${id}/workers`),
    enabled: !!id,
  });
}

export function useCreateProjectMapping(id: string) {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: (input: { worker_id: string; path: string }) =>
      api.post<WorkerMapping>(`/projects/${id}/workers`, input),
    onSuccess: () => {
      void qc.invalidateQueries({ queryKey: ['project-mappings', id] });
    },
  });
}

export function useDeleteProjectMapping(projectID: string) {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: async (mappingID: string) => {
      const resp = await fetch(
        withOrgSlug(`/api/projects/${encodeURIComponent(projectID)}/workers/${encodeURIComponent(mappingID)}`),
        { method: 'DELETE' },
      );
      if (!resp.ok && resp.status !== 204) {
        throw new Error(`HTTP ${resp.status}`);
      }
    },
    onSuccess: () => {
      void qc.invalidateQueries({ queryKey: ['project-mappings', projectID] });
    },
  });
}
