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
  // owner_ref pins a task/issue conversation to its pm owner
  // (pm://tasks|issues/{id}); empty/absent for channels and DMs. v2.7 #137.
  owner_ref?: string;
  opened_at?: string;
}

// ContextRefs (v2.7 #137) — the pm/agent work-item provenance a message
// carries. Present only when the message was produced under a work item;
// absent on plain channel/DM chat. Mirrors the backend context_refs map.
export interface ContextRefs {
  work_item_ref: string;
  task_ref: string;
  agent_ref: string;
}

// MessageAttachment (v2.7 #133) — a file attached to a message: a reference to an
// uploaded blob (ac://files/{ulid}) + display metadata. Present only when the
// message carries attachments. The UI derives the display type from mime_type.
export interface MessageAttachment {
  uri: string;
  filename: string;
  mime_type: string;
  size: number;
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
  context_refs?: ContextRefs;
  attachments?: MessageAttachment[];
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
  // v2.7 #157: the agent identity-member this execution Agent represents (empty
  // for standalone agents). Lets Members navigate member→AgentDetail.
  identity_member_id?: string;
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

// WorkItemRow is one fleet work-item row (v2.7 #107: the work-item model
// replaced the retired task-execution model — executions→work_items). Mirrors
// internal/observability/query/work_item_row.go.
export interface WorkItemRow {
  work_item_id: string;
  agent_id: string;
  task_id?: string;
  status: string;
  current_activity?: string;
  total_tool_calls: number;
  total_tokens_input: number;
  total_tokens_output: number;
  // 0 in v2.7 (no per-turn duration source; deferred v2.8).
  working_seconds: number;
  last_activity_at?: string;
}

// EnvWorker (v2.7 E1 #138): the Environment-page worker — the CONTROL-CONNECTED
// view (environment.Worker), distinct from FleetWorkerRow (legacy workforce
// enrolled set). status is the control-connection state (online|offline);
// last_acked_offset is the control-stream cursor.
export interface EnvWorker {
  worker_id: string;
  organization_id: string;
  name: string;
  status: string; // 'online' | 'offline' (control-connection state)
  last_acked_offset: number;
  last_heartbeat_at?: string;
  created_at: string;
  updated_at: string;
  version: number;
}

// TransferSession (v2.7 E1 #139): an in-flight file-transfer session shown on the
// Environment page. Org is resolved server-side via the session's scope
// (fail-closed); the list only contains the caller org's open+unexpired sessions.
export interface TransferSession {
  id: string;
  file_uri: string;
  transfer_uri: string;
  direction: string; // 'upload' | 'download'
  status: string; // 'open' (the list is in-flight only)
  scope: string; // task | issue | project | conversation | agent
  scope_id: string;
  content_type: string;
  size: number;
  created_by: string;
  created_at: string;
  expires_at: string;
}

// WorkerCapability is one probed agent-CLI on a worker (v2.7 #176 /
// FINDING-C): what ProbeAllAdapters discovered + its detected/enabled state.
export interface WorkerCapability {
  agent_cli: string;
  detected: boolean;
  enabled: boolean;
  version?: string;
}

export interface FleetWorkerRow {
  worker_id: string;
  // Friendly operator-facing label (v2.4-D-X1). Falls back to
  // worker_id when unset.
  name: string;
  status: string;
  active_count: number;
  last_heartbeat_at?: string;
  // Probed agent-CLI capabilities (v2.7 #176). Omitted when the worker has
  // reported none yet.
  capabilities?: WorkerCapability[];
}

export interface FleetIssueRow {
  issue_id: string;
  project_id: string;
  title: string;
  status: string;
  opened_at: string;
  opener: string;
}

// FleetSnapshot (v2.7 #107/#118): executions→work_items; the open_input_requests
// segment was dropped — "waiting input" is a work item with status=waiting_input,
// surfaced in work_items.
export interface FleetSnapshot {
  work_items: WorkItemRow[];
  workers: FleetWorkerRow[];
  pending_issues: FleetIssueRow[];
  generated_at?: string;
  warnings?: string[];
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

// ProjectMember — read-only project membership entry (v2.7 ProjectManager BC).
export interface ProjectMember {
  id: string;
  project_id: string;
  identity_id: string;
  role: string;
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
  attachments?: MessageAttachment[];
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
