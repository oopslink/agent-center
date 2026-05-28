package api

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"io"
	"net/http"

	admintokensvc "github.com/oopslink/agent-center/internal/admintoken/service"
	"github.com/oopslink/agent-center/internal/blobstore"
	"github.com/oopslink/agent-center/internal/conversation"
	convservice "github.com/oopslink/agent-center/internal/conversation/service"
	"github.com/oopslink/agent-center/internal/discussion"
	disservice "github.com/oopslink/agent-center/internal/discussion/service"
	"github.com/oopslink/agent-center/internal/observability"
	"github.com/oopslink/agent-center/internal/observability/query"
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
	ConvRepo            conversation.ConversationRepository
	MsgRepo             conversation.MessageRepository
	ConvRefRepo         conversation.ConversationMessageReferenceRepository
	ReadStateRepo       conversation.UserConversationReadStateRepository
	MessageWriter       *convservice.MessageWriter
	ChannelMgmtSvc      *convservice.ChannelManagementService
	ParticipantMgmtSvc  *convservice.ParticipantManagementService
	CarryOverSvc        *convservice.CarryOverService
	DerivationSvc       *convservice.MessageDerivationService
	ReadStateSvc        *convservice.ReadStateService

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
	UserSecretRepo       secretmgmt.UserSecretRepository
	UserSecretSvc        *secretservice.UserSecretService
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
