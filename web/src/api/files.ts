import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query';
import { api, withOrgSlug } from './client';
import { qk } from './queryKeys';

// v2.10.0 [T73]: task/issue-scoped file attachments. The Task/Issue detail pages
// list + upload + download files placed in the task/issue scope. The backend
// surface (project-member-gated, fail-closed) is:
//   GET  /projects/{pid}/{tasks|issues}/{id}/files                          list
//   POST /projects/{pid}/{tasks|issues}/{id}/files                          create upload
//   POST /projects/{pid}/{tasks|issues}/{id}/files/transfer/{tid}/complete  finalize
// Blob bytes go through the generic PUT /files/transfer/{tid}; download is the
// generic GET /files/{ulid} (reachability-gated → attachmentHref builds the URL).

export type ScopeKind = 'tasks' | 'issues';

export interface ScopeFile {
  uri: string;
  filename: string;
  mime_type: string;
  size: number;
  created_by: string;
  created_at: string;
}

interface CreateUploadResult {
  file_uri: string;
  transfer_uri: string;
  transfer_id: string;
}

function scopeBase(kind: ScopeKind, projectId: string, scopeId: string): string {
  return `/projects/${encodeURIComponent(projectId)}/${kind}/${encodeURIComponent(scopeId)}/files`;
}

function useScopeFiles(
  kind: ScopeKind,
  projectId: string | undefined,
  scopeId: string | undefined,
  queryKey: readonly unknown[],
) {
  return useQuery({
    queryKey,
    queryFn: async () => {
      const res = await api.get<{ files: ScopeFile[] }>(
        scopeBase(kind, projectId as string, scopeId as string),
      );
      return res.files ?? [];
    },
    enabled: !!projectId && !!scopeId,
  });
}

export function useTaskFiles(projectId: string | undefined, taskId: string | undefined) {
  return useScopeFiles('tasks', projectId, taskId, qk.taskFiles(taskId ?? ''));
}

export function useIssueFiles(projectId: string | undefined, issueId: string | undefined) {
  return useScopeFiles('issues', projectId, issueId, qk.issueFiles(issueId ?? ''));
}

// uploadScopeFile runs the three-step create → PUT bytes → complete flow against
// the task/issue scope, creating the placement reference (with the real
// filename) on complete. Mirrors uploadMessageAttachment but scope-bound.
async function uploadScopeFile(
  kind: ScopeKind,
  projectId: string,
  scopeId: string,
  file: File,
): Promise<ScopeFile> {
  const base = scopeBase(kind, projectId, scopeId);
  const contentType = file.type || 'application/octet-stream';
  const created = await api.post<CreateUploadResult>(base, {
    content_type: contentType,
    size: file.size,
  });
  // Raw PUT of the blob bytes through the generic transfer route (org-scoped).
  const putPath = `/api${withOrgSlug(`/files/transfer/${encodeURIComponent(created.transfer_id)}`)}`;
  const putResp = await fetch(putPath, {
    method: 'PUT',
    headers: { 'Content-Type': contentType },
    body: file,
  });
  if (!putResp.ok) {
    throw new Error(`upload failed: ${putResp.status}`);
  }
  await api.post(`${base}/transfer/${encodeURIComponent(created.transfer_id)}/complete`, {
    size: file.size,
    filename: file.name,
  });
  return {
    uri: created.file_uri,
    filename: file.name,
    mime_type: contentType,
    size: file.size,
    created_by: '',
    created_at: '',
  };
}

export function useUploadTaskFile(projectId: string, taskId: string) {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: (file: File) => uploadScopeFile('tasks', projectId, taskId, file),
    onSuccess: () => {
      void qc.invalidateQueries({ queryKey: qk.taskFiles(taskId) });
    },
  });
}

export function useUploadIssueFile(projectId: string, issueId: string) {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: (file: File) => uploadScopeFile('issues', projectId, issueId, file),
    onSuccess: () => {
      void qc.invalidateQueries({ queryKey: qk.issueFiles(issueId) });
    },
  });
}
