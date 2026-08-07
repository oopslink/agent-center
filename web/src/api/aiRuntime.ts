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

export function aiRuntimeExportHref(format: 'yaml' | 'json' = 'yaml'): string {
  return withOrgSlug(`/api/ai-runtime/export?format=${format}`);
}
