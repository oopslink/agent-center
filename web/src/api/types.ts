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

export interface AgentInstance {
  id: string;
  identity_id: string;
  name: string;
  agent_cli: string;
  state: 'idle' | 'active' | 'sleeping' | 'archived';
  worker_id?: string;
  is_builtin?: boolean;
  max_concurrent?: number;
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

// Project mirrors the backend projection emitted by projectPublicMap
// (id, name, kind, default_agent_cli, description, created_at,
// updated_at). All fields except id/name/created_at/updated_at are
// optional — the backend omits empty strings.
export interface Project {
  id: string;
  name: string;
  kind?: string;
  default_agent_cli?: string;
  description?: string;
  created_at: string;
  updated_at: string;
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
