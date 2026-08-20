import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query';
import { api } from './client';
import { qk } from './queryKeys';

export type AccessSubjectKind = 'human' | 'agent' | 'worker' | 'system';
export type AccessResourceKind =
  | 'org'
  | 'project'
  | 'team'
  | 'task'
  | 'issue'
  | 'plan'
  | 'conversation'
  | 'file'
  | 'agent'
  | 'worker'
  | 'admin_token';
export type AccessSource =
  | 'org_role'
  | 'project_member'
  | 'team_member'
  | 'team_memory_policy'
  | 'conversation_participant'
  | 'file_scope'
  | 'admin_token_scope'
  | 'worker_owner'
  | 'agent_worker_binding'
  | 'custom_role'
  | 'system';
export type AccessStatus = 'allowed' | 'denied' | 'unauthorized' | 'not_applicable';
export type AccessRisk = 'low' | 'medium' | 'high';
export type AccessGrantStatus = 'active' | 'expires_soon' | 'expired' | 'revoked';
export type AccessBatchStepStatus = 'pending' | 'ready' | 'current';

export interface AccessResourceScope {
  kind: AccessResourceKind;
  id: string;
  org_id?: string;
  project_id?: string;
  label?: string;
}

export interface AccessSubject {
  ref: string;
  kind: AccessSubjectKind;
  name: string;
  role?: string;
  status?: 'joined' | 'disabled' | 'left' | 'unavailable';
  team_names?: string[];
}

export interface AccessPermissionDefinition {
  key: string;
  label: string;
  description: string;
  resource_kinds: AccessResourceKind[];
  actions: string[];
  risk: AccessRisk;
  high_risk?: boolean;
  category: 'access';
  legacy_sources: AccessSource[];
}

export interface AccessRole {
  id: string;
  name: string;
  scope_kind: AccessResourceKind;
  description: string;
  permissions: string[];
  editable: boolean;
  source: AccessSource;
  high_risk?: boolean;
}

export interface RAMRole {
  id: string;
  name: string;
  version: number;
  kind: 'system' | 'custom';
  description: string;
  permissions: string[];
  risk: AccessRisk;
  revoked_at?: string | null;
  created_at?: string;
}

export interface RAMRoleDetail {
  id: string;
  name: string;
  description: string;
  kind: 'system' | 'custom';
  revoked_at?: string | null;
  latest: RAMRole;
  versions: RAMRole[];
}

export interface RAMRoleWriteRequest {
  name?: string;
  description?: string;
  permissions: string[];
  expected_latest_version?: number;
}

export interface AccessDecision {
  allowed: boolean;
  subject_ref: string;
  permission: string;
  resource: AccessResourceScope;
  source: AccessSource;
  reason: string;
  evidence_ref: string;
  status?: AccessStatus;
  expires_at?: string | null;
  grant_id?: string;
  risk?: AccessRisk;
}

export interface AccessGrant {
  id: string;
  subject_ref: string;
  subject_name: string;
  permission: string;
  resource: AccessResourceScope;
  source: AccessSource;
  status: AccessGrantStatus;
  starts_at?: string | null;
  expires_at?: string | null;
  created_by: string;
  created_at: string;
  revoked_at?: string | null;
  risk: AccessRisk;
}

export interface AccessOverview {
  generated_at: string;
  subjects: AccessSubject[];
  roles: AccessRole[];
  catalog: AccessPermissionDefinition[];
  decisions: AccessDecision[];
  grants: AccessGrant[];
  summary: {
    allowed: number;
    high_risk: number;
    expiring: number;
    denied: number;
    not_applicable: number;
  };
}

export interface AccessFilters {
  q?: string;
  subject_kind?: AccessSubjectKind | 'all';
  resource_kind?: AccessResourceKind | 'all';
  risk?: AccessRisk | 'all';
  status?: AccessStatus | 'all';
}

export interface AccessBatchRequest {
  subject_refs: string[];
  permission_keys: string[];
  resources: AccessResourceScope[];
  expires_at?: string | null;
  reason: string;
}

export interface AccessBatchItem {
  id: string;
  subject_ref: string;
  subject_name: string;
  permission: string;
  resource: AccessResourceScope;
  status: AccessStatus;
  risk: AccessRisk;
  high_risk: boolean;
  reason: string;
  evidence_ref?: string;
  grant_id?: string;
}

export interface AccessBatchPreview {
  request_id: string;
  expires_at?: string | null;
  items: AccessBatchItem[];
  summary: {
    total: number;
    grantable: number;
    high_risk: number;
    unauthorized: number;
    not_applicable: number;
  };
}

export interface AccessBatchResult {
  operation_id: string;
  applied_at: string;
  items: AccessBatchItem[];
  summary: {
    total: number;
    succeeded: number;
    failed: number;
    unauthorized: number;
    not_applicable: number;
    partial_failure: boolean;
  };
}

export interface AccessBulkRevokeRequest {
  grant_ids: string[];
  reason: string;
  message?: string;
  preview_id?: string;
  token?: string;
  idempotency_key?: string;
}

export interface AccessRoleUpdateRequest {
  role_id: string;
  permissions: string[];
  reason: string;
}

function qs(filters?: AccessFilters): string {
  const params = new URLSearchParams();
  params.set('view', 'access');
  if (filters) {
    for (const [key, value] of Object.entries(filters)) {
      if (value && value !== 'all') params.set(key, value);
    }
  }
  const encoded = params.toString();
  return encoded ? `?${encoded}` : '';
}

export const accessApi = {
  overview: (filters?: AccessFilters) =>
    api.get<AccessOverview>(`/access/overview${qs(filters)}`),
  ramRoles: () =>
    api.get<{ roles: RAMRole[] }>('/access/ram-roles'),
  ramRole: (id: string) =>
    api.get<RAMRoleDetail>(`/access/ram-roles/${encodeURIComponent(id)}`),
  createRAMRole: (payload: RAMRoleWriteRequest) =>
    api.post<RAMRoleDetail>('/access/ram-roles', payload),
  createRAMRoleVersion: (id: string, payload: RAMRoleWriteRequest) =>
    api.post<RAMRoleDetail>(`/access/ram-roles/${encodeURIComponent(id)}/versions`, payload),
  revokeRAMRole: (id: string, payload: { expected_latest_version?: number; reason?: string }) =>
    api.post<void>(`/access/ram-roles/${encodeURIComponent(id)}/revoke`, payload),
  previewBatch: (payload: AccessBatchRequest) =>
    api.post<AccessBatchPreview>('/access/batch/preview', payload),
  applyBatch: (payload: AccessBatchRequest & { preview_request_id?: string }) =>
    api.post<AccessBatchResult>('/access/batch/apply', payload),
  previewRevoke: (payload: AccessBulkRevokeRequest) =>
    api.post<AccessBatchPreview & { preview_id: string; token: string }>('/access/grants/revoke/preview', payload),
  confirmRevoke: (payload: AccessBulkRevokeRequest) =>
    api.post<AccessBatchResult>('/access/grants/revoke/confirm', payload),
  updateRole: (payload: AccessRoleUpdateRequest) =>
    api.patch<AccessRole>(`/access/roles/${encodeURIComponent(payload.role_id)}`, {
      permissions: payload.permissions,
      reason: payload.reason,
    }),
};

export function useRAMRoles() {
  return useQuery({
    queryKey: qk.ramRoles(),
    queryFn: () => accessApi.ramRoles(),
    staleTime: 60_000,
  });
}

export function useRAMRole(id: string | null) {
  return useQuery({
    queryKey: id ? qk.ramRole(id) : qk.ramRole(''),
    queryFn: () => accessApi.ramRole(id ?? ''),
    enabled: Boolean(id),
    staleTime: 10_000,
  });
}

export function useRAMRoleCreate() {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: (payload: RAMRoleWriteRequest) => accessApi.createRAMRole(payload),
    onSuccess: (detail) => {
      void qc.invalidateQueries({ queryKey: qk.ramRoles() });
      void qc.setQueryData(qk.ramRole(detail.id), detail);
    },
  });
}

export function useRAMRoleNewVersion() {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: ({ id, payload }: { id: string; payload: RAMRoleWriteRequest }) =>
      accessApi.createRAMRoleVersion(id, payload),
    onSuccess: (detail) => {
      void qc.invalidateQueries({ queryKey: qk.ramRoles() });
      void qc.setQueryData(qk.ramRole(detail.id), detail);
    },
  });
}

export function useRAMRoleRevoke() {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: ({ id, expected_latest_version, reason }: { id: string; expected_latest_version?: number; reason?: string }) =>
      accessApi.revokeRAMRole(id, { expected_latest_version, reason }),
    onSuccess: () => {
      void qc.invalidateQueries({ queryKey: qk.ramRoles() });
    },
  });
}

export function useAccessOverview(filters?: AccessFilters, enabled = true) {
  return useQuery({
    queryKey: qk.accessOverview(filters ?? null),
    queryFn: () => accessApi.overview(filters),
    enabled,
    staleTime: 10_000,
  });
}

export function useAccessBatchPreview() {
  return useMutation({
    mutationFn: (payload: AccessBatchRequest) => accessApi.previewBatch(payload),
  });
}

export function useAccessBatchApply() {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: (payload: AccessBatchRequest & { preview_request_id?: string }) =>
      accessApi.applyBatch(payload),
    onSuccess: () => {
      void qc.invalidateQueries({ queryKey: qk.accessOverview() });
    },
  });
}

export function useAccessBulkRevoke() {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: (payload: AccessBulkRevokeRequest) => accessApi.confirmRevoke(payload),
    onSuccess: () => {
      void qc.invalidateQueries({ queryKey: qk.accessOverview() });
    },
  });
}

export function useAccessRevokePreview() {
  return useMutation({
    mutationFn: (payload: AccessBulkRevokeRequest) => accessApi.previewRevoke(payload),
  });
}

export function useAccessRoleUpdate() {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: (payload: AccessRoleUpdateRequest) => accessApi.updateRole(payload),
    onSuccess: () => {
      void qc.invalidateQueries({ queryKey: qk.accessOverview() });
    },
  });
}
