import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query';
import { api } from './client';
import { qk } from './queryKeys';
import type { InputRequest, RespondInputRequestInput } from './types';

export function useInputRequests() {
  return useQuery({
    queryKey: qk.inputRequests(),
    queryFn: () => api.get<InputRequest[]>('/input_requests'),
  });
}

export function useRespondInputRequest() {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: ({ id, answer, decided_by }: RespondInputRequestInput) =>
      api.post<{ event_id: string }>(`/input_requests/${id}/respond`, {
        answer,
        decided_by,
      }),
    onSuccess: () => {
      void qc.invalidateQueries({ queryKey: qk.inputRequests() });
    },
  });
}

export function useCancelInputRequest() {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: ({ id, message, reason }: { id: string; message: string; reason?: string }) =>
      api.post<{ cancelled: boolean }>(`/input_requests/${id}/cancel`, {
        message,
        reason: reason ?? 'user_cancel',
      }),
    onSuccess: () => {
      void qc.invalidateQueries({ queryKey: qk.inputRequests() });
    },
  });
}
