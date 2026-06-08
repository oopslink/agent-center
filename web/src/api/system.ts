import { useQuery } from '@tanstack/react-query';
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
