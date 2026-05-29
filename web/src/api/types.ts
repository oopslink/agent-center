// Typed mirrors of backend projections (convPublicMap, msgPublicMap,
// agentPublicMap, secretPublicMap, irPublicMap, fleet snapshot). Field
// names match the JSON keys emitted by handlers.go (snake_case).
//
// Hand-written per F4 oversight #5 (no openapi-codegen — small project).

export type ConversationKind = 'channel' | 'dm' | 'issue' | 'task' | 'adhoc' | 'notification';
export type ConversationStatus = 'active' | 'closed' | 'archived';

export interface Participant {
  identity_id: string;
  role: string;
  joined_at: string;
  joined_by: string;
  left_at?: string;
  left_reason?: string;
}

export interface Conversation {
  id: string;
  kind: ConversationKind;
  name: string;
  description?: string;
  status: ConversationStatus;
  participants?: Participant[];
  parent_conversation_id?: string;
  opened_at?: string;
}

export interface Message {
  id: string;
  conversation_id: string;
  sender_identity_id: string;
  content_kind: string;
  content: string;
  direction: 'inbound' | 'outbound' | 'internal';
  posted_at: string;
  input_request_ref?: string;
}

// Agent BC (v2.7 #101). Org-scoped agents with a lifecycle/availability
// state machine, backed by /api/agents. Replaces the retired
// workforce.AgentInstance surface. Mirrors agentMap from the backend.
export type AgentLifecycle =
  | 'stopped'
  | 'running'
  | 'stopping'
  | 'resetting'
  | 'error';

export type Availability = 'available' | 'busy' | 'unavailable';

export interface Agent {
  id: string;
  organization_id: string;
  name: string;
  description: string;
  model: string;
  cli: string;
  env_vars: Record<string, string>;
  skills: string[];
  worker_id: string;
  lifecycle: AgentLifecycle;
  availability: Availability;
  created_by: string;
  version: number;
  created_at: string;
  updated_at: string;
  lifecycle_error?: string;
}

export type WorkItemStatus =
  | 'queued'
  | 'active'
  | 'waiting_input'
  | 'done'
  | 'failed'
  | 'canceled'
  | 'superseded';

export interface AgentWorkItem {
  id: string;
  agent_id: string;
  task_ref: string;
  status: WorkItemStatus;
  interactions: number;
  version: number;
  created_at: string;
  updated_at: string;
}

export interface AgentActivityEvent {
  id: string;
  agent_id: string;
  event_type: string;
  payload: string;
  occurred_at: string;
  work_item_ref?: string;
  interaction_ref?: string;
}

export type SecretKind = 'mcp' | 'cloud_credential' | 'repo_deploy_key' | 'other';
export type SecretState = 'active' | 'revoked';

export interface Secret {
  id: string;
  name: string;
  kind: SecretKind;
  state: SecretState;
  created_at: string;
  created_by: string;
  revoked_at?: string;
  revoked_by?: string;
  revoked_reason?: string;
  revoked_message?: string;
}

export interface InputRequest {
  id: string;
  status: 'pending' | 'responded' | 'cancelled';
  execution_id: string;
  question: string;
  options?: string[];
  urgency: string;
  created_at: string;
  answer?: string;
  decided_by?: string;
  decided_at?: string;
}

export interface FleetExecutionRow {
  execution_id: string;
  task_id: string;
  worker_id: string;
  agent_cli: string;
  workspace_mode: string;
  status: string;
  current_activity?: string;
  working_seconds: number;
  started_at: string;
  projection_last_push_at?: string;
}

export interface FleetWorkerRow {
  worker_id: string;
  // Friendly operator-facing label (v2.4-D-X1). Falls back to
  // worker_id when unset.
  name: string;
  status: string;
  active_count: number;
  mappings_count: number;
  last_heartbeat_at?: string;
}

export interface FleetIRRow {
  input_request_id: string;
  task_execution_id: string;
  question: string;
  urgency: string;
  requested_at: string;
}

export interface FleetIssueRow {
  issue_id: string;
  project_id: string;
  title: string;
  status: string;
  opened_at: string;
  opener: string;
}

export interface FleetSnapshot {
  executions: FleetExecutionRow[];
  workers: FleetWorkerRow[];
  open_input_requests: FleetIRRow[];
  pending_issues: FleetIssueRow[];
  generated_at?: string;
  warnings?: string[];
}

export interface TraceEvent {
  id: string;
  event_type: string;
  occurred_at: string;
  payload?: Record<string, unknown>;
}

// Project mirrors the v2.7 ProjectManager BC projection emitted by
// projectPublicMap. `tags` was retired; the project now carries
// organization_id, a status enum (active/archived), and created_by.
export interface Project {
  id: string;
  organization_id: string;
  name: string;
  description: string;
  status: 'active' | 'archived';
  created_by: string;
  // version is required for the projection / CAS bookkeeping.
  version: number;
  created_at: string;
  updated_at: string;
}

// Issue mirrors the v2.7 ProjectManager BC Issue projection. Issues are
// project-scoped (nested under /projects/{pid}/issues). The status
// machine: open→{in_progress,withdrawn}; in_progress→{resolved,withdrawn};
// resolved→{closed,reopened}; closed→{reopened}; reopened→{open};
// withdrawn=terminal.
export type IssueStatus =
  | 'open'
  | 'in_progress'
  | 'resolved'
  | 'closed'
  | 'withdrawn'
  | 'reopened';

export interface Issue {
  id: string;
  project_id: string;
  title: string;
  description: string;
  status: IssueStatus;
  created_by: string;
  version: number;
  created_at: string;
  updated_at: string;
}

// Task mirrors the v2.7 ProjectManager BC Task projection. Tasks are
// project-scoped (nested under /projects/{pid}/tasks). New 8-state
// status machine driven by POST sub-route actions.
export type TaskStatus =
  | 'open'
  | 'assigned'
  | 'running'
  | 'blocked'
  | 'completed'
  | 'verified'
  | 'canceled'
  | 'reopened';

export interface Task {
  id: string;
  project_id: string;
  title: string;
  description: string;
  status: TaskStatus;
  assignee?: string;
  derived_from_issue?: string;
  completed_by?: string;
  blocked_reason?: string;
  version: number;
  created_at: string;
  updated_at: string;
}

// CodeRepoMap — read-only project code repo entry (v2.7).
export interface CodeRepo {
  id: string;
  project_id: string;
  url: string;
  label: string;
  added_by: string;
  created_at: string;
}

export interface ConversationMessageReference {
  id: string;
  child_conversation_id: string;
  source_conversation_id: string;
  source_message_id: string;
  created_by: string;
  created_at: string;
}

// Mutation payloads.

export interface CreateConversationInput {
  kind: 'channel' | 'dm';
  name?: string;
  description?: string;
  members?: string[];
}

export interface CreateConversationResult {
  conversation_id: string;
  event_id: string;
  kind: ConversationKind;
}

export interface SendMessageInput {
  conversationId: string;
  content: string;
  sender_identity_id?: string;
  content_kind?: string;
  direction?: 'inbound' | 'outbound' | 'internal';
  input_request_ref?: string;
}

export interface SendMessageResult {
  message_id: string;
  event_id: string;
}

export interface CreateSecretInput {
  name: string;
  kind: SecretKind;
  value: string;
}

export interface RespondInputRequestInput {
  id: string;
  answer: string;
  decided_by?: string;
}
