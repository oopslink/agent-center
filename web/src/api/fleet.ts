import { useQuery } from '@tanstack/react-query';
import { api } from './client';
import { qk } from './queryKeys';
import type { FleetSnapshot, TraceEvent } from './types';

export function useFleet() {
  return useQuery({
    queryKey: qk.fleet(),
    queryFn: () => api.get<FleetSnapshot>('/fleet'),
  });
}

export function useTaskTrace(taskId: string | undefined) {
  return useQuery({
    queryKey: qk.taskTrace(taskId ?? ''),
    queryFn: () => api.get<TraceEvent[]>(`/tasks/${taskId}/trace`),
    enabled: !!taskId,
  });
}
