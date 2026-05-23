import { useMutation, useQueryClient } from '@tanstack/react-query';
import { api } from './client';
import { qk } from './queryKeys';

// CV4 derivation (carry-over source messages into a fresh issue / task).
// Per ADR-0036; backend endpoints are POST /api/issues and POST /api/tasks.

export interface DeriveIssueInput {
  source_conversation_id: string;
  source_message_ids: string[];
  title: string;
  description?: string;
  project_id?: string;
}

export interface DeriveTaskInput extends DeriveIssueInput {
  agent_instance_id?: string;
}

export interface DeriveIssueResult {
  issue_id: string;
  conversation_id: string;
  reference_count: number;
  issue_event_id: string;
  carry_over_event_id: string;
}

export interface DeriveTaskResult {
  task_id: string;
  conversation_id: string;
  reference_count: number;
  task_event_id: string;
  carry_over_event_id: string;
}

export function useDeriveIssue() {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: (input: DeriveIssueInput) => api.post<DeriveIssueResult>('/issues', input),
    onSuccess: () => {
      void qc.invalidateQueries({ queryKey: qk.conversations() });
    },
  });
}

export function useDeriveTask() {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: (input: DeriveTaskInput) => api.post<DeriveTaskResult>('/tasks', input),
    onSuccess: () => {
      void qc.invalidateQueries({ queryKey: qk.conversations() });
    },
  });
}
