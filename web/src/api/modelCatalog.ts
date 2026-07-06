import { useQuery, useMutation, useQueryClient } from '@tanstack/react-query';
import { api } from './client';
import { qk } from './queryKeys';

// ModelCatalogEntry mirrors the backend pm_model_catalog row (issue-93dd8daa ①):
// an org-level, user-managed model the org's agents may run.
export interface ModelCatalogEntry {
  id: string;
  model_id: string;
  display_name: string;
  input_cost: number;
  output_cost: number;
  context_window: number;
  tier: string;
  version?: number;
  updated_at?: string;
}

// The mutable fields (create / update body; also one element of an import array).
export interface ModelCatalogFields {
  model_id: string;
  display_name: string;
  input_cost: number;
  output_cost: number;
  context_window: number;
  tier: string;
}

export function useModelCatalog() {
  return useQuery({
    queryKey: qk.modelCatalog(),
    queryFn: async () => {
      const resp = await api.get<{ entries: ModelCatalogEntry[] }>('/model-catalog');
      return resp.entries ?? [];
    },
  });
}

export function useCreateModelCatalogEntry() {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: async (data: ModelCatalogFields) => api.post<ModelCatalogEntry>('/model-catalog', data),
    onSuccess: () => qc.invalidateQueries({ queryKey: qk.modelCatalog() }),
  });
}

export function useUpdateModelCatalogEntry(id: string) {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: async (data: ModelCatalogFields) => api.put<ModelCatalogEntry>(`/model-catalog/${id}`, data),
    onSuccess: () => qc.invalidateQueries({ queryKey: qk.modelCatalog() }),
  });
}

export function useDeleteModelCatalogEntry() {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: async (id: string) => api.del(`/model-catalog/${id}`),
    onSuccess: () => qc.invalidateQueries({ queryKey: qk.modelCatalog() }),
  });
}

// useImportModelCatalog posts a raw JSON array string + mode (upsert|replace). The
// backend validates the WHOLE batch — a bad row rejects everything (surfaced as an
// error), so the panel never half-applies.
export function useImportModelCatalog() {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: async (data: { json: string; mode: 'upsert' | 'replace' }) =>
      api.post<{ ok: boolean; mode: string; imported: number }>('/model-catalog/import', data),
    onSuccess: () => qc.invalidateQueries({ queryKey: qk.modelCatalog() }),
  });
}
