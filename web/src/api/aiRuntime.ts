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

export interface AIRuntimeCatalog {
  org_id?: string;
  revision: number;
  default_runtime_profile_id?: string;
  clis: RuntimeCLI[];
  models: RuntimeModel[];
  profiles: RuntimeProfile[];
}

export type RuntimeCLIInput = Pick<
  RuntimeCLI,
  'key' | 'display_name' | 'executable' | 'version_constraint' | 'required_features' | 'parameter_schema' | 'enabled'
>;

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

export type RuntimeProfileInput = Pick<
  RuntimeProfile,
  'key' | 'name' | 'description' | 'cli_key' | 'model_key' | 'parameters' | 'enabled'
>;

export interface RuntimeMutationInput<T> {
  expectedRevision: number;
  value: T;
}

export interface RuntimeMutationResult<T> {
  revision: number;
  entry: T;
}

export type RuntimeImportStrategy = 'merge' | 'create_only' | 'replace';

export interface RuntimeExportDocument {
  schema_version: number;
  kind: 'agent-center-ai-runtime';
  exported_at: string;
  runtime: {
    default_profile_key?: string;
    clis: RuntimeExportCLI[];
    models: RuntimeExportModel[];
    profiles: RuntimeExportProfile[];
  };
}

export interface RuntimeExportCLI {
  key: string;
  display_name: string;
  executable: string;
  version_constraint?: string;
  required_features: string[];
  parameter_schema: unknown;
  enabled: boolean;
}

export interface RuntimeExportModel {
  key: string;
  model_key: string;
  display_name: string;
  compatible_cli_keys: string[];
  default_parameters: Record<string, unknown>;
  enabled: boolean;
  context_window?: number;
  input_cost_per_mtok?: number;
  output_cost_per_mtok?: number;
  tier?: string;
}

export interface RuntimeExportProfile {
  key: string;
  name: string;
  description?: string;
  cli_key: string;
  model_key: string;
  parameters: Record<string, unknown>;
  enabled: boolean;
}

export interface RuntimeImportItem {
  entity_type: string;
  key: string;
  action: string;
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

export function canManageAIRuntime(role: string | undefined): boolean {
  return role === 'owner' || role === 'admin';
}

export function useAIRuntimeCatalog() {
  return useQuery({
    queryKey: qk.aiRuntimeCatalog(),
    queryFn: () => api.get<AIRuntimeCatalog>('/ai-runtime'),
  });
}

function invalidateRuntimeCatalog(qc: ReturnType<typeof useQueryClient>) {
  void qc.invalidateQueries({ queryKey: qk.aiRuntimeCatalog() });
  // The retired /model-catalog compatibility endpoints project the same model
  // data. Keep old readers coherent while AI Runtime is the UI source of truth.
  void qc.invalidateQueries({ queryKey: qk.modelCatalog() });
}

export function useCreateRuntimeCLI() {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: (input: RuntimeMutationInput<RuntimeCLIInput>) =>
      api.post<RuntimeMutationResult<RuntimeCLI>>('/ai-runtime/clis', {
        expected_revision: input.expectedRevision,
        value: input.value,
      }),
    onSuccess: () => invalidateRuntimeCatalog(qc),
  });
}

export function useUpdateRuntimeCLI(id: string) {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: (input: RuntimeMutationInput<RuntimeCLIInput>) =>
      api.patch<RuntimeMutationResult<RuntimeCLI>>(`/ai-runtime/clis/${id}`, {
        expected_revision: input.expectedRevision,
        value: input.value,
      }),
    onSuccess: () => invalidateRuntimeCatalog(qc),
  });
}

export function useCreateRuntimeModel() {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: (input: RuntimeMutationInput<RuntimeModelInput>) =>
      api.post<RuntimeMutationResult<RuntimeModel>>('/ai-runtime/models', {
        expected_revision: input.expectedRevision,
        value: input.value,
      }),
    onSuccess: () => invalidateRuntimeCatalog(qc),
  });
}

export function useUpdateRuntimeModel(id: string) {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: (input: RuntimeMutationInput<RuntimeModelInput>) =>
      api.patch<RuntimeMutationResult<RuntimeModel>>(`/ai-runtime/models/${id}`, {
        expected_revision: input.expectedRevision,
        value: input.value,
      }),
    onSuccess: () => invalidateRuntimeCatalog(qc),
  });
}

export function useCreateRuntimeProfile() {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: (input: RuntimeMutationInput<RuntimeProfileInput>) =>
      api.post<RuntimeMutationResult<RuntimeProfile>>('/ai-runtime/profiles', {
        expected_revision: input.expectedRevision,
        value: input.value,
      }),
    onSuccess: () => invalidateRuntimeCatalog(qc),
  });
}

export function useUpdateRuntimeProfile(id: string) {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: (input: RuntimeMutationInput<RuntimeProfileInput>) =>
      api.patch<RuntimeMutationResult<RuntimeProfile>>(`/ai-runtime/profiles/${id}`, {
        expected_revision: input.expectedRevision,
        value: input.value,
      }),
    onSuccess: () => invalidateRuntimeCatalog(qc),
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
    onSuccess: () => invalidateRuntimeCatalog(qc),
  });
}

export function usePreviewAIRuntimeImport() {
  return useMutation({
    mutationFn: (input: { strategy: RuntimeImportStrategy; document: RuntimeExportDocument }) =>
      api.post<RuntimeImportPreview>('/ai-runtime/import/preview', input),
  });
}

export function useApplyAIRuntimeImport() {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: (input: { strategy: RuntimeImportStrategy; document: RuntimeExportDocument; validationToken: string }) =>
      api.post<RuntimeImportReport>('/ai-runtime/import/apply', {
        strategy: input.strategy,
        document: input.document,
        validation_token: input.validationToken,
      }),
    onSuccess: () => invalidateRuntimeCatalog(qc),
  });
}

export function aiRuntimeExportHref(format: 'yaml' | 'json' = 'yaml'): string {
  return withOrgSlug(`/api/ai-runtime/export?format=${format}`);
}
