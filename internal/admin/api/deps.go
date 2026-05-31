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
	"github.com/oopslink/agent-center/internal/conversation"
	convservice "github.com/oopslink/agent-center/internal/conversation/service"
	"github.com/oopslink/agent-center/internal/discussion"
	disservice "github.com/oopslink/agent-center/internal/discussion/service"
	envservice "github.com/oopslink/agent-center/internal/environment/service"
	filesservice "github.com/oopslink/agent-center/internal/files/service"
	"github.com/oopslink/agent-center/internal/observability"
	"github.com/oopslink/agent-center/internal/observability/query"
	"github.com/oopslink/agent-center/internal/outbox"
	pmservice "github.com/oopslink/agent-center/internal/projectmanager/service"
	"github.com/oopslink/agent-center/internal/secretmgmt"
	secretservice "github.com/oopslink/agent-center/internal/secretmgmt/service"
	"github.com/oopslink/agent-center/internal/taskruntime/dispatch"
	"github.com/oopslink/agent-center/internal/taskruntime/execution"
	"github.com/oopslink/agent-center/internal/taskruntime/inputrequest"
	"github.com/oopslink/agent-center/internal/taskruntime/kill"
	trservice "github.com/oopslink/agent-center/internal/taskruntime/service"
	"github.com/oopslink/agent-center/internal/taskruntime/task"
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

	// Workforce BC
	WorkerRepo        workforce.WorkerRepository
	MappingRepo       workforce.WorkerProjectMappingRepository
	ProposalRepo      workforce.WorkerProjectProposalRepository
	ProjectRepo       workforce.ProjectRepository
	AgentInstanceRepo workforce.AgentInstanceRepository
	EnrollSvc         *wfservice.WorkerEnrollService
	DiscoverySvc      *wfservice.ProjectDiscoveryService
	AcceptanceSvc     *wfservice.ProposalAcceptanceService
	ProjectSvc        *wfservice.ProjectCRUDService
	AgentMgmtSvc      *wfservice.AgentInstanceManagementService

	// Environment BC (v2.7 D1, ADR-0050, task #102) — worker-initiated
	// control channel riding this same admin API + bearer auth. WorkerRepo
	// (above) supplies org provenance on connect.
	EnvControlSvc *envservice.EnvControl

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
	// AgentWorkItemRepo is the raw Agent WorkItem repository (v2.7 D2-b2).
	// The agent-tools write surface (request_input) needs Update + WaitInput
	// composed inside the SAME outer tx as the conversation AddMessage so the
	// pair is atomic; the AppService only exposes a read-only ListWorkItems,
	// and the scope checks (agent owns a WorkItem for the task) read from it.
	AgentWorkItemRepo agent.WorkItemRepository
	// AgentActivityRepo is the append-only AgentActivityEvent repository (v2.7
	// D2-c-i). The controller→center feedback /admin/environment/agent/activity
	// endpoint asserts via repo in tests; the handler appends through the
	// AgentSvc AppService.
	AgentActivityRepo agent.ActivityEventRepository

	// OutboxRepo is the cross-BC outbox emitter (v2.7 D2-e-ii). request_input
	// uses it to emit `agent.awaiting_input` IN THE SAME outer tx as the
	// AddMessage + WaitInput, so the batch-flush trigger commits atomically with
	// the agent entering waiting_input. nil-tolerant (like MessageWriter's
	// optional outbox): a nil repo skips the emit, keeping existing tests green.
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

	// TaskRuntime BC
	TaskRepo        task.Repository
	ExecRepo        execution.Repository
	IRRepo          inputrequest.Repository
	ArtifactRepo    execution.ArtifactRepository
	TaskSvc         *trservice.TaskService
	IRSvc           *trservice.InputRequestService
	ArtifactSvc     *trservice.ArtifactService
	ExecSvc         *trservice.ExecutionService
	DispatchSvc     *dispatch.Service
	KillCoordinator *kill.Coordinator

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

	// Discussion BC
	IssueRepo                discussion.IssueRepository
	IssueLifecycleSvc        *disservice.IssueLifecycleService
	IssueCommentSvc          *disservice.IssueCommentService
	IssueBindConversationSvc *disservice.IssueBindConversationService
	IssueLinkConversationSvc *disservice.IssueLinkConversationService

	// Observability BC
	EventRepo observability.EventRepository
	QuerySvc  *query.Service
	FleetSvc  *query.FleetSnapshotService
	StatsSvc  *query.StatsService
	LogsSvc   *query.LogsService
	BlobStore blobstore.BlobStore
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
