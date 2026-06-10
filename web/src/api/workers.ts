import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query';
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

// v2.8.1 force-delete (admin): DELETE /api/workers/{id}?force=true cleans the
// center's metadata only — the backend skips the stop/busy guards that the soft
// remove enforces (no-force + busy → 409 `worker_busy`), unbinds the worker's
// agents, and does NOT kill the process. The 200 body reports how many agents
// were unbound (`unbound_agents`) so the caller can note "N agents unbound".
// Org-admin gated server-side. ApiError surfaces 404/403/409 for the caller.
export interface ForceDeleteWorkerResult {
  ok: boolean;
  unbound_agents: number;
}

export function useForceDeleteWorker() {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: (id: string) =>
      api.del<ForceDeleteWorkerResult>(`/workers/${id}?force=true`),
    onSuccess: (_, id) => {
      void qc.invalidateQueries({ queryKey: qk.worker(id) });
      void qc.invalidateQueries({ queryKey: qk.workers() });
      void qc.invalidateQueries({ queryKey: qk.fleet() });
      // Force-delete unbinds agents → their worker_id changes; refresh agents too.
      void qc.invalidateQueries({ queryKey: qk.agents() });
    },
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
