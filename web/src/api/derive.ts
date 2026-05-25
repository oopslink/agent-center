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
  // Required since v2.1-A — DeriveModal collects via the project picker.
  // Backend deriveIssue / deriveTask handlers require it at the service
  // layer (would otherwise 500 with a project_not_found error).
  project_id: string;
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
      // v2.3-5b: BC-native Issue list lives at qk.issues. Conversation
      // BC cache is still invalidated because the derive flow creates a
      // bound `kind=issue` conversation (CV4 carry-over flow).
      void qc.invalidateQueries({ queryKey: qk.conversations() });
      void qc.invalidateQueries({ queryKey: qk.issues() });
    },
  });
}

export function useDeriveTask() {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: (input: DeriveTaskInput) => api.post<DeriveTaskResult>('/tasks', input),
    onSuccess: () => {
      // v2.3-5b: BC-native Task list lives at qk.tasksList. Conversation
      // BC cache is still invalidated because the derive flow creates a
      // bound `kind=task` conversation.
      void qc.invalidateQueries({ queryKey: qk.conversations() });
      void qc.invalidateQueries({ queryKey: qk.tasksList() });
    },
  });
}
