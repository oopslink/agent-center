import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query';
import { api } from './client';
import { currentOrgScope, qk } from './queryKeys';

export type RuntimeSelectionMode = 'inherit' | 'profile' | 'override';

export interface RuntimeSelection {
  mode: RuntimeSelectionMode | '';
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

export interface RuntimeCatalog {
  org_id: string;
  revision: number;
  default_runtime_profile_id?: string;
  clis: RuntimeCLI[];
  models: RuntimeModel[];
  profiles: RuntimeProfile[];
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

export interface BasicCoverageResponse {
  basic_capability_coverage: RuntimeCoverage[];
  diagnostics: RuntimeDiagnostic[];
  effective_schedulability_note: string;
}

export interface RuntimeReference {
  entity_type: string;
  entity_id: string;
  entity_name?: string;
  field: string;
  mode?: string;
}

export interface RuntimeImpactPreview {
  entity_type: string;
  entity_id?: string;
  entity_key?: string;
  action?: string;
  reference_count: number;
  references: RuntimeReference[];
  basic_capability_coverage: RuntimeCoverage[];
  diagnostics: RuntimeDiagnostic[];
  canary_percent?: number;
  canary_supported: boolean;
  historical_snapshots_immutable: boolean;
  effective_schedulability_note: string;
  historical_snapshot_immutability: string;
}

export interface RuntimeAuditEvent {
  ID?: string;
  OrgID?: string;
  Actor?: string;
  EntityType?: string;
  EntityKey?: string;
  Action?: string;
  Before?: unknown;
  After?: unknown;
  Revision?: number;
  OccurredAt?: string;
  id?: string;
  actor?: string;
  entity_type?: string;
  entity_key?: string;
  action?: string;
  revision?: number;
  occurred_at?: string;
}

export function useAIRuntimeCatalog() {
  return useQuery({ queryKey: qk.aiRuntime(), queryFn: () => api.get<RuntimeCatalog>('/ai-runtime') });
}

export function useAIRuntimeBasicCoverage() {
  return useQuery({
    queryKey: qk.aiRuntimeCoverage(),
    queryFn: () => api.get<BasicCoverageResponse>('/ai-runtime/basic-coverage'),
  });
}

export function useAIRuntimeImpact(params: {
  entity_type: string;
  entity_id?: string;
  entity_key?: string;
  action?: string;
  canary_percent?: number;
}) {
  const search = new URLSearchParams();
  for (const [key, value] of Object.entries(params)) {
    if (value !== undefined && value !== '') search.set(key, String(value));
  }
  return useQuery({
    queryKey: ['org', currentOrgScope(), 'ai-runtime', 'impact', params],
    queryFn: () => api.get<RuntimeImpactPreview>(`/ai-runtime/impact?${search.toString()}`),
    enabled:
      !!params.entity_type &&
      (params.entity_type === 'default_profile' || !!params.entity_id || !!params.entity_key),
  });
}

export function useAIRuntimeAudit(limit = 20) {
  return useQuery({
    queryKey: ['org', currentOrgScope(), 'ai-runtime', 'audit', limit],
    queryFn: async () => {
      const res = await api.get<{ events: RuntimeAuditEvent[] }>(`/ai-runtime/audit?limit=${limit}`);
      return res.events ?? [];
    },
  });
}

export function useSetDefaultRuntimeProfile() {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: (input: { expected_revision: number; profile_id: string }) =>
      api.put<{ revision: number; default_runtime_profile_id: string }>('/ai-runtime/default-profile', input),
    onSuccess: () => {
      void qc.invalidateQueries({ queryKey: qk.aiRuntime() });
      void qc.invalidateQueries({ queryKey: qk.aiRuntimeCoverage() });
      void qc.invalidateQueries({ queryKey: ['org', currentOrgScope(), 'ai-runtime'] });
    },
  });
}

export function useUpdateRuntimeProfile() {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: (input: { expected_revision: number; profile: RuntimeProfile }) =>
      api.patch<{ revision: number; entry: RuntimeProfile }>(`/ai-runtime/profiles/${input.profile.id}`, {
        expected_revision: input.expected_revision,
        value: input.profile,
      }),
    onSuccess: () => {
      void qc.invalidateQueries({ queryKey: qk.aiRuntime() });
      void qc.invalidateQueries({ queryKey: qk.aiRuntimeCoverage() });
      void qc.invalidateQueries({ queryKey: ['org', currentOrgScope(), 'ai-runtime'] });
    },
  });
}

export interface ResolvedRuntimeSelection {
  cli: RuntimeCLI;
  model: RuntimeModel;
  profile?: RuntimeProfile;
  label: string;
}

export function defaultRuntimeSelection(): RuntimeSelection {
  return { mode: 'inherit' };
}

export function normalizeRuntimeSelection(selection?: RuntimeSelection): RuntimeSelection {
  if (!selection || !selection.mode) return defaultRuntimeSelection();
  if (selection.mode === 'inherit') return { mode: 'inherit' };
  if (selection.mode === 'profile') return { mode: 'profile', profile_id: selection.profile_id ?? '' };
  return {
    mode: 'override',
    cli_id: selection.cli_id ?? '',
    model_id: selection.model_id ?? '',
    parameters: selection.parameters,
  };
}

export function selectionFromLegacy(cli: string, model: string): RuntimeSelection {
  if (cli && model) return { mode: 'override', cli_id: cli, model_id: model };
  return defaultRuntimeSelection();
}

export function resolveRuntimeSelection(
  catalog: RuntimeCatalog | undefined,
  selection: RuntimeSelection | undefined,
): ResolvedRuntimeSelection | null {
  if (!catalog) return null;
  const sel = normalizeRuntimeSelection(selection);
  let profile: RuntimeProfile | undefined;
  let cliKey = '';
  let modelKey = '';
  if (sel.mode === 'inherit') {
    profile = catalog.profiles.find((p) => p.id === catalog.default_runtime_profile_id);
    cliKey = profile?.cli_key ?? '';
    modelKey = profile?.model_key ?? '';
  } else if (sel.mode === 'profile') {
    profile = catalog.profiles.find((p) => p.id === sel.profile_id || p.key === sel.profile_id);
    cliKey = profile?.cli_key ?? '';
    modelKey = profile?.model_key ?? '';
  } else {
    cliKey = sel.cli_id ?? '';
    modelKey = sel.model_id ?? '';
  }
  const cli = catalog.clis.find((c) => c.enabled && (c.id === cliKey || c.key === cliKey));
  const model = catalog.models.find((m) => m.enabled && (m.id === modelKey || m.key === modelKey || m.model_key === modelKey));
  if (!cli || !model || !model.compatible_cli_keys.includes(cli.key)) return null;
  const prefix = sel.mode === 'inherit' ? 'Inherit' : sel.mode === 'profile' ? profile?.name ?? 'Profile' : 'Override';
  return { cli, model, profile, label: `${prefix}: ${cli.key} / ${model.model_key}` };
}

export function enabledRuntimeProfiles(catalog: RuntimeCatalog | undefined): RuntimeProfile[] {
  return (catalog?.profiles ?? []).filter((p) => p.enabled);
}

export function enabledRuntimeCLIs(catalog: RuntimeCatalog | undefined): RuntimeCLI[] {
  return (catalog?.clis ?? []).filter((c) => c.enabled);
}

export function enabledRuntimeModelsForCLI(
  catalog: RuntimeCatalog | undefined,
  cliKey: string,
): RuntimeModel[] {
  return (catalog?.models ?? []).filter((m) => m.enabled && m.compatible_cli_keys.includes(cliKey));
}
