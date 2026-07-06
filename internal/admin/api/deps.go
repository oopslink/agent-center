package api

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"io"
	"net/http"

	admintokensvc "github.com/oopslink/agent-center/internal/admintoken/service"
	"github.com/oopslink/agent-center/internal/agent"
	agentservice "github.com/oopslink/agent-center/internal/agent/service"
	"github.com/oopslink/agent-center/internal/blobstore"
	coderepservice "github.com/oopslink/agent-center/internal/coderepo/service"
	cogservice "github.com/oopslink/agent-center/internal/cognition/reminder/service"
	"github.com/oopslink/agent-center/internal/concurrency"
	"github.com/oopslink/agent-center/internal/conversation"
	convservice "github.com/oopslink/agent-center/internal/conversation/service"
	"github.com/oopslink/agent-center/internal/environment/controlstream"
	envservice "github.com/oopslink/agent-center/internal/environment/service"
	filesservice "github.com/oopslink/agent-center/internal/files/service"
	"github.com/oopslink/agent-center/internal/identity"
	"github.com/oopslink/agent-center/internal/observability"
	"github.com/oopslink/agent-center/internal/observability/query"
	"github.com/oopslink/agent-center/internal/outbox"
	projectmanager "github.com/oopslink/agent-center/internal/projectmanager"
	orch "github.com/oopslink/agent-center/internal/projectmanager/orchestration"
	pmservice "github.com/oopslink/agent-center/internal/projectmanager/service"
	"github.com/oopslink/agent-center/internal/runtimefs"
	"github.com/oopslink/agent-center/internal/secretmgmt"
	secretservice "github.com/oopslink/agent-center/internal/secretmgmt/service"
	"github.com/oopslink/agent-center/internal/usage"
	"github.com/oopslink/agent-center/internal/workforce"
	wfservice "github.com/oopslink/agent-center/internal/workforce/service"
)

// HandlerDeps is the dep bag for admin endpoint handlers.
//
// Same shape conceptually as webconsole/api.HandlerDeps but extended
// to cover the full CLI surface (79 unique AppService methods across
// 18 CLI handler files). We keep it independent from webconsole so
// changes to either transport don't ripple into the other.
type HandlerDeps struct {
	Actor observability.Actor

	// DB is the raw connection pool used by composite endpoints that
	// must wrap two AppService calls in the SAME tx (ADR-0014 § 2 —
	// dispatch/kill + DecisionRecord atomicity, v2.3-2). Nested
	// persistence.RunInTx calls reuse the outer tx; lower-level
	// services don't need to know whether they are nested.
	DB *sql.DB

	// Conversation BC
	ConvRepo           conversation.ConversationRepository
	MsgRepo            conversation.MessageRepository
	ConvRefRepo        conversation.ConversationMessageReferenceRepository
	ReadStateRepo      conversation.UserConversationReadStateRepository
	MessageWriter      *convservice.MessageWriter
	ChannelMgmtSvc     *convservice.ChannelManagementService
	ParticipantMgmtSvc *convservice.ParticipantManagementService
	CarryOverSvc       *convservice.CarryOverService
	ReadStateSvc       *convservice.ReadStateService
	// InboxSvc backs the agent get_my_unread tool (v2.8.1 #278 D PR4b dual-stream):
	// the agent's unread messages directed at it (DM-all + channel-@mention),
	// org-scoped, across its conversations. nil → get_my_unread returns 501.
	InboxSvc *convservice.AgentInboxService
	// ReplyNudgeSvc backs the reply-guardrail (T341): given an idle agent, it
	// derives the directed replies it still owes, gates agent-authored ones through
	// the shared wake-guardrail, and returns bounded re-inject nudges. The worker
	// calls /admin/environment/agent/reply-nudges at turn-end + TrueIdle and injects
	// the prompts. nil → the endpoint returns 501 (feature off).
	ReplyNudgeSvc *convservice.ReplyNudgeService

	// Workforce BC
	WorkerRepo workforce.WorkerRepository
	EnrollSvc  *wfservice.WorkerEnrollService
	// WorkerConfigSvc backs the operator per-CLI capability toggle
	// (v2.7 #147 PATCH .../capabilities/{name}/enabled).
	WorkerConfigSvc *wfservice.WorkerConfigService

	// Environment BC (v2.7 D1, ADR-0050, task #102) — worker-initiated
	// control channel riding this same admin API + bearer auth. WorkerRepo
	// (above) supplies org provenance on connect.
	EnvControlSvc *envservice.EnvControl

	// ControlStreamBus is the OPTIONAL center-side SSE down-push bus (v2.7 D5
	// slice-1). When wired, GET /admin/environment/worker/commands/stream
	// subscribes a worker to its own control commands; AppendCommand publishes
	// here best-effort after commit. nil → the stream endpoint returns 501 (the
	// poll endpoint /admin/environment/worker/commands remains the path). The
	// SAME WorkerControlEvent log backs both; the bus is a low-latency push, not
	// a new log.
	ControlStreamBus *controlstream.Bus

	// RuntimeFsDispatcher is the OPTIONAL in-process correlator for the agent
	// runtime file browser (issue-921db054 / I5). The worker POSTs its read reply to
	// /admin/environment/agent/runtime-fs/response; this matches it (by req_id) to the
	// waiting Web Console request. The SAME *runtimefs.Dispatcher instance is shared
	// with the webconsole server (which Registers + awaits). nil → the response
	// endpoint returns 501.
	RuntimeFsDispatcher *runtimefs.Dispatcher

	// Agent BC (v2.7 C3 / D2-b1) — drives the per-agent MCP tool surface
	// (/admin/agent-tools/...). The per-agent auth gate (requireAgentOnWorker)
	// resolves + authorizes the operating agent via this AppService; the
	// handler never touches the DB directly.
	AgentSvc *agentservice.Service
	// AgentRepo is the raw Agent repository (v2.7 D2-f s4). The worker boot-resume
	// endpoint (/admin/environment/worker/resume-state) enumerates this worker's
	// agents via ListByWorker to compute the resumable set; resume-state is a
	// worker-level read (one worker → many agents), so it reads the repo directly
	// (no AppService method fits the one-to-many shape).
	AgentRepo agent.Repository
	// v2.14.0 F7 (issue I14): AgentWorkItemRepo removed — AgentWorkItem retired.
	// The agent-tools own-work scope is now Task.Assignee == agentActor(a) via the
	// PM service; the input-required await is block_task(reason_type=input_required)
	// blocking the Task instead of parking a WorkItem. (I25: the request_input alias
	// route was physically removed — block_task is the single recovery path.)
	// AgentActivityRepo is the append-only AgentActivityEvent repository (v2.7
	// D2-c-i). The controller→center feedback /admin/environment/agent/activity
	// endpoint asserts via repo in tests; the handler appends through the
	// AgentSvc AppService.
	AgentActivityRepo agent.ActivityEventRepository

	// OutboxRepo is the cross-BC outbox emitter (v2.7 D2-e-ii). The MessageWriter
	// uses it to emit `conversation.message_added` IN THE SAME outer tx as the
	// AddMessage, so the conversational-wake trigger commits atomically with the
	// message. nil-tolerant (like MessageWriter's optional outbox): a nil repo
	// skips the emit, keeping existing tests green. (The retired request_input →
	// agent.awaiting_input emit no longer exists; AgentWorkItem was removed in F7.)
	OutboxRepo outbox.Repository

	// Files module (v2.7 post-D3, task #104) — backs the agent file MCP tools
	// (/admin/agent-tools/upload_file, attach_file + /admin/files/...). The same
	// transfer Service the webconsole human transport uses; agent-domain
	// reachability (own-domain scopes) is the authz layer in agent_tools_files.go.
	// nil when the blobstore root is unset → the file endpoints degrade to 501.
	FilesSvc *filesservice.Service

	// ProjectManager BC (v2.7 D2-b2) — backs block_task / complete_task. The
	// agent-tools surface calls BlockTask / CompleteTask with the operating
	// agent as the actor (#5a made the assigned agent a ProjectMember so the
	// pm write-gate passes). These services runInTx internally, so they nest
	// inside the agent-tools outer RunInTx for atomicity with AddMessage.
	PMService *pmservice.Service
	// ReminderSvc is the Cognition Reminder application service backing the
	// create/list/get/update_reminder agent tools + the admin reminders API
	// (T206). Nil until wired (handlers then return reminder_not_wired).
	ReminderSvc *cogservice.ReminderAppService
	// CodeRepoSvc is the workspace CodeRepo app service (v2.18.4 BE-2) backing the
	// agent MCP repo tools (list_project_repos / get_repo_info live). The repo_id →
	// workspace Repo resolution + remote viewing go through it; it NEVER returns the
	// credential. nil → get_repo_info(live) degrades to static info only.
	CodeRepoSvc *coderepservice.Service
	// LiveState records the per-agent live executor snapshots the worker ships on its
	// heartbeat (v2.19.0). The heartbeat handler writes it; the webconsole
	// .../agents/{id}/concurrency endpoint reads the SAME store instance. nil →
	// snapshots are dropped (feature off / not wired).
	LiveState concurrency.LiveStateStore
	// OrchService is the orchestration engine application service (P2-T2) backing
	// the graph/node/edge agent MCP tools. nil when not wired (handlers then
	// return orchestration_not_wired 501).
	OrchService *orch.Service
	// TemplateRepo backs the list_templates / get_template agent tools.
	// nil when not wired (handlers return templates_not_wired 501).
	TemplateRepo projectmanager.TemplateRepository
	// ModelCatalogRepo backs the *_model_catalog_entry / import_model_catalog agent
	// tools (issue-93dd8daa ①). nil when not wired (handlers return
	// model_catalog_not_wired 501).
	ModelCatalogRepo projectmanager.ModelCatalogRepository
	// PMProjectRepo is the new-model (pm) project repo backing the
	// operator/admin-token project find-* read endpoints. v2.7 #131 PR-3:
	// repointed off the retired workforce.Project model. Operator-scoped —
	// projectFindAllHandler uses its operator-global ListAll.
	PMProjectRepo projectmanager.ProjectRepository

	// IdentityOrgRepo resolves the org name for the get_my_profile agent tool
	// (v2.7.1 #239) — read-only GetByID(org_id). Same repo the webconsole uses.
	IdentityOrgRepo identity.OrganizationRepository

	// DisplayNameResolver (T460 ③) resolves an identity ref ("agent:<id>"/"user:<id>")
	// to its display_name, used by the post_message unresolved-mention report so a
	// valid HUMAN @mention of a conversation participant is not falsely flagged as
	// unresolved. Optional; nil → only the org AGENT roster resolves names (a human
	// participant mention may then be reported unresolved). Mirrors the WakeProjector's
	// DisplayName resolver, wired from the same IdentityRepo.
	DisplayNameResolver func(ctx context.Context, identityRef string) (string, bool)

	// SecretManagement BC
	UserSecretRepo secretmgmt.UserSecretRepository
	UserSecretSvc  *secretservice.UserSecretService
	// UserSecretResolveSvc returns plaintext for `secret:resolve`-scoped
	// callers (worker daemon agent dispatch). v2.3-3b (task #29).
	UserSecretResolveSvc *secretservice.SecretResolutionService

	// AdminToken BC (v2.3-3a task #28) — drives the
	// /admin/admintoken/{create,list,revoke} surface used by the admin
	// CLI. The middleware Verifier is the SAME *Service; we wire it as
	// a concrete pointer here so handlers can reach FindAll / Create /
	// Revoke too (the Verifier interface only exposes verify/mark).
	AdminTokenSvc *admintokensvc.Service

	// Observability BC
	EventRepo observability.EventRepository
	QuerySvc  *query.Service
	FleetSvc  *query.FleetSnapshotService
	StatsSvc  *query.StatsService
	LogsSvc   *query.LogsService
	BlobStore blobstore.BlobStore

	// Usage BC (v2.15.0 I28/F2): the report_usage agent-tool materializes cost
	// (ModelPriceRepo → PriceBook) and persists raw events (UsageEventRepo).
	UsageEventRepo usage.UsageEventRepository
	ModelPriceRepo usage.ModelPriceRepository
}

type depsKey struct{}

// hd retrieves the typed dep bag from request context.
func hd(r *http.Request) HandlerDeps {
	v, _ := r.Context().Value(depsKey{}).(HandlerDeps)
	return v
}

// WithDeps installs the dep bag into the request context. Use as
// middleware around all handlers.
func WithDeps(deps HandlerDeps) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			ctx := context.WithValue(r.Context(), depsKey{}, deps)
			next.ServeHTTP(w, r.WithContext(ctx))
		})
	}
}

// =============================================================================
// JSON helpers (parallel to webconsole/api; intentionally duplicated to keep
// the two transport layers independent)
// =============================================================================

func writeJSON(w http.ResponseWriter, status int, body any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(body)
}

func writeError(w http.ResponseWriter, status int, code, message string) {
	writeJSON(w, status, map[string]any{
		"error":   code,
		"message": message,
	})
}

func decodeJSON(r *http.Request, dst any) error {
	if r.Body == nil {
		return errors.New("empty body")
	}
	defer r.Body.Close()
	body, err := io.ReadAll(r.Body)
	if err != nil {
		return err
	}
	if len(body) == 0 {
		return nil
	}
	return json.Unmarshal(body, dst)
}
