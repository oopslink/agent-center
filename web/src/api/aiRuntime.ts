import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query';
import { api } from './client';
import { currentOrgScope, qk } from './queryKeys';

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
  enabled: boolean;
  system: boolean;
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
}

export interface RuntimeCoverage {
  scope: 'basic_capability_coverage' | string;
  profile_id: string;
  online_worker_count: number;
  eligible_worker_count: number;
  status: string;
  calculated_at: string;
  reasons?: Array<{ code: string; count: number; message: string }>;
}

export interface RuntimeCatalog {
  org_id: string;
  revision: number;
  default_runtime_profile_id?: string;
  clis: RuntimeCLI[];
  models: RuntimeModel[];
  profiles: RuntimeProfile[];
  coverage?: RuntimeCoverage[];
}

export interface RuntimeRolloutPlan {
  enabled?: boolean;
  percent?: number;
  label?: string;
}

export interface RuntimeReferenceCount {
  source: string;
  entity_type: string;
  entity_id: string;
  count: number;
  mutable: boolean;
}

export interface RuntimeImpactPreview {
  entity_type: string;
  entity_id: string;
  action: string;
  reference_counts: RuntimeReferenceCount[];
  basic_capability_coverage: RuntimeCoverage[];
  execution_schedulability: RuntimeCoverage[];
  snapshot_back_mutation: boolean;
  historical_snapshot_policy: string;
  rollout?: RuntimeRolloutPlan;
  calculated_at: string;
}

export interface RuntimeAuditEvent {
  id: string;
  actor: string;
  entity_type: string;
  entity_key: string;
  action: string;
  revision: number;
  occurred_at: string;
}

export function useRuntimeCatalog() {
  return useQuery({
    queryKey: qk.aiRuntime(),
    queryFn: () => api.get<RuntimeCatalog>('/ai-runtime'),
    enabled: currentOrgScope() !== 'no-org',
  });
}

export function useRuntimeAudit() {
  return useQuery({
    queryKey: qk.aiRuntimeAudit(),
    queryFn: async () => (await api.get<{ entries: RuntimeAuditEvent[] }>('/ai-runtime/audit')).entries,
    enabled: currentOrgScope() !== 'no-org',
  });
}

export function useRuntimeImpact(entityType: string, entityId: string, action: string) {
  return useQuery({
    queryKey: qk.aiRuntimeImpact(entityType, entityId, action),
    queryFn: () =>
      api.get<RuntimeImpactPreview>(
        `/ai-runtime/impact?entity_type=${encodeURIComponent(entityType)}&entity_id=${encodeURIComponent(entityId)}&action=${encodeURIComponent(action)}`,
      ),
    enabled: currentOrgScope() !== 'no-org' && !!entityType && !!entityId,
  });
}

export function useSetRuntimeDefaultProfile() {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: (body: { expected_revision: number; profile_id: string; rollout?: RuntimeRolloutPlan }) =>
      api.put<{ revision: number; default_runtime_profile_id: string; impact: RuntimeImpactPreview }>('/ai-runtime/default-profile', body),
    onSuccess: () => {
      void qc.invalidateQueries({ queryKey: qk.aiRuntime() });
      void qc.invalidateQueries({ queryKey: qk.aiRuntimeAudit() });
    },
  });
}
