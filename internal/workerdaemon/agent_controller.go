// Package workerdaemon: AgentController is the v2.7 control-command executor. It
// drives a per-agent SupervisorSession (D2-f s3b-1) — the persistent supervisor
// that SOLELY owns claude — in response to declarative control commands pulled by
// the ControlLoop, and reports RESULT feedback to the center via the D2-c-i
// /admin/environment/agent/* endpoints (the feedbackReporter seam).
//
// It implements CommandHandler (control_loop.go). It is PURELY ADDITIVE and stays
// DORMANT until the D2-f cutover: the control loop only runs when
// RuntimeConfig.ControlClient != nil (set by --use-control-loop). Until activated,
// the daemon's observable behaviour is unchanged.
//
// Command dispatch (see Handle):
//   - "agent.reconcile" → reconcile the real process to the desired lifecycle
//     (start / stop / reset), keyed by a monotonic version for replay safety.
//   - "agent.work"      → inject the work brief into the running session +
//     report the WorkItem active.
//   - "agent.wake"      → inject a posted task message + report the WorkItem active.
//   - unknown           → log + return nil (never wedge the ack cursor).
//
// Idempotency: returning nil from Handle advances the cumulative ack cursor;
// returning an error keeps the command un-acked so the ControlLoop re-pulls it
// next tick. The controller therefore returns nil for "already applied" replays
// (no-op) and reserves errors for genuinely transient failures it WANTS retried.
//
// OWNERSHIP (s3b-2b, PM-pinned): the controller NEVER execs claude. Every session
// is started via the injected sessionStarter, which in PRODUCTION is the real
// supervisor-spawn adapter (claude's parent is the supervisor, never the daemon).
// The agentSession interface is a TEST SEAM only: controller LOGIC is unit-tested
// with a lightweight fake that lives ONLY in _test.go and never appears in a
// production path. grep-clean = no direct claude exec on any production path.
package workerdaemon

import (
	"github.com/oopslink/agent-center/internal/agent"
)

// Command types (mirror the projector constants — kept local so the controller
// does not import the Environment/PM service packages).
const (
	cmdTypeAgentReconcile = "agent.reconcile"
	cmdTypeAgentWork      = "agent.work"
	cmdTypeAgentWake      = "agent.wake"
	cmdTypeAgentConverse  = "agent.converse"       // v2.7 #185: DM/channel message → inject (no WorkItem)
	cmdTypeWorkAvailable  = "agent.work_available" // v2.8.1 #278 D pull-model WAKE (PR2 emit / PR3 handle)
)

// mcpServerName is the `mcpServers` map key for the per-agent worker mcp-host
// server in the generated --mcp-config document.
const mcpServerName = "agent-center"

// reconcilePayload decodes an "agent.reconcile" command payload. Matches
// internal/environment/service/agent_control_projector.go reconcileCommandPayload.
type reconcilePayload struct {
	AgentID          string `json:"agent_id"`
	DesiredLifecycle string `json:"desired_lifecycle"`
	Model            string `json:"model,omitempty"`
	// DisplayName is the agent's human-readable display_name, carried the SAME way as
	// Model from the lifecycle event so the supervisor injects it as
	// GIT_{AUTHOR,COMMITTER}_NAME via the ② AgentEnv seam (T469). Empty → ULID fallback.
	DisplayName string `json:"display_name,omitempty"`
	// EnvVars is the persisted per-agent profile env overlay applied to agent CLI
	// processes (supervisor-owned claude, codex turns, and forked executors).
	EnvVars map[string]string `json:"env_vars,omitempty"`
	// CLI selects the per-CLI session starter ("codex" → CodexSession; empty /
	// "claude-code" → the claude supervisor path).
	CLI string `json:"cli,omitempty"`
	// T236 LLM tuning — transported all the way from the persisted agent profile
	// to this reconcile handler (modeled + persisted + carried through the control
	// loop). Model/CLI are applied at spawn today; reasoning/mode/provider are
	// reserved here for the spawn wiring (the supervisor→claude exec flags), which
	// lands as the CLI adapter gains flag support. Empty = runtime default.
	Reasoning string `json:"reasoning,omitempty"`
	Mode      string `json:"mode,omitempty"`
	Provider  string `json:"provider,omitempty"`
	// F3 model routing (design §5 & §10) — transported from the persisted agent
	// profile through the control loop to the daemon. The modelrouter package
	// consumes these at executor-spawn time. Empty/zero = center default.
	OrchestratorModel    string                  `json:"orchestrator_model,omitempty"`
	DefaultExecutorModel string                  `json:"default_executor_model,omitempty"`
	MaxConcurrentTasks   int                     `json:"max_concurrent_tasks,omitempty"`
	AllowedModels        []string                `json:"allowed_models,omitempty"`
	AllowedExecutors     []agent.ExecutorProfile `json:"allowed_executors,omitempty"` // v2.18.1 BE-1: authoritative {cli,model} candidates (opt-in gate reads this)
	JudgeEnabled         bool                    `json:"judge_enabled,omitempty"`     // T950 ②: per-agent judge opt-in (default OFF)
	// PromptDescription is the already-gated description text to inject into the
	// agent's system prompt (T728), carried the SAME way as DisplayName. Empty ⇒ no
	// injection. Threaded to the supervisor's --prompt-description at spawn.
	PromptDescription string `json:"prompt_description,omitempty"`
	Version           int    `json:"version"`
	ResetScope        string `json:"reset_scope,omitempty"`
}

// workPayload decodes an "agent.work" command payload. Matches
// internal/projectmanager/service/work_item_projector.go workCommandPayload.
type workPayload struct {
	AgentID string `json:"agent_id"`
	TaskID  string `json:"task_id"`
	TaskRef string `json:"task_ref"`
	Brief   string `json:"brief"`
}

// wakePayload decodes an "agent.wake" command payload. Matches
// internal/environment/service/wake_projector.go wakeCommandPayload.
//
// D2-e-ii: ConversationID is carried so the controller can advance the agent's
// read-state cursor (ReportMarkSeen) after a successful inject — both for the
// e-i immediate wake (single message) and the e-ii batch flush. MessageID is
// the NEWEST delivered message id (the cursor target); MessageText is the merged
// batch text in the e-ii path (single message in the e-i path).
type wakePayload struct {
	AgentID        string `json:"agent_id"`
	TaskID         string `json:"task_id"`
	TaskRef        string `json:"task_ref"`
	ConversationID string `json:"conversation_id"`
	MessageID      string `json:"message_id"`
	MessageText    string `json:"message_text"`
	// RootMessageID (F4): thread root of the triggering message (empty if top-level).
	RootMessageID string `json:"root_message_id,omitempty"`
}

// workAvailablePayload decodes an "agent.work_available" (wake) command. Matches
// the projectors' workAvailablePayload (pm WorkItemProjector + env
// AgentControlProjector). v2.8.1 #278 D pull model: a per-agent "you have new
// work — pull your queue" signal. PR3 only DEDUPS (per work_item_id, mirroring
// wake message dedup) + logs + acks — the actual session inject ("check your
// queue") + the agent's pull-loop land together in PR4. WorkItemID is the
// per-WI idempotency/dedup key.
type workAvailablePayload struct {
	AgentID string `json:"agent_id"`
	TaskID  string `json:"task_id"`
}

// conversePayload decodes an "agent.converse" command (v2.7 #185). Mirrors
// environment/service.converseCommandPayload. NON-WorkItem: a DM/channel message
// injected into the running session so the agent replies via the post_message
// MCP tool (conversation_id is the reply target + the read-state cursor).
type conversePayload struct {
	AgentID        string `json:"agent_id"`
	ConversationID string `json:"conversation_id"`
	ConvKind       string `json:"conv_kind"`
	ConvName       string `json:"conv_name"`
	SenderRef      string `json:"sender_ref"`
	SenderDisplay  string `json:"sender_display"`
	MessageID      string `json:"message_id"`
	MessageText    string `json:"message_text"`
	// RootMessageID (F4): thread root of the triggering message (empty if top-level).
	// When set, the agent was @mentioned INSIDE a thread → its reply must land in the
	// same thread (the brief tells it to pass parent_message_id).
	RootMessageID string `json:"root_message_id,omitempty"`
	// AttachmentCount (v2.10.0 [T74]): how many attachments the triggering message
	// carries. >0 → the brief tells the agent a human sent file(s) (e.g. a
	// screenshot) and to call get_my_unread → download_file to view them.
	AttachmentCount int `json:"attachment_count,omitempty"`
	// OwnerRef (T250/T254): the source conversation's owner_ref. For a pm:// owner
	// chat it is pm://plans|issues|tasks|projects/{id}; buildConverseBrief resolves
	// it through the OwnerContext table (internal/conversation) to tell the agent
	// WHICH object the message belongs to (with ConvName carrying the env-resolved
	// name/title) so it can disambiguate "this {kind}" across concurrent chats.
	// Empty for DM; id://organizations/{org} for a channel (not id-anchored).
	OwnerRef string `json:"owner_ref,omitempty"`
}
