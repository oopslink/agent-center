import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query';
import { api } from './client';
import { qk } from './queryKeys';

export type DecisionSource =
  | 'org_role'
  | 'project_member'
  | 'team_member'
  | 'team_memory_policy'
  | 'conversation_participant'
  | 'file_scope'
  | 'admin_token_scope'
  | 'worker_owner'
  | 'agent_worker_binding'
  | 'system'
  | 'custom_role'
  | string;

export interface FileRef {
  scope: string;
  scope_id: string;
}

export interface ResourceScope {
  kind: string;
  id?: string;
  org_id?: string;
  project_id?: string;
  uri?: string;
  owner_ref?: string;
  identity_member_id?: string;
  refs?: FileRef[];
}

export interface AccessDecision {
  allowed: boolean;
  subject_ref: string;
  permission: string;
  resource: ResourceScope;
  source?: DecisionSource;
  reason: string;
  evidence_ref?: string;
}

export interface EffectivePermission {
  key: string;
  source: DecisionSource;
  evidence_ref: string;
  delegatable?: boolean;
  role_id?: string;
  assignment_id?: string;
}

export interface EffectivePermissions {
  subject_ref: string;
  resource: ResourceScope;
  permissions: EffectivePermission[];
  complete?: boolean;
  truncated?: boolean;
  has_more?: boolean;
  warnings?: string[];
}

export interface ExplainResult {
  decision: AccessDecision;
  effective: EffectivePermission[];
  denied_by?: string[];
  resolved_org?: string;
}

export interface PermissionDefinition {
  key: string;
  category: string;
  resource_kinds: string[];
  actions: string[];
  legacy_sources: string[];
}

export interface PermissionDefinitionsResponse {
  definitions: PermissionDefinition[];
}

export interface PermissionAuditEvent {
  id: string;
  event_type: string;
  actor_ref: string;
  subject_ref?: string;
  permission_key?: string;
  resource_kind?: string;
  resource_id?: string;
  role_id?: string;
  assignment_id?: string;
  request_id?: string;
  payload?: Record<string, unknown>;
  created_at: string;
}

export interface PermissionAuditResponse {
  events: PermissionAuditEvent[];
  complete?: boolean;
  truncated?: boolean;
  has_more?: boolean;
  limit?: number;
  warnings?: string[];
}

interface RoleInput {
  id?: string;
  name?: string;
  description?: string;
}

interface RolePermissionInput {
  permission_key: string;
  resource_kind: string;
  delegatable?: boolean;
}

interface AssignmentInput {
  id?: string;
  subject_ref: string;
  role_id: string;
  resource: ResourceScope;
}

interface RevokeInput {
  assignment_id?: string;
  subject_ref?: string;
  role_id?: string;
  resource?: ResourceScope;
  reason?: string;
}

interface BatchOperation {
  id?: string;
  type: string;
  role?: RoleInput;
  permissions?: RolePermissionInput[];
  assignment?: AssignmentInput;
  revoke?: RevokeInput;
}

interface BatchRequest {
  idempotency_key?: string;
  operations: BatchOperation[];
}

export interface OperationResult {
  id?: string;
  type: string;
  status: string;
  role_id?: string;
  assignment_id?: string;
  reason?: string;
}

export interface BatchResult {
  idempotency_key?: string;
  replayed?: boolean;
  preview: boolean;
  operations: OperationResult[];
}

export interface DirectGrantInput {
  subjectRef: string;
  permissionKey: string;
  resource: ResourceScope;
}

export interface DirectRevokeInput {
  assignmentId: string;
  reason?: string;
}

export function permissionResourceKey(resource: ResourceScope): string {
  return [
    resource.kind,
    resource.id ?? '',
    resource.org_id ?? '',
    resource.project_id ?? '',
    resource.uri ?? '',
    resource.owner_ref ?? '',
  ].join('|');
}

function effectivePath(subjectRef: string, resource: ResourceScope): string {
  const qs = new URLSearchParams();
  qs.set('subject_ref', subjectRef);
  qs.set('resource_kind', resource.kind);
  if (resource.id) qs.set('resource_id', resource.id);
  if (resource.uri) qs.set('uri', resource.uri);
  return `/permissions/effective?${qs.toString()}`;
}

function auditPath(subjectRef: string): string {
  const qs = new URLSearchParams();
  qs.set('subject_ref', subjectRef);
  qs.set('limit', '50');
  return `/permissions/audit?${qs.toString()}`;
}

function idempotencyKey(prefix: string): string {
  return `${prefix}-${Date.now()}-${Math.random().toString(36).slice(2)}`;
}

function directRoleID(resourceKind: string, permissionKey: string): string {
  const key = `${resourceKind}-${permissionKey}`
    .toLowerCase()
    .replace(/[^a-z0-9]+/g, '-')
    .replace(/^-+|-+$/g, '');
  return `role-direct-${key}`;
}

export function usePermissionDefinitions() {
  return useQuery({
    queryKey: qk.permissionDefinitions(),
    queryFn: () => api.get<PermissionDefinitionsResponse>('/permissions/definitions'),
  });
}

export function useEffectivePermissions(subjectRef: string, resource: ResourceScope | null) {
  const key = resource ? permissionResourceKey(resource) : '';
  return useQuery({
    queryKey: qk.permissionEffective(subjectRef, key),
    queryFn: () => api.get<EffectivePermissions>(effectivePath(subjectRef, resource as ResourceScope)),
    enabled: !!subjectRef && !!resource?.kind && (!!resource.id || !!resource.uri),
  });
}

export function usePermissionExplain(
  subjectRef: string,
  permissionKey: string,
  resource: ResourceScope | null,
  enabled: boolean,
) {
  const key = resource ? permissionResourceKey(resource) : '';
  return useQuery({
    queryKey: qk.permissionExplain(subjectRef, permissionKey, key),
    queryFn: () =>
      api.post<ExplainResult>('/permissions/explain', {
        subject_ref: subjectRef,
        permission: permissionKey,
        resource,
      }),
    enabled: enabled && !!subjectRef && !!permissionKey && !!resource?.kind && (!!resource.id || !!resource.uri),
  });
}

export function usePermissionAudit(subjectRef: string, enabled = true) {
  return useQuery({
    queryKey: qk.permissionAudit(subjectRef),
    queryFn: () => api.get<PermissionAuditResponse>(auditPath(subjectRef)),
    enabled: enabled && !!subjectRef,
  });
}

export function useGrantDirectPermission() {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: ({ subjectRef, permissionKey, resource }: DirectGrantInput) => {
      const roleID = directRoleID(resource.kind, permissionKey);
      const body: BatchRequest = {
        idempotency_key: idempotencyKey('grant-direct'),
        operations: [
          {
            id: 'role',
            type: 'upsert_role',
            role: {
              id: roleID,
              name: `Direct ${permissionKey}`,
              description: `Direct grant role for ${permissionKey}`,
            },
          },
          {
            id: 'permissions',
            type: 'set_role_permissions',
            role: { id: roleID },
            permissions: [{ permission_key: permissionKey, resource_kind: resource.kind, delegatable: false }],
          },
          {
            id: 'assignment',
            type: 'assign_role',
            assignment: { subject_ref: subjectRef, role_id: roleID, resource },
          },
        ],
      };
      return api.post<BatchResult>('/permissions/batch/apply', body);
    },
    onSuccess: () => {
      void qc.invalidateQueries({ queryKey: qk.permissions() });
    },
  });
}

export function useRevokeDirectPermission() {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: ({ assignmentId, reason }: DirectRevokeInput) => {
      const body: BatchRequest = {
        idempotency_key: idempotencyKey('revoke-direct'),
        operations: [
          {
            id: 'revoke',
            type: 'revoke_assignment',
            revoke: { assignment_id: assignmentId, reason: reason ?? 'revoked from detail access tab' },
          },
        ],
      };
      return api.post<BatchResult>('/permissions/batch/revoke', body);
    },
    onSuccess: () => {
      void qc.invalidateQueries({ queryKey: qk.permissions() });
    },
  });
}
