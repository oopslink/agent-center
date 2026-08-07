import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query';
import { api } from './client';
import { qk } from './queryKeys';

export type RuntimeSelectionMode = 'inherit' | 'profile' | 'override';

export interface RuntimeSelection {
  mode: RuntimeSelectionMode;
  profile_id?: string;
  cli_id?: string;
  model_id?: string;
  parameters?: Record<string, unknown>;
}

export interface RuntimeCLI {
  id: string;
  key: string;
  display_name: string;
  executable: string;
  version_constraint?: string;
  required_features: string[];
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
  compatible_cli_keys: string[];
  default_parameters: Record<string, unknown>;
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
  parameters: Record<string, unknown>;
  enabled: boolean;
  created_at?: string;
  updated_at?: string;
}

export interface RuntimeCatalog {
  org_id: string;
  revision: number;
  default_runtime_profile_id?: string;
  clis: RuntimeCLI[];
  models: RuntimeModel[];
  profiles: RuntimeProfile[];
}

export interface RuntimeReferenceCounts {
  profile_id?: string;
  default_profile: number;
  agent_profile_selections: number;
  executor_profile_selections: number;
  team_role_profile_selections: number;
  team_role_inherit_selections: number;
  historical_execution_snapshot: number;
}

export interface RuntimeImpactPreview {
  entity_type: string;
  entity_id?: string;
  action: string;
  reference_counts: RuntimeReferenceCounts;
  affected_new_runs: number;
  historical_note: string;
  gray_release_ready: boolean;
}

export interface RuntimeMutationResponse<T> {
  revision: number;
  entry: T;
  impact_preview?: RuntimeImpactPreview;
}

export interface RuntimeDefaultResponse {
  revision: number;
  default_runtime_profile_id: string;
  impact_preview?: RuntimeImpactPreview;
}

export interface RuntimeCoverageReason {
  code: string;
  count: number;
  message: string;
}

export interface RuntimeCoverage {
  profile_id: string;
  online_worker_count: number;
  eligible_worker_count: number;
  status: string;
  reasons: RuntimeCoverageReason[];
  calculated_at: string;
}

export interface RuntimeDiagnostic {
  code: string;
  severity?: string;
  path?: string;
  entity_type?: string;
  key?: string;
  message: string;
}

export interface RuntimeCoverageResponse {
  coverage_kind: 'basic_capability_coverage';
  schedulability_kind: 'effective_schedulability_not_inferred';
  coverage: RuntimeCoverage[];
  diagnostics: RuntimeDiagnostic[];
}

export interface RuntimeWrite<T> {
  expected_revision: number;
  value: T;
}

export function useAIRuntimeCatalog() {
  return useQuery({
    queryKey: qk.aiRuntime(),
    queryFn: () => api.get<RuntimeCatalog>('/ai-runtime'),
  });
}

export function useAIRuntimeCoverage() {
  return useQuery({
    queryKey: qk.aiRuntimeCoverage(),
    queryFn: () => api.get<RuntimeCoverageResponse>('/ai-runtime/coverage'),
  });
}

export function useCreateRuntimeProfile() {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: (input: RuntimeWrite<Omit<RuntimeProfile, 'id' | 'created_at' | 'updated_at'>>) =>
      api.post<RuntimeMutationResponse<RuntimeProfile>>('/ai-runtime/profiles', input),
    onSuccess: () => {
      qc.invalidateQueries({ queryKey: qk.aiRuntime() });
      qc.invalidateQueries({ queryKey: qk.aiRuntimeCoverage() });
    },
  });
}

export function useUpdateRuntimeProfile(id: string) {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: (input: RuntimeWrite<RuntimeProfile>) =>
      api.patch<RuntimeMutationResponse<RuntimeProfile>>(`/ai-runtime/profiles/${id}`, input),
    onSuccess: () => {
      qc.invalidateQueries({ queryKey: qk.aiRuntime() });
      qc.invalidateQueries({ queryKey: qk.aiRuntimeCoverage() });
    },
  });
}

export function useSetDefaultRuntimeProfile() {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: (input: { expected_revision: number; profile_id: string }) =>
      api.put<RuntimeDefaultResponse>('/ai-runtime/default-profile', input),
    onSuccess: () => {
      qc.invalidateQueries({ queryKey: qk.aiRuntime() });
      qc.invalidateQueries({ queryKey: qk.aiRuntimeCoverage() });
    },
  });
}
