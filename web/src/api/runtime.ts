import { useQuery } from '@tanstack/react-query';
import { api } from './client';
import { qk } from './queryKeys';

// Agent runtime browser (T583, issue-921db054 / I5). Read-ONLY visualization of an
// agent's on-worker runtime files (memory/ git repo, workspace/, events, configs).
// All paths are org-scoped — the api client injects /api/orgs/{slug}. Data is
// fetched live via the Center→Worker control-channel; when the worker is offline
// or times out, every endpoint returns { unavailable: true } and the UI degrades.

export interface RuntimeEntry {
  name: string;
  path: string;
  type: 'file' | 'directory';
  size: number;
  mtime: string;
  // Sensitive (plaintext credentials) or special (sock/lock) — the UI marks it
  // and read() returns redacted/binary with content: null.
  sensitive?: boolean;
}

export interface RuntimeListResp {
  path: string;
  type: 'directory';
  entries: RuntimeEntry[];
  truncated: boolean;
}

export interface RuntimeReadResp {
  type: 'file';
  size: number;
  mtime: string;
  content_type: string;
  binary: boolean;
  // redacted = a sensitive file (plaintext credentials); content is null and never
  // leaves the worker. binary likewise carries content: null (metadata-only).
  redacted?: boolean;
  // image = a previewable image; content holds base64 bytes (encoding: 'base64') the
  // FE renders inline via a data URL. An image over the worker cap falls back to
  // binary:true / content:null.
  image?: boolean;
  encoding?: string;
  truncated: boolean;
  content: string | null;
}

export interface RuntimeCommit {
  sha: string;
  message: string;
  author: string;
  date: string;
}

export interface RuntimeGitLogResp {
  commits: RuntimeCommit[];
  truncated: boolean;
}

export interface RuntimeGitDiffResp {
  sha: string;
  diff: string;
  truncated: boolean;
}

// Worker offline / timeout sentinel — any endpoint may return this instead of data.
export interface RuntimeUnavailable {
  unavailable: true;
  reason?: string;
}

export function isUnavailable(d: unknown): d is RuntimeUnavailable {
  return typeof d === 'object' && d !== null && (d as { unavailable?: unknown }).unavailable === true;
}

const base = (agentId: string) => `/agents/${encodeURIComponent(agentId)}/runtime`;

export function useRuntimeList(agentId: string, path: string, enabled = true) {
  return useQuery({
    queryKey: qk.runtimeList(agentId, path),
    queryFn: () =>
      api.get<RuntimeListResp | RuntimeUnavailable>(`${base(agentId)}/list?path=${encodeURIComponent(path)}`),
    enabled: !!agentId && enabled,
    retry: false,
  });
}

export function useRuntimeRead(agentId: string, path: string | null, enabled = true) {
  return useQuery({
    queryKey: qk.runtimeRead(agentId, path ?? ''),
    queryFn: () =>
      api.get<RuntimeReadResp | RuntimeUnavailable>(`${base(agentId)}/read?path=${encodeURIComponent(path ?? '')}`),
    enabled: !!agentId && !!path && enabled,
    retry: false,
  });
}

export function useRuntimeGitLog(agentId: string, path: string, limit = 30, enabled = true) {
  return useQuery({
    queryKey: qk.runtimeGitLog(agentId, path),
    queryFn: () =>
      api.get<RuntimeGitLogResp | RuntimeUnavailable>(
        `${base(agentId)}/gitlog?path=${encodeURIComponent(path)}&limit=${limit}`,
      ),
    enabled: !!agentId && enabled,
    retry: false,
  });
}

export function useRuntimeGitDiff(agentId: string, path: string, ref: string, enabled = true) {
  return useQuery({
    queryKey: qk.runtimeGitDiff(agentId, path, ref),
    queryFn: () =>
      api.get<RuntimeGitDiffResp | RuntimeUnavailable>(
        `${base(agentId)}/gitdiff?path=${encodeURIComponent(path)}&ref=${encodeURIComponent(ref)}`,
      ),
    enabled: !!agentId && !!ref && enabled,
    retry: false,
  });
}
