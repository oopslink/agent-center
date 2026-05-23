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

export function useCreateSecret() {
  const qc = useQueryClient();
  return useMutation({
    // Plaintext value is sent in the request body only and never echoed
    // back by the response (per ADR-0026 § 5). The mutation result
    // contains metadata only.
    mutationFn: (input: CreateSecretInput) =>
      api.post<Omit<Secret, 'name'> & { name: string }>('/secrets', input),
    onSuccess: () => {
      void qc.invalidateQueries({ queryKey: qk.secrets() });
    },
  });
}

export function useRevokeSecret() {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: (id: string) => api.del<{ event_id: string }>(`/secrets/${id}`),
    onSuccess: () => {
      void qc.invalidateQueries({ queryKey: qk.secrets() });
    },
  });
}
