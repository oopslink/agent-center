import { useQuery } from '@tanstack/react-query';
import { api } from './client';
import { qk } from './queryKeys';
import type { FleetSnapshot } from './types';

export function useFleet() {
  return useQuery({
    queryKey: qk.fleet(),
    queryFn: () => api.get<FleetSnapshot>('/fleet'),
  });
}
