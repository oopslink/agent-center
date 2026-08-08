import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query';
import { api, withOrgSlug } from './client';
import { qk } from './queryKeys';

export interface RuntimeCLI {
  id: string;
  key: string;
  display_name: string;
  executable: string;
  version_constraint?: string;
  required_features?: string[];
  parameter_schema?: unknown;
  enabled: boolean;
  system?: boolean;
  created_at?: string;
  updated_at?: string;
}

export type RuntimeCLIInput = Pick<
  RuntimeCLI,
  'key' | 'display_name' | 'executable' | 'version_constraint' | 'required_features' | 'parameter_schema' | 'enabled'
>;

export interface RuntimeModel {
  id: string;
  key: string;
  model_key: string;
  display_name: string;
  compatible_cli_keys?: string[];
  default_parameters?: Record<string, unknown>;
  enabled: boolean;
  context_window?: number;
  input_cost_per_mtok?: number;
  output_cost_per_mtok?: number;
  tier?: string;
  created_at?: string;
  updated_at?: string;
}

export type RuntimeModelInput = Pick<
  RuntimeModel,
  | 'key'
  | 'model_key'
  | 'display_name'
  | 'compatible_cli_keys'
  | 'default_parameters'
  | 'enabled'
  | 'context_window'
  | 'input_cost_per_mtok'
  | 'output_cost_per_mtok'
  | 'tier'
>;

export interface RuntimeProfile {
  id: string;
  key: string;
  name: string;
  description?: string;
  cli_key: string;
  model_key: string;
  parameters?: Record<string, unknown>;
  enabled: boolean;
  created_at?: string;
  updated_at?: string;
}

export type RuntimeProfileInput = Pick<
  RuntimeProfile,
  'key' | 'name' | 'description' | 'cli_key' | 'model_key' | 'parameters' | 'enabled'
>;

export interface AIRuntimeCatalog {
  org_id?: string;
  revision: number;
  default_runtime_profile_id?: string;
  clis: RuntimeCLI[];
  models: RuntimeModel[];
  profiles: RuntimeProfile[];
}

export interface RuntimeExportDocument {
  schema_version: number;
  kind: 'agent-center-ai-runtime';
  exported_at: string;
  runtime: {
    default_profile_key?: string;
    clis: RuntimeCLIInput[];
    models: RuntimeModelInput[];
    profiles: RuntimeProfileInput[];
  };
}

export interface RuntimeImportItem {
  entity_type: 'cli' | 'model' | 'profile' | string;
  key: string;
  action: 'create' | 'update' | 'disable' | 'unchanged' | string;
}

export interface RuntimeImportDiagnostic {
  code: string;
  severity?: string;
  path?: string;
  entity_type?: string;
  key?: string;
  message: string;
}

export interface RuntimeImportReport {
  dry_run: boolean;
  applied: boolean;
  revision: number;
  items: RuntimeImportItem[];
  diagnostics: RuntimeImportDiagnostic[];
}

export interface RuntimeImportPreview {
  report: RuntimeImportReport;
  validation_token: string;
  expires_at: string;
  document_sha256: string;
}

interface RuntimeWrite<T> {
  expected_revision: number;
  value: T;
}

export function canManageAIRuntime(role: string | undefined): boolean {
  return role === 'owner' || role === 'admin';
}

export function useAIRuntimeCatalog() {
  return useQuery({
    queryKey: qk.aiRuntimeCatalog(),
    queryFn: () => api.get<AIRuntimeCatalog>('/ai-runtime'),
  });
}

export function useSetDefaultRuntimeProfile() {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: (input: { profileId: string; expectedRevision: number }) =>
      api.put<{ revision: number; default_runtime_profile_id: string }>('/ai-runtime/default-profile', {
        expected_revision: input.expectedRevision,
        profile_id: input.profileId,
      }),
    onSuccess: () => qc.invalidateQueries({ queryKey: qk.aiRuntimeCatalog() }),
  });
}

function invalidateRuntimeCatalog(qc: ReturnType<typeof useQueryClient>) {
  void qc.invalidateQueries({ queryKey: qk.aiRuntimeCatalog() });
  void qc.invalidateQueries({ queryKey: qk.modelCatalog() });
}

export function useCreateRuntimeCLI() {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: (input: { expectedRevision: number; value: RuntimeCLIInput }) =>
      api.post<{ revision: number; entry: RuntimeCLI }>('/ai-runtime/clis', write(input)),
    onSuccess: () => invalidateRuntimeCatalog(qc),
  });
}

export function useUpdateRuntimeCLI(id: string) {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: (input: { expectedRevision: number; value: RuntimeCLIInput }) =>
      api.patch<{ revision: number; entry: RuntimeCLI }>(`/ai-runtime/clis/${encodeURIComponent(id)}`, write(input)),
    onSuccess: () => invalidateRuntimeCatalog(qc),
  });
}

export function useCreateRuntimeModel() {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: (input: { expectedRevision: number; value: RuntimeModelInput }) =>
      api.post<{ revision: number; entry: RuntimeModel }>('/ai-runtime/models', write(input)),
    onSuccess: () => invalidateRuntimeCatalog(qc),
  });
}

export function useUpdateRuntimeModel(id: string) {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: (input: { expectedRevision: number; value: RuntimeModelInput }) =>
      api.patch<{ revision: number; entry: RuntimeModel }>(`/ai-runtime/models/${encodeURIComponent(id)}`, write(input)),
    onSuccess: () => invalidateRuntimeCatalog(qc),
  });
}

export function useCreateRuntimeProfile() {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: (input: { expectedRevision: number; value: RuntimeProfileInput }) =>
      api.post<{ revision: number; entry: RuntimeProfile }>('/ai-runtime/profiles', write(input)),
    onSuccess: () => invalidateRuntimeCatalog(qc),
  });
}

export function useUpdateRuntimeProfile(id: string) {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: (input: { expectedRevision: number; value: RuntimeProfileInput }) =>
      api.patch<{ revision: number; entry: RuntimeProfile }>(`/ai-runtime/profiles/${encodeURIComponent(id)}`, write(input)),
    onSuccess: () => invalidateRuntimeCatalog(qc),
  });
}

export function usePreviewRuntimeImport() {
  return useMutation({
    mutationFn: (input: { strategy: 'merge'; document: RuntimeExportDocument }) =>
      api.post<RuntimeImportPreview>('/ai-runtime/import/preview', input),
  });
}

export function useApplyRuntimeImport() {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: (input: { strategy: 'merge'; document: RuntimeExportDocument; validation_token: string }) =>
      api.post<RuntimeImportReport>('/ai-runtime/import/apply', input),
    onSuccess: () => invalidateRuntimeCatalog(qc),
  });
}

function write<T>(input: { expectedRevision: number; value: T }): RuntimeWrite<T> {
  return {
    expected_revision: input.expectedRevision,
    value: input.value,
  };
}

export function aiRuntimeExportHref(format: 'yaml' | 'json' = 'yaml'): string {
  return withOrgSlug(`/api/ai-runtime/export?format=${format}`);
}
