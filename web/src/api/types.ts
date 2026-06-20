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
  // v2.8.1 list-enrich (contract LOCK): on the channel-list row summary the
  // backend embeds the name + kind so the avatar-stack avoids a per-row member
  // lookup; absent on the detail projection (resolve via the resolver).
  display_name?: string;
  kind?: string; // 'agent' | 'human' — drives the Avatar kind on the row summary.
}

export interface Conversation {
  id: string;
  kind: ConversationKind;
  name: string;
  description?: string;
  status: ConversationStatus;
  participants?: Participant[];
  // v2.7.1 #215: for DM kind, the resolved other party (participants - self).
  // Absent on channels / malformed DMs → UI falls back to "Direct message".
  peer_identity_id?: string;
  peer_display_name?: string;
  parent_conversation_id?: string;
  // owner_ref pins a task/issue conversation to its pm owner
  // (pm://tasks|issues/{id}); empty/absent for channels and DMs. v2.7 #137.
  owner_ref?: string;
  opened_at?: string;
  // v2.8 #264 P1 / #268 (unread/badge/follow contract §2) — computed for the
  // requesting HUMAN user, embedded per-row in GET /conversations + GET /:id.
  // human-only (Q-T1): agent sessions always get 0/0/false (backend skip-writes
  // the read/follow rows, so they don't exist). mention_count ≤ unread_count.
  // Optional on the type because legacy/older payloads may omit them (treat as 0/false).
  unread_count?: number; // messages with id > my last_seen_message_id (999+ cap); 0 when caught up.
  mention_count?: number; // subset of unread that @-mention me; 0 when none.
  followed?: boolean; // am I following this conversation/thread (§4).
  // v2.8.1 list-enrichment (mock=contract, Dev backend in parallel — VERIFY vs
  // the real GET /conversations later). These are server-side-enriched summary
  // fields embedded per channel row so the Channels list renders rich previews
  // without N+1 client fetches. All optional: older/legacy payloads omit them
  // and the UI degrades gracefully (omit / placeholder).
  created_at?: string; // RFC3339; rendered via formatLocalTime (LOCAL tz, not raw Z).
  // CONTRACT ASSUMPTION: the existing `participants?: Participant[]` field (above)
  // is reused for the row summary — on a list row the backend embeds the first
  // few participants (each may carry an inline display_name; the non-summary
  // Participant fields like role/joined_at may be omitted on this projection).
  // `participant_count` is the TOTAL (drives the "+N" overflow + the count text).
  participant_count?: number;
  recent_messages?: RecentMessageSummary[]; // ≤3, newest-relevant; plain-text previews.
}

// v2.8.1 list-enrichment: a ≤3 recent-message preview embedded per channel row.
// `content` is a PLAIN-TEXT preview (the UI renders it as text, never as a
// markdown block / code / image). `sender_identity_id` is a (possibly deleted)
// identity ref — resolve via useDisplayNameResolver, render "(deleted)" on a miss.
export interface RecentMessageSummary {
  id: string;
  sender_identity_id: string;
  // contract LOCK: the backend resolves the sender's display name server-side.
  // Use it directly; a deleted sender → empty/raw-ref → the UI renders "(deleted)".
  sender_display_name?: string;
  content: string;
  posted_at: string;
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
  // v2.9.1 Threads (mock=contract, P1-BE in parallel — VERIFY vs the real
  // GET /conversations/{id}/messages later). A message in the main list is
  // TOP-LEVEL (replies are excluded from that endpoint); a message inside a
  // thread carries `parent_message_id` (its root). `reply_count` +
  // `thread_last_activity_at` are computed server-side on the top-level row and
  // drive the per-message ThreadButton (count chip + has-activity dot). All
  // optional: legacy/older payloads omit them (treat as not-a-thread / 0).
  parent_message_id?: string; // set on a reply; absent on a top-level message.
  reply_count?: number; // # direct replies to this top-level message; 0/absent = none.
  thread_last_activity_at?: string; // RFC3339 of the most recent reply; drives sort.
  // v2.9.1 P3: per-user "new activity since last viewed" — server-derived
  // (latest reply id > my conversation last_seen). Drives the thread-button dot.
  // Absent/false on older payloads or for agents (no read state).
  has_new_activity?: boolean;
}

// v2.9.1 Threads P2 (mock=contract, P2-BE in parallel — VERIFY vs the real
// GET /conversations/{id}/threads later). One summary per thread (root message)
// in the conversation: the root message + its reply count + last activity. Drives
// the Participants-sidebar thread list (preview + count chip + has-activity dot +
// activity sort). `root` is a full Message so clicking opens the SAME ThreadSidebar
// (which takes a root Message) with no extra fetch.
export interface ThreadSummary {
  root: Message;
  reply_count: number;
  thread_last_activity_at?: string; // RFC3339 of the most recent reply; drives sort.
  // v2.9.1 P3: per-user "new activity since last viewed" (server-derived). Drives
  // the thread-list dot. Absent/false on older payloads or for agents.
  has_new_activity?: boolean;
}

// Agent BC (v2.7 #101). Org-scoped agents with a lifecycle/availability
// state machine, backed by /api/agents. Replaces the retired
// workforce.AgentInstance surface. Mirrors agentMap from the backend.
export type AgentLifecycle =
  | 'stopped'
  | 'running'
  | 'stopping'
  | 'resetting'
  | 'error'
  // v2.8 #272: terminal soft-archive state. Archived agents are excluded from
  // the default list (GET ?include_archived=true to include) but GET-by-id
  // still resolves (history/detail). No un-archive in v2.8.
  | 'archived';

export type Availability = 'available' | 'busy' | 'unavailable';

export interface Agent {
  id: string;
  organization_id: string;
  name: string;
  description: string;
  model: string;
  cli: string;
  // T236: real LLM tuning (were hardcoded UI placeholders). Optional; "" = the
  // runtime/center default. reasoning ∈ minimal|low|medium|high.
  reasoning?: string;
  mode?: string;
  provider?: string;
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
  // v2.7.1 #228/#120 — Profile-only enrichment, present ONLY on the single-agent
  // detail load (GET /api/agents/{id}), never on the list. Each is omitted when
  // unresolvable; created_agents is always an array (never null, #183 contract).
  created_by_display_name?: string;
  computer?: AgentComputer;
  created_agents?: AgentRef[];
  // v2.8.1 list-enrichment (mock=contract, Dev backend in parallel — VERIFY vs
  // the real GET /api/agents later). Embedded per agent row so the Agents list
  // shows last-activity without a per-row activity fetch (no N+1). Both optional:
  // an agent with no activity omits them and the UI shows a friendly placeholder.
  last_activity_at?: string; // RFC3339; rendered via formatLocalTime (LOCAL tz).
  last_activity_content?: string; // single-line PLAIN-TEXT preview (truncated in UI).
}

// v2.7.1 #120: the bound worker's label + connected state. daemon version is
// deliberately NOT included (no Worker BC field — the UI never fabricates it).
export interface AgentComputer {
  worker_id: string;
  name: string;
  status: string;
  connected: boolean;
}

// v2.7.1 #120: a minimal {id,name} reference to another agent (the sub-agents
// this agent created).
export interface AgentRef {
  id: string;
  name: string;
}

export type WorkItemStatus =
  | 'queued'
  | 'active'
  | 'paused' // v2.8.1 #278 D: agent paused this item to switch to another (scheduling autonomy)
  | 'waiting_input'
  | 'done'
  | 'failed'
  | 'canceled'
  | 'superseded';

export interface AgentWorkItem {
  id: string;
  agent_id: string;
  task_ref: string;
  // v2.7.1 #206: bare task id + resolved title + parent project for display +
  // linking (read-side join; absent until the DTO carries them → UI falls back).
  task_id?: string;
  task_title?: string;
  project_id?: string;
  // T100: the underlying task's org_ref ("T<n>") so the work-item list/card shows
  // T84 instead of an id-tail (#b6eb82). Absent when the task has no org_number
  // (UI falls back), mirroring the task/issue DTO contract.
  org_ref?: string;
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
  // v2.7.1 #206: resolved task title + parent project for display + linking.
  task_title?: string;
  // v2.10.2 [T140]: resolved org_ref token ("T<n>") so the Worker Activity feed
  // shows "T<n> + title" instead of a raw task-<id>. Empty → UI falls back to #hash.
  task_org_ref?: string;
  project_id?: string;
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
  enrolled_at?: string; // #273: registration time (envWorkerMap emits it; ≠ created_at)
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
  // v2.10.0 #T81 (§3.4.1): per-project counts for the Projects list cards
  // ("12 tasks · 3 issues · 4 plans · 2 repos"). Present ONLY on the LIST
  // response (GET /projects); the single-project GET omits them — hence optional.
  task_count?: number;
  issue_count?: number;
  plan_count?: number;
  repo_count?: number;
}

// Issue mirrors the v2.7 ProjectManager BC Issue projection. Issues are
// project-scoped (nested under /projects/{pid}/issues). The status
// machine: open→{in_progress,discarded}; in_progress→{resolved,discarded};
// resolved→{closed,reopened}; closed→{reopened}; reopened→{open};
// discarded=terminal.
export type IssueStatus =
  | 'open'
  | 'in_progress'
  | 'resolved'
  | 'closed'
  | 'discarded'
  | 'reopened';

export interface Issue {
  id: string;
  project_id: string;
  title: string;
  description: string;
  status: IssueStatus;
  created_by: string;
  // v2.8.1 issue-edit (#251, mirror of task #278): free-form label set (cleaned +
  // deduped + bounded to ≤16 runes each, ≤10 entries by the backend). The DTO
  // emits a non-nil array ([] when none); optional on the type so older payloads
  // that omit it are treated as no tags.
  tags?: string[];
  // v2.7.1 #245: org-internal display/reference token ("I1234"). The hash `id`
  // (issue-xxx) stays the stable internal ref (URL/API). Absent until the
  // backend DTO carries it (migration 0049) → UI falls back to the id handle.
  org_ref?: string;
  // v2.8.1 sidebar-align: when the issue last changed status, emitted by the
  // Issue DTO (rfc3339OrEmpty(StatusChangedAt)). Drives the in-status duration
  // text in the sidebar (mirror of Task). Optional — older payloads may omit it.
  status_changed_at?: string;
  version: number;
  created_at: string;
  updated_at: string;
}

// Task mirrors the v2.7 ProjectManager BC Task projection. Tasks are
// project-scoped (nested under /projects/{pid}/tasks). ADR-0046 simplified
// state machine: open | running | completed | discarded | reopened. `blocked`
// and `verified` are NO LONGER statuses — "stuck" is now a `blocked_reason`
// annotation on a RUNNING task (see Task.blocked_reason below).
export type TaskStatus =
  | 'open'
  | 'running'
  | 'completed'
  | 'discarded'
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
  // ADR-0046: "stuck" annotation on a RUNNING task (no longer a status). When
  // non-empty, the task is running but blocked on something; the UI surfaces a
  // "Stuck" badge gated on status === 'running' && blocked_reason.
  blocked_reason?: string;
  // v2.8.1 edit-task #278: free-form label set (cleaned + deduped + bounded to
  // ≤16 runes each, ≤10 entries by the backend). The DTO always emits a non-nil
  // array ([] when none) — pmTaskMap normalizes nil→[]. Optional on the type so
  // older/legacy payloads (pre-#278) that omit it are treated as no tags.
  tags?: string[];
  // v2.8.1 #278: RFC3339 timestamp of the last status change; "" when never set.
  status_changed_at?: string;
  // v2.7.1 #245: org-internal display/reference token ("T1234"); hash `id`
  // (task-xxx) stays the stable internal ref. Absent → UI falls back to handle.
  org_ref?: string;
  // T106: the owning plan's id when the task is selected into a plan; absent for
  // a backlog task. The Task detail sidebar shows + links to the plan.
  plan_id?: string;
  version: number;
  created_at: string;
  updated_at: string;
}

// v2.8 #258: a row in the org-scoped cross-project Issues/Tasks aggregation
// (GET /api/orgs/:slug/issues|tasks). The backend enriches project + assignee
// to complete-consumable forms (no frontend resolution). `status` carries the
// raw issue/task domain status string; `org_ref` is "I12"/"T34" (omitted when 0
// → UI falls back to the id-tail handle). Issues are not assignable in the pm
// domain (only created_by), so `assignee` is always null for issue rows; only
// task rows carry an enriched assignee.
export interface OrgWorkItemRef {
  /** prefixed identity ref (agent:/user:) — complete-consumable. */
  ref: string;
  /** chrome display name. */
  display_name: string;
  /** member-id for hover (#192 id-as-content). */
  member_id: string;
  // v2.8 #270/#272 (#184): the assignee's agent lifecycle, so the UI can show a
  // "(archived)" chip on a soft-archived agent (the history ref is preserved).
  // Generic string (PD pick) — only agent: refs carry it; user: refs → "".
  // Optional → older payloads omit it (no chip).
  assignee_lifecycle?: string;
}
export interface OrgWorkItem {
  id: string;
  org_ref?: string;
  project: { id: string; name: string };
  title: string;
  status: string;
  assignee: OrgWorkItemRef | null;
  // pm domain has no priority field → always null today (DTO keeps the slot for
  // forward-compat); the UI does not render it.
  priority?: string | null;
  updated_at: string;
  created_at: string;
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
  // v2.9.1 Threads: when set, the message is a REPLY in this root message's
  // thread (POST /conversations/{id}/messages body carries parent_message_id).
  // Absent → a normal top-level message. Mirrors the BE sendMessageReq add.
  parent_message_id?: string;
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
