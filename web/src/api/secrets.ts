import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query';
import { api } from './client';
import { qk } from './queryKeys';
import type { CreateSecretInput, Secret } from './types';

export function useSecrets() {
  return useQuery({
    queryKey: qk.secrets(),
    queryFn: () => api.get<Secret[]>('/secrets'),
  });
}

// CreateSecretResult is intentionally narrow — backend only returns
// id + name + event_id on create (no plaintext, no full Secret
// projection) per ADR-0026 § 5.
export interface CreateSecretResult {
  id: string;
  name: string;
  event_id: string;
}

export function useCreateSecret() {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: (input: CreateSecretInput) =>
      api.post<CreateSecretResult>('/secrets', input),
    onSuccess: () => {
      void qc.invalidateQueries({ queryKey: qk.secrets() });
    },
  });
}

export function useRevokeSecret() {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: (id: string) => api.del<{ revoked: boolean }>(`/secrets/${id}`),
    onSuccess: () => {
      void qc.invalidateQueries({ queryKey: qk.secrets() });
    },
  });
}
