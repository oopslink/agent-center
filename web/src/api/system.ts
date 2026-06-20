import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query';
import { api } from './client';

// Build identity of the running server (@oopslink version convention:
// version = `${branch}-${git-hash}`, e.g. "v2.8.1-9908825"). Injected at build
// time via Go ldflags; sourced from the real git state, never fabricated.
export interface SystemVersion {
  version: string;
  branch: string;
  commit: string;
  built_at: string;
}

// useSystemVersion — fetch GET /api/system/version (org-agnostic build info).
// The value is fixed for the process lifetime, so it never needs refetching.
export function useSystemVersion() {
  return useQuery({
    queryKey: ['system', 'version'],
    queryFn: () => api.get<SystemVersion>('/system/version'),
    staleTime: Infinity,
    gcTime: Infinity,
  });
}

// Wake-guardrail thresholds (I7-D1 §3.5) — the live center settings exposed by
// GET/PUT /api/system/wake-guardrail. All five are positive ints; cycle_window
// is in seconds. GET returns the EFFECTIVE config (stored overrides on code
// defaults); PUT validates (all > 0) + persists, and takes effect immediately
// (the WakeGuard reads the store on every evaluation — no restart).
export interface WakeGuardrail {
  max_depth: number;
  cycle_window_sec: number;
  cycle_threshold: number;
  rate_per_min: number;
  chain_token_budget: number;
}

// useWakeGuardrail — fetch the effective wake-guardrail config (org-agnostic).
export function useWakeGuardrail() {
  return useQuery({
    queryKey: ['system', 'wake-guardrail'],
    queryFn: () => api.get<WakeGuardrail>('/system/wake-guardrail'),
  });
}

// useUpdateWakeGuardrail — PUT the thresholds; on success refresh the cached
// effective config with the server's returned (canonical) values.
export function useUpdateWakeGuardrail() {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: (body: WakeGuardrail) => api.put<WakeGuardrail>('/system/wake-guardrail', body),
    onSuccess: (data) => {
      qc.setQueryData(['system', 'wake-guardrail'], data);
    },
  });
}
