import { useMutation, useQueryClient } from '@tanstack/react-query';
import { api } from './client';
import { qk } from './queryKeys';

// CV4 derivation (carry-over source messages into a fresh issue / task).
// Per ADR-0036; backend endpoints are POST /api/issues and POST /api/tasks.

interface DeriveInput {
  source_conversation_id: string;
  source_message_ids: string[];
  name: string;
  description?: string;
}

interface DeriveResult {
  conversation_id: string;
  event_id: string;
}

export function useDeriveIssue() {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: (input: DeriveInput) => api.post<DeriveResult>('/issues', input),
    onSuccess: () => {
      void qc.invalidateQueries({ queryKey: qk.conversations() });
    },
  });
}

export function useDeriveTask() {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: (input: DeriveInput) => api.post<DeriveResult>('/tasks', input),
    onSuccess: () => {
      void qc.invalidateQueries({ queryKey: qk.conversations() });
    },
  });
}
