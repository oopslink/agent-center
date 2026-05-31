import { useQuery } from '@tanstack/react-query';
import { api } from './client';
import { qk } from './queryKeys';
import type { EnvWorker, TransferSession } from './types';

// Environment BC (v2.7 E1 #138). Org-scoped control-connected workers backed by
// GET /api/workers (the Environment page's worker view). The list response is a
// WRAPPED object ({workers:[...]}); detail returns a bare EnvWorker.

export function useWorkers() {
  return useQuery({
    queryKey: qk.workers(),
    queryFn: async () => {
      const resp = await api.get<{ workers: EnvWorker[] }>('/workers');
      return resp.workers;
    },
  });
}

export function useWorker(id: string | undefined) {
  return useQuery({
    queryKey: qk.worker(id ?? ''),
    queryFn: () => api.get<EnvWorker>(`/workers/${id}`),
    enabled: !!id,
  });
}

// In-flight file-transfer sessions for the org (Environment page, #139). The list
// is org-scoped + open-only server-side (scope→org fail-closed). Wrapped response.
export function useTransferSessions() {
  return useQuery({
    queryKey: qk.transferSessions(),
    queryFn: async () => {
      const resp = await api.get<{ transfer_sessions: TransferSession[] }>('/files/transfers');
      return resp.transfer_sessions;
    },
  });
}
