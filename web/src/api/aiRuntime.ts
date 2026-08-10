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

export interface AIRuntimeCatalog {
  org_id?: string;
  revision: number;
  clis: RuntimeCLI[];
  models: RuntimeModel[];
}

export type RuntimeModelInput = {
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
};

export type RuntimeCLIInput = {
  key: string;
  display_name: string;
  executable: string;
  version_constraint?: string;
  required_features: string[];
  parameter_schema?: unknown;
  enabled: boolean;
};

export type RuntimeEntityKind = 'models' | 'clis';

export type RuntimeInputByKind = {
  models: RuntimeModelInput;
  clis: RuntimeCLIInput;
};

export type RuntimeEntryByKind = {
  models: RuntimeModel;
  clis: RuntimeCLI;
};

export interface RuntimeWriteResponse<T> {
  revision: number;
  entry: T;
}

export type RuntimeImportStrategy = 'merge' | 'create_only' | 'replace';

export interface AIRuntimeExportCLI {
  key: string;
  display_name: string;
  executable: string;
  version_constraint?: string;
  required_features: string[];
  parameter_schema?: unknown;
  enabled: boolean;
}

export interface AIRuntimeExportModel {
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

export interface AIRuntimeExportDocument {
  schema_version: number;
  kind: 'agent-center-ai-runtime';
  exported_at: string;
  runtime: {
    clis: AIRuntimeExportCLI[];
    models: AIRuntimeExportModel[];
  };
}

export interface RuntimeImportItem {
  entity_type: 'cli' | 'model' | string;
  key: string;
  action: 'create' | 'update' | 'unchanged' | 'disable' | string;
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

export interface RuntimeImportPreviewResponse {
  report: RuntimeImportReport;
  validation_token: string;
  expires_at: string;
  document_sha256: string;
}

export class RuntimeImportError extends Error {
  readonly status: number;
  readonly code: string;
  readonly report?: RuntimeImportReport;
  readonly diagnostics: RuntimeImportDiagnostic[];

  constructor(status: number, code: string, message: string, report?: RuntimeImportReport) {
    super(`[${status} ${code}] ${message}`);
    this.name = 'RuntimeImportError';
    this.status = status;
    this.code = code;
    this.report = report;
    this.diagnostics = report?.diagnostics ?? [];
  }
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

export function useCreateRuntimeEntry<K extends RuntimeEntityKind>(kind: K) {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: (input: { expectedRevision: number; value: RuntimeInputByKind[K] }) =>
      api.post<RuntimeWriteResponse<RuntimeEntryByKind[K]>>(`/ai-runtime/${kind}`, {
        expected_revision: input.expectedRevision,
        value: input.value,
      }),
    onSuccess: () => qc.invalidateQueries({ queryKey: qk.aiRuntimeCatalog() }),
  });
}

export function useUpdateRuntimeEntry<K extends RuntimeEntityKind>(kind: K) {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: (input: { id: string; expectedRevision: number; value: RuntimeInputByKind[K] }) =>
      api.patch<RuntimeWriteResponse<RuntimeEntryByKind[K]>>(`/ai-runtime/${kind}/${encodeURIComponent(input.id)}`, {
        expected_revision: input.expectedRevision,
        value: input.value,
      }),
    onSuccess: () => qc.invalidateQueries({ queryKey: qk.aiRuntimeCatalog() }),
  });
}

export function usePreviewRuntimeImport() {
  return useMutation({
    mutationFn: (input: { strategy: RuntimeImportStrategy; document: AIRuntimeExportDocument }) =>
      postRuntimeImport<RuntimeImportPreviewResponse>('/ai-runtime/import/preview', input),
  });
}

export function useApplyRuntimeImport() {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: (input: {
      strategy: RuntimeImportStrategy;
      document: AIRuntimeExportDocument;
      validation_token: string;
    }) => postRuntimeImport<RuntimeImportReport>('/ai-runtime/import/apply', input),
    onSuccess: () => qc.invalidateQueries({ queryKey: qk.aiRuntimeCatalog() }),
  });
}

export function aiRuntimeExportHref(format: 'yaml' | 'json' = 'yaml'): string {
  return withOrgSlug(`/api/ai-runtime/export?format=${format}`);
}

async function postRuntimeImport<T>(path: string, body: unknown): Promise<T> {
  const resp = await fetch(`/api${withOrgSlug(path)}`, {
    method: 'POST',
    credentials: 'same-origin',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify(body),
  });
  const parsed = await safeJSON(resp);
  if (!resp.ok) {
    const report = isRecord(parsed) && isRuntimeImportReport(parsed.report) ? parsed.report : undefined;
    const nestedError = isRecord(parsed) && isRecord(parsed.error) ? parsed.error : undefined;
    const code =
      typeof nestedError?.reason === 'string'
        ? nestedError.reason
        : isRecord(parsed) && typeof parsed.error === 'string'
          ? parsed.error
          : 'http_error';
    const message =
      typeof nestedError?.message === 'string'
        ? nestedError.message
        : isRecord(parsed) && typeof parsed.message === 'string'
          ? parsed.message
          : resp.statusText;
    throw new RuntimeImportError(resp.status, code, message, report);
  }
  return parsed as T;
}

async function safeJSON(resp: Response): Promise<unknown> {
  try {
    return await resp.json();
  } catch {
    return null;
  }
}

function isRecord(value: unknown): value is Record<string, unknown> {
  return !!value && typeof value === 'object' && !Array.isArray(value);
}

function isRuntimeImportReport(value: unknown): value is RuntimeImportReport {
  return isRecord(value) && Array.isArray(value.items) && Array.isArray(value.diagnostics);
}
