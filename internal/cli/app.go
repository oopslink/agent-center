package cli

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/oopslink/agent-center/internal/admintoken"
	admintokensvc "github.com/oopslink/agent-center/internal/admintoken/service"
	admintokensqlite "github.com/oopslink/agent-center/internal/admintoken/sqlite"
	agentpkg "github.com/oopslink/agent-center/internal/agent"
	agentsvc "github.com/oopslink/agent-center/internal/agent/service"
	agentsql "github.com/oopslink/agent-center/internal/agent/sqlite"
	"github.com/oopslink/agent-center/internal/blobstore"
	"github.com/oopslink/agent-center/internal/clock"
	coderepprovider "github.com/oopslink/agent-center/internal/coderepo/provider"
	coderepservice "github.com/oopslink/agent-center/internal/coderepo/service"
	coderepsql "github.com/oopslink/agent-center/internal/coderepo/sqlite"
	"github.com/oopslink/agent-center/internal/cognition/wakeguard"
	"github.com/oopslink/agent-center/internal/concurrency"
	"github.com/oopslink/agent-center/internal/config"
	"github.com/oopslink/agent-center/internal/conversation"
	"github.com/oopslink/agent-center/internal/conversation/replyguard"
	convservice "github.com/oopslink/agent-center/internal/conversation/service"
	convsqlite "github.com/oopslink/agent-center/internal/conversation/sqlite"
	"github.com/oopslink/agent-center/internal/environment/controlstream"
	envservice "github.com/oopslink/agent-center/internal/environment/service"
	envsql "github.com/oopslink/agent-center/internal/environment/sqlite"
	"github.com/oopslink/agent-center/internal/identity"
	"github.com/oopslink/agent-center/internal/idgen"
	"github.com/oopslink/agent-center/internal/observability"
	"github.com/oopslink/agent-center/internal/observability/query"
	obsqlite "github.com/oopslink/agent-center/internal/observability/sqlite"
	"github.com/oopslink/agent-center/internal/outbox"
	outboxsql "github.com/oopslink/agent-center/internal/outbox/sqlite"
	"github.com/oopslink/agent-center/internal/persistence"
	pm "github.com/oopslink/agent-center/internal/projectmanager"
	"github.com/oopslink/agent-center/internal/projectmanager/gatecheck"
	pmservice "github.com/oopslink/agent-center/internal/projectmanager/service"
	pmsql "github.com/oopslink/agent-center/internal/projectmanager/sqlite"
	"github.com/oopslink/agent-center/internal/runtimefs"
	"github.com/oopslink/agent-center/internal/secretmgmt"
	secretservice "github.com/oopslink/agent-center/internal/secretmgmt/service"
	secretsqlite "github.com/oopslink/agent-center/internal/secretmgmt/sqlite"
	settingssql "github.com/oopslink/agent-center/internal/settings/sqlite"
	usagesql "github.com/oopslink/agent-center/internal/usage/sqlite"
	"github.com/oopslink/agent-center/internal/workforce"
	wfservice "github.com/oopslink/agent-center/internal/workforce/service"
	wfsqlite "github.com/oopslink/agent-center/internal/workforce/sqlite"
)

// App carries everything CLI handlers need.
//
// Two construction modes (v2.2 Phase B per conventions § 0.4):
//
//   - **Server mode**: NewApp opens the DB and wires every Service / Repo
//     field. Used by `agent-center server` (handlers_system.go) which IS
//     the process that owns the DB and serves the admin endpoint.
//   - **CLI mode**: NewClientApp builds a lightweight App with only
//     Config + Clock + Client populated; every Service / Repo field is
//     nil. CLI handlers go through Client to talk to a running server
//     (the only legitimate AppService entry point per § 0.4).
//
// Handlers that need to run in BOTH modes (rare; mostly only server
// boot + schema migrations) must consult whichever fields the active
// mode populates. The pattern across handlers_*.go (post-Phase B
// migration) is: prefer `a.Client.<Method>` over `a.<Svc>.<Method>`.
type App struct {
	Config config.Config

	// Client is the admin transport. Populated in CLI mode; may be nil
	// in server mode (the server doesn't dial itself).
	Client *Client

	// DB / Clock / IDGen / Service / Repo fields below are wired only
	// in server mode (NewApp). CLI mode (NewClientApp) leaves them nil.
	DB    *sql.DB
	Clock clock.Clock
	IDGen idgen.Generator

	WorkerRepo workforce.WorkerRepository
	// PMProjectRepo is the new-model (pm) project repo used by the
	// operator-scoped CLI project READ handlers (list/show). v2.7 #131
	// PR-3 — the LOCAL list path uses its operator-global ListAll.
	PMProjectRepo pm.ProjectRepository
	ConvRepo      conversation.ConversationRepository
	MsgRepo       conversation.MessageRepository
	EventRepo     *obsqlite.EventRepo
	Sink          *observability.EventSink

	// Usage BC (v2.15.0 I28/F2): usage_events + model_prices repos backing the
	// report_usage agent-tool.
	UsageEventRepo *usagesql.UsageEventRepo
	ModelPriceRepo *usagesql.ModelPriceRepo

	EnrollSvc *wfservice.WorkerEnrollService
	// WorkerConfigSvc backs the operator per-CLI capability toggle (v2.7 #147).
	WorkerConfigSvc *wfservice.WorkerConfigService

	// v2.7 ProjectManager BC AppService facade (ADR-0046) — backs the nested
	// /api/projects/{project_id}/... routes + produces the outbox events the
	// server-runtime relay projects into Conversation/Agent.
	PMService *pmservice.Service

	// CodeRepoService is the v2.18.4 BE-1 workspace CodeRepo AppService (issue-f980c8de)
	// — workspace Repos CRUD + encrypted credential storage + the merge-check resolver.
	CodeRepoService *coderepservice.Service

	// LiveState is the per-agent live executor snapshot store (v2.19.0): the SAME
	// instance is wired into the admin heartbeat handler (writer) and the webconsole
	// .../agents/{id}/concurrency endpoint (reader).
	LiveState concurrency.LiveStateStore

	// AgentService is the v2.7 Agent BC AppService facade (C3).
	AgentService *agentsvc.Service

	// AgentRepo is the raw Agent repository (v2.7 D2-f s4). The worker boot-resume
	// admin endpoint enumerates a worker's agents (ListByWorker) — a worker-level
	// read with no fitting AppService method, so the repo is exposed directly.
	AgentRepo agentpkg.Repository

	// v2.14.0 F7 (issue I14): AgentWorkItemRepo removed — AgentWorkItem retired.

	// AgentActivityRepo is the append-only Agent activity-event repository (C2).
	// The admin controller→center feedback surface (v2.7 D2-c-i activity sink)
	// reads it back in tests; writes go through AgentService.AppendActivity.
	AgentActivityRepo agentpkg.ActivityEventRepository

	// EnvControlSvc is the v2.7 Environment BC control-channel AppService
	// (D1, ADR-0050, task #102) — backs the additive /admin/environment/...
	// worker control endpoints.
	EnvControlSvc *envservice.EnvControl

	// ControlStreamBus is the v2.7 D5 slice-1 center-side SSE down-push bus. A
	// single shared instance: the projector's ControlLog publishes appended
	// commands here (after commit, best-effort), and the
	// /admin/environment/worker/commands/stream endpoint subscribes workers to
	// it. Same WorkerControlEvent log backs both push + poll.
	ControlStreamBus *controlstream.Bus

	// RuntimeFsDispatcher is the I5 (issue-921db054) agent-runtime-browser correlator
	// — ONE shared instance: the webconsole runtime endpoints Register+await a req_id
	// here, and the admin /admin/environment/agent/runtime-fs/response endpoint
	// Resolves the worker's reply against it. Both servers must hold the SAME pointer.
	RuntimeFsDispatcher *runtimefs.Dispatcher

	MessageWriter      *convservice.MessageWriter
	ChannelMgmtSvc     *convservice.ChannelManagementService
	ParticipantMgmtSvc *convservice.ParticipantManagementService
	CarryOverSvc       *convservice.CarryOverService
	ConvRefRepo        conversation.ConversationMessageReferenceRepository
	ReadStateRepo      conversation.UserConversationReadStateRepository
	ReadStateSvc       *convservice.ReadStateService
	InboxSvc           *convservice.AgentInboxService
	FollowStateRepo    conversation.UserConversationFollowStateRepository
	FollowStateSvc     *convservice.FollowStateService

	// WakeGuard is the ONE process-singleton wake-chain circuit breaker (I7-D1).
	// It holds the rate/cycle/depth anti-storm runtime state shared across ALL
	// agent→agent down-pushes. Hoisted onto App (formerly a webconsole-wiring
	// local) so the reply-guardrail (T341) gates agent-authored reply nudges
	// through the SAME instance as wake delivery — one shared budget, no
	// ungoverned ping-pong. Config is resolved LIVE from center settings.
	WakeGuard *wakeguard.Guard
	// ReplyNudgeSvc is the server-side reply-guardrail (T341, 方案 A). At
	// turn-end + TrueIdle the worker asks it which directed replies an idle agent
	// still owes; it derives them from the message log + read-state, gates
	// agent-authored ones through WakeGuard, and returns bounded re-inject prompts.
	ReplyNudgeSvc *convservice.ReplyNudgeService

	// OutboxRepo is the cross-BC outbox emitter (v2.7 D2-e-ii). The MessageWriter
	// uses it to emit `conversation.message_added` in the same tx as the message
	// append (the conversational-wake trigger). (The retired request_input →
	// agent.awaiting_input emit no longer exists; AgentWorkItem was removed in F7.)
	OutboxRepo outbox.Repository

	// SecretManagement (P11 § 3.7b)
	UserSecretRepo secretmgmt.UserSecretRepository
	UserSecretSvc  *secretservice.UserSecretService
	// UserSecretResolveSvc gates plaintext via SecretResolutionService.
	// v2.3-3b (task #29): wired alongside UserSecretSvc when master key is
	// loaded, so the admin endpoint can serve secret:resolve to worker
	// daemons that hold a `secret:resolve`-scoped admin token.
	UserSecretResolveSvc *secretservice.SecretResolutionService

	// AdminToken (v2.3-3a task #28) — bearer tokens that gate the admin
	// endpoint. Server mode wires both fields; CLI mode (NewClientApp)
	// leaves them nil — the CLI talks to a server that already has them.
	AdminTokenRepo admintoken.Repository
	AdminTokenSvc  *admintokensvc.Service

	// v2.6: Identity BC services.
	IdentitySignupSvc           *identity.SignupService
	IdentitySigninSvc           *identity.SigninService
	IdentitySignoutSvc          *identity.SignoutService
	IdentityAuthSvc             *identity.AuthService
	IdentityPasscodeChangeSvc   *identity.PasscodeChangeService
	IdentityRepo                identity.IdentityRepository
	IdentityOrgRepo             identity.OrganizationRepository
	IdentityOrgCreateSvc        *identity.OrganizationCreateService
	IdentityOrgLifecycleSvc     *identity.OrganizationLifecycleService
	IdentityMemberRepo          identity.MemberRepository
	IdentityMemberAddSvc        *identity.MemberAddService
	IdentityMemberCreateUserSvc *identity.MemberCreateUserService
	IdentityMemberRoleChangeSvc *identity.MemberRoleChangeService
	IdentityMemberDisableSvc    *identity.MemberDisableService
	IdentityMemberRemoveSvc     *identity.MemberRemoveService
	IdentityAgentProvisionSvc   *identity.AgentIdentityProvisionService
	IdentityOrgUpdateSvc        *identity.OrganizationUpdateService
	IdentityInvitationRepo      identity.InvitationRepository

	// Observability Phase 4
	QuerySvc  *query.Service
	FleetSvc  *query.FleetSnapshotService
	StatsSvc  *query.StatsService
	LogsSvc   *query.LogsService
	BlobStore blobstore.BlobStore
}

// NewApp wires the full dependency graph from a Config. The DB must
// already be open + migrated.
func NewApp(cfg config.Config, db *sql.DB, clk clock.Clock) (*App, error) {
	if db == nil {
		return nil, errors.New("cli: NewApp requires non-nil db")
	}
	if clk == nil {
		clk = clock.SystemClock{}
	}
	gen := idgen.NewGenerator(clk)
	er, err := obsqlite.NewEventRepo(context.Background(), db)
	if err != nil {
		return nil, fmt.Errorf("event repo: %w", err)
	}
	sink := observability.NewEventSink(er, er, gen, clk)
	wr := wfsqlite.NewWorkerRepo(db)
	// pm (new-model) project repo for the operator-scoped CLI project READ
	// handlers (list/show). v2.7 #131 PR-3.
	pmProjRepo := pmsql.NewProjectRepo(db)
	cr := convsqlite.NewConversationRepo(db)
	mgRepo := convsqlite.NewMessageRepo(db)

	enroll := wfservice.NewWorkerEnrollService(db, wr, sink, clk)
	workerConfig := wfservice.NewWorkerConfigService(db, wr, sink, clk)
	// v2.7 D2-e-i (OQ5): attach the cross-BC outbox emitter so AddMessage emits a
	// conversation.message_added wake-trigger event (same tx) for task-owned
	// conversations. The WakeProjector (webconsole_wiring) consumes it.
	outboxRepo := outboxsql.NewOutboxRepo(db)
	writer := convservice.NewMessageWriter(db, cr, mgRepo, sink, gen, clk).WithOutbox(outboxRepo)
	channelMgmt := convservice.NewChannelManagementService(db, cr, sink, gen, clk)
	participantMgmt := convservice.NewParticipantManagementService(db, cr, sink, clk)
	convRefRepo := convsqlite.NewReferenceRepo(db)
	carryOver := convservice.NewCarryOverService(db, cr, mgRepo, convRefRepo, sink, gen, clk)
	readStateRepo := convsqlite.NewReadStateRepo(db)
	readStateSvc := convservice.NewReadStateService(db, readStateRepo, mgRepo, sink, clk)
	inboxSvc := convservice.NewAgentInboxService(db, cr, readStateRepo)
	followStateRepo := convsqlite.NewFollowStateRepo(db)
	followStateSvc := convservice.NewFollowStateService(followStateRepo, cr, clk)

	// I7-D1/T341: the ONE wake-chain circuit breaker, hoisted here so wake
	// delivery (webconsole wiring) and the reply-guardrail (admin wiring) share a
	// SINGLE instance — same rate/cycle/depth budget across both down-push paths.
	// Config is resolved LIVE from the center settings store on every evaluation
	// (an I7-D3 / T341 §3.5 Settings PUT takes effect without a restart); a read
	// error or absent keys fall back to the conservative DefaultConfig — the guard
	// is never disabled by missing settings.
	guardSettings := settingssql.NewStore(db, clk)
	wakeGuard := wakeguard.NewGuardFunc(func() wakeguard.Config {
		m, err := guardSettings.GetByPrefix(context.Background(), "wake.")
		if err != nil {
			return wakeguard.DefaultConfig()
		}
		return wakeguard.ConfigFromMap(m)
	})
	// T341 reply-guardrail (方案 A): obligation derivation (read-only, no new
	// table) + bounded re-inject. Agent-authored obligations are gated through the
	// shared wakeGuard; thresholds come live from the `reply.` settings prefix.
	replyObligationSvc := convservice.NewReplyObligationService(db, cr, readStateRepo)
	replyNudgeSvc := convservice.NewReplyNudgeService(replyObligationSvc, wakeGuard, func() replyguard.Config {
		m, err := guardSettings.GetByPrefix(context.Background(), "reply.")
		if err != nil {
			return replyguard.DefaultConfig()
		}
		return replyguard.ConfigFromMap(m)
	}, clk)

	// Observability Phase 4
	deps := query.Deps{
		Events:        er,
		Conversations: cr,
		Messages:      mgRepo,
		Workers:       wr,
		// v2.14.0 F7 (issue I14): the WorkItemProjections / WorkItems read deps were
		// removed — AgentWorkItem retired.
		PMTasks:    pmsql.NewTaskRepo(db),
		PMProjects: pmsql.NewProjectRepo(db),
		PMIssues:   pmsql.NewIssueRepo(db),
		// v2.7 #107 Phase-2 proj-A: inspectWorker/queryExecutions worker→agents→work-items (Q3 MAP).
		Agents: agentsql.NewAgentRepo(db),
	}
	querySvc := query.NewService(deps)
	fleetSvc := query.NewFleetSnapshotService(deps)
	statsSvc := query.NewStatsService(deps)
	var bs blobstore.BlobStore
	if cfg.BlobStore.Root != "" {
		if local, err := blobstore.NewLocalDir(cfg.BlobStore.Root); err == nil {
			bs = local
		}
	}
	logsSvc := query.NewLogsService(deps, bs)

	// v2.6: Identity BC repos. Always wired; signin/auth services need the
	// master key so they are wired in the master key block below.
	idIdentityRepo := identity.NewSQLiteIdentityRepo(db)
	idOrgRepo := identity.NewSQLiteOrganizationRepo(db)
	idMemberRepo := identity.NewSQLiteMemberRepo(db)
	identitySignupSvc := identity.NewSignupServiceWithSink(db, idIdentityRepo, idOrgRepo, idMemberRepo, sink)
	identitySignoutSvc := identity.NewSignoutService(sink)
	identityPasscodeChangeSvc := identity.NewPasscodeChangeServiceWithSink(db, idIdentityRepo, sink)
	identityOrgCreateSvc := identity.NewOrganizationCreateServiceWithSink(db, idOrgRepo, idMemberRepo, sink)
	identityOrgLock := identity.NewOrganizationLockManager()
	identityOrgLifecycleSvc := identity.NewOrganizationLifecycleServiceWithSink(db, idOrgRepo, idMemberRepo, identityOrgLock, sink)
	identityMemberAddSvc := identity.NewMemberAddServiceWithSink(db, idIdentityRepo, idMemberRepo, sink)
	identityMemberCreateUserSvc := identity.NewMemberCreateUserServiceWithSink(db, idIdentityRepo, idMemberRepo, sink)
	identityMemberRoleChangeSvc := identity.NewMemberRoleChangeServiceWithSink(db, idMemberRepo, identityOrgLock, sink)
	identityMemberDisableSvc := identity.NewMemberDisableServiceWithSink(db, idMemberRepo, identityOrgLock, sink)
	identityMemberRemoveSvc := identity.NewMemberRemoveServiceWithSink(db, idMemberRepo, identityOrgLock, sink)
	identityAgentProvisionSvc := identity.NewAgentIdentityProvisionServiceWithSink(db, idIdentityRepo, idMemberRepo, sink)
	identityOrgUpdateSvc := identity.NewOrganizationUpdateServiceWithSink(db, idOrgRepo, sink)
	identityInvitationRepo := identity.NewSQLiteInvitationRepo(db)
	var (
		identitySigninSvc *identity.SigninService
		identityAuthSvc   *identity.AuthService
	)

	// P11 § 3.7b: UserSecret management — wired iff master key file is
	// configured. Without master key the CLI handlers refuse with
	// ExitNotImplemented (handler-side gate).
	userSecretRepo := secretsqlite.NewUserSecretRepo(db)
	// v2.3-3a (task #28): AdminToken repo + service always wired in server
	// mode. The bootstrap token writer (admin_bootstrap.go) lives at the
	// server-boot site and writes a fresh `*` token to disk when the table
	// is empty, so the operator can issue scoped tokens via the CLI.
	adminTokenRepo := admintokensqlite.New(db)
	adminTokenSvc := admintokensvc.New(adminTokenRepo, gen, clk)
	var (
		userSecretSvc        *secretservice.UserSecretService
		userSecretResolveSvc *secretservice.SecretResolutionService
		// v2.18.4 BE-1: hoisted so the workspace CodeRepo service can AES-GCM-encrypt
		// repo credentials with the same master key (nil when unconfigured → credential
		// writes fail loudly via ErrMasterKeyNotLoaded).
		masterKey *secretmgmt.MasterKey
	)
	if cfg.SecretManagement.MasterKeyFile != "" {
		mk, err := secretmgmt.LoadMasterKey(cfg.SecretManagement.MasterKeyFile, cfg.SecretManagement.SkipPermsCheck)
		if err != nil {
			return nil, fmt.Errorf("load master key: %w", err)
		}
		masterKey = mk
		userSecretSvc = secretservice.NewUserSecretService(db, userSecretRepo, gen, sink, clk, mk)
		// v2.3-3b (task #29): SecretResolutionService is the only path that
		// returns plaintext. Wired here so the admin endpoint can expose
		// /admin/secret/user-secret/resolve to worker daemons.
		userSecretResolveSvc = secretservice.NewSecretResolutionService(db, userSecretRepo, sink, clk, mk)
		// v2.5-B2: hand the same master key to the AdminToken service so
		// it can AES-GCM-encrypt enroll-token plaintext for the show-
		// install-command flow. Without the master key the show endpoint
		// returns a "not configured" hint instead of leaking nothing.
		adminTokenSvc.WithMasterKey(mk)
		// v2.6: Identity signin + auth services use the master key as the
		// JWT HS256 signing key (ADR-0043 § 6).
		identitySigninSvc = identity.NewSigninServiceWithSink(idIdentityRepo, mk.Bytes(), sink)
		identityAuthSvc = identity.NewAuthService(idIdentityRepo, mk.Bytes())
	}

	// v2.7 ProjectManager AppService facade (ADR-0046/0052). Produces outbox
	// events drained by the server-runtime relay (wired in runWebConsole).
	// Shared agent repo: the Agent BC's AppService owns it, and the pm Service
	// resolves an assignee agent's org through it (#5a, AssignTask→ProjectMember
	// cross-org guard, ADR-0049/0052/OQ6).
	agentRepo := agentsql.NewAgentRepo(db)

	// v2.18.4 BE-1 (issue-f980c8de): the workspace CodeRepo service. The shared
	// CodeRepoRefRepo doubles as the RefUnlinker (clears project refs on Repo delete);
	// the coderepo service doubles as the pm CodeRepoResolver (merge-check primaryRepoURL).
	codeRepoRefRepo := pmsql.NewCodeRepoRefRepo(db)
	codeRepoSvc := coderepservice.New(coderepservice.Deps{
		DB:        db,
		Repos:     coderepsql.NewRepoRepo(db),
		IDGen:     gen,
		Clock:     clk,
		MasterKey: masterKey,
		Unlinker:  codeRepoRefRepo,
		// v2.18.4 BE-2: remote viewing (commits/branches) — go-github for "github",
		// the git ls-remote fallback for everything else. No clone; credential used
		// server-side only.
		Providers: coderepprovider.NewFactory(coderepprovider.NewGitHub(), coderepprovider.NewGit()),
	})

	pmSvc := pmservice.New(pmservice.Deps{
		DB:               db,
		Projects:         pmsql.NewProjectRepo(db),
		Members:          pmsql.NewProjectMemberRepo(db),
		Issues:           pmsql.NewIssueRepo(db),
		Tasks:            pmsql.NewTaskRepo(db),
		TaskSubs:         pmsql.NewTaskSubscriberRepo(db),
		IssueSubs:        pmsql.NewIssueSubscriberRepo(db),
		CodeRepoRefs:     codeRepoRefRepo,
		CodeRepoResolver: codeRepoSvc,
		Plans:            pmsql.NewPlanRepo(db),        // v2.9 #283/#285: Plan aggregate + DAG + dispatch records
		Findings:         pmsql.NewPlanFindingRepo(db), // v2.10 ADR-0053: plan-scoped shared findings (DeLM shared context)
		// v2.14.0 I14/F3 §7.3: persist the append-only Task lifecycle log (block/unblock/
		// lease_expired/reassigned) to pm_task_action_logs from the log-producing flows.
		TaskActionLogs: pmsql.NewTaskActionLogRepo(db, gen),
		Outbox:         outboxsql.NewOutboxRepo(db),
		IDGen:          gen,
		Clock:          clk,
		AgentDir:       agentpkg.NewOrgDirectory(agentRepo),
		// v2.18.3 BE-2 (issue-577a7b0e): the auto-assign reconciler's candidate source
		// (org agents × online/opt-out/capability/cap) + the per-project master switch's
		// settings store. Both nil-safe at the Service level; wired here in production so
		// auto-assign is live.
		AutoAssignDir:      pmservice.NewAgentAutoAssignDirectory(agentRepo, wr),
		AutoAssignSettings: settingssql.NewStore(db, clk),
		OrgSeq: pmsql.NewOrgSequenceRepo(db), // v2.7.1 #245: per-org T<n>/I<n> allocation
		// v2.9 #285: advance posts the node-ready @mention into the Plan conversation
		// via MessageWriter (the wake+mention path #220 wakes an agent assignee). The
		// resolver mirrors the WakeProjector's DisplayName resolution (strip the
		// agent:/user: scheme → IdentityRepo display_name) so the @<display_name> the
		// adapter prepends matches exactly what the wake detector (mention.Present)
		// scans for — otherwise an idle agent is never woken (BUG C).
		PlanDispatcher: convservice.NewPlanDispatchAdapter(writer, func(ctx context.Context, assigneeRef string) (string, bool) {
			id := assigneeRef
			if i := strings.IndexByte(id, ':'); i >= 0 {
				id = id[i+1:] // strip the agent:/user: scheme → bare identity id
			}
			idn, err := idIdentityRepo.GetByID(ctx, id)
			if err != nil || idn == nil {
				return "", false
			}
			return idn.DisplayName(), true
		}),
	})

	// v2.14.0 F7 (issue I14): the shared AgentWorkItem repo (+ its transition sink)
	// and the WorkItem-backed paused-task provider were removed — AgentWorkItem
	// retired. The pm Service's PausedTaskPort is left unwired (it degrades to a
	// nil-safe no-op: plan view shows no paused nodes).
	agentActivityRepo := agentsql.NewActivityEventRepo(db)

	// Usage BC (v2.15.0 I28/F2): plain db-backed repos for the report_usage tool.
	usageEventRepo := usagesql.NewUsageEventRepo(db)
	modelPriceRepo := usagesql.NewModelPriceRepo(db)

	agentSvc := agentsvc.New(agentsvc.Deps{
		DB:       db,
		Agents:   agentRepo,
		Activity: agentActivityRepo,
		Workers:  wr,
		Outbox:   outboxsql.NewOutboxRepo(db),
		IDGen:    gen,
		Clock:    clk,
	})
	// T130: wire the open→running authorization port so start_task rejects a
	// backlog task (one that is neither a real-plan node nor a dispatched pool
	// member) — closing the direct-assign→start_task path the T83 claim guard did
	// not cover. The pm Service owns the plan/pool knowledge; the Agent BC depends
	// only on the port (no import cycle).
	agentSvc.SetTaskRunGate(pmservice.NewAgentTaskRunGate(pmSvc))

	// v2.7 D1 Environment BC (ADR-0050, task #102): control-channel
	// AppService over the env sqlite repos (migration 0044).
	envControlSvc := envservice.New(envservice.Deps{
		DB:      db,
		Workers: envsql.NewWorkerRepo(db),
		Events:  envsql.NewControlEventRepo(db),
		IDGen:   gen,
		Clock:   clk,
	})

	// v2.14.0 F7 (issue I14): the WorkItem-backed node-resumer adapter wiring was
	// removed — AgentWorkItem retired. The pm Service's NodeResumer port is left
	// unwired (ResumePausedNode degrades to ErrNodeResumerUnavailable).

	// v2.13.0 I18/B3: wire the §-1 gate adapter so completing a DECISION node without
	// an explicit outcome auto-derives pass/reject from the gate verdict. Running the
	// real gate is HEAVY (a full checkout + build/test, run synchronously inside
	// complete_task), so it is an explicit operator OPT-IN via AC_DECISION_GATE_CMD
	// (the gate command, space-separated, e.g. "make gate"). Unset ⇒ B3 defers every
	// decision to a human (manual complete_task outcome only) — the safe default.
	if gateCmd := strings.Fields(os.Getenv("AC_DECISION_GATE_CMD")); len(gateCmd) > 0 {
		gateCacheBase := os.TempDir()
		if cfg.BlobStore.Root != "" {
			gateCacheBase = filepath.Dir(cfg.BlobStore.Root)
		}
		gateCacheDir := filepath.Join(gateCacheBase, "gatecheck-clones")
		pmSvc.SetDecisionGate(gatecheck.New(gateCacheDir, gateCmd, gatecheck.NewExecCommandRunner()))
	}

	// v2.7 D5 slice-1: the shared SSE down-push bus. Created here so it is the
	// SAME instance the projector's ControlLog publishes to (webconsole_wiring.go)
	// and the stream endpoint subscribes from (admin_wiring.go → HandlerDeps).
	controlStreamBus := controlstream.NewBus()

	// I5 (issue-921db054): the agent-runtime-browser correlator — ONE instance shared
	// by the webconsole runtime endpoints (Register+await) and the admin runtime-fs
	// response endpoint (Resolve), wired into both HandlerDeps below.
	runtimeFsDispatcher := runtimefs.NewDispatcher()

	return &App{
		Config:              cfg,
		DB:                  db,
		Clock:               clk,
		IDGen:               gen,
		PMService:           pmSvc,
		CodeRepoService:     codeRepoSvc,
		LiveState:           concurrency.NewInMemoryStore(),
		AgentService:        agentSvc,
		AgentRepo:           agentRepo,
		AgentActivityRepo:   agentActivityRepo,
		EnvControlSvc:       envControlSvc,
		ControlStreamBus:    controlStreamBus,
		RuntimeFsDispatcher: runtimeFsDispatcher,
		WorkerRepo:          wr,
		PMProjectRepo:       pmProjRepo,
		ConvRepo:            cr,
		MsgRepo:             mgRepo,
		EventRepo:           er,
		Sink:                sink,
		UsageEventRepo:      usageEventRepo,
		ModelPriceRepo:      modelPriceRepo,
		EnrollSvc:           enroll,
		WorkerConfigSvc:     workerConfig,
		MessageWriter:       writer,
		ChannelMgmtSvc:      channelMgmt,
		ParticipantMgmtSvc:  participantMgmt,
		CarryOverSvc:        carryOver,
		ConvRefRepo:         convRefRepo,
		ReadStateRepo:       readStateRepo,
		ReadStateSvc:        readStateSvc,
		InboxSvc:            inboxSvc,
		WakeGuard:           wakeGuard,
		ReplyNudgeSvc:       replyNudgeSvc,
		FollowStateRepo:     followStateRepo,
		FollowStateSvc:      followStateSvc,
		OutboxRepo:          outboxRepo,

		UserSecretRepo:       userSecretRepo,
		UserSecretSvc:        userSecretSvc,
		UserSecretResolveSvc: userSecretResolveSvc,

		IdentitySignupSvc:           identitySignupSvc,
		IdentitySigninSvc:           identitySigninSvc,
		IdentitySignoutSvc:          identitySignoutSvc,
		IdentityAuthSvc:             identityAuthSvc,
		IdentityPasscodeChangeSvc:   identityPasscodeChangeSvc,
		IdentityRepo:                idIdentityRepo,
		IdentityOrgRepo:             idOrgRepo,
		IdentityOrgCreateSvc:        identityOrgCreateSvc,
		IdentityOrgLifecycleSvc:     identityOrgLifecycleSvc,
		IdentityMemberRepo:          idMemberRepo,
		IdentityMemberAddSvc:        identityMemberAddSvc,
		IdentityMemberCreateUserSvc: identityMemberCreateUserSvc,
		IdentityMemberRoleChangeSvc: identityMemberRoleChangeSvc,
		IdentityMemberDisableSvc:    identityMemberDisableSvc,
		IdentityMemberRemoveSvc:     identityMemberRemoveSvc,
		IdentityAgentProvisionSvc:   identityAgentProvisionSvc,
		IdentityOrgUpdateSvc:        identityOrgUpdateSvc,
		IdentityInvitationRepo:      identityInvitationRepo,

		AdminTokenRepo: adminTokenRepo,
		AdminTokenSvc:  adminTokenSvc,

		QuerySvc:  querySvc,
		FleetSvc:  fleetSvc,
		StatsSvc:  statsSvc,
		LogsSvc:   logsSvc,
		BlobStore: bs,
	}, nil
}

// NewClientApp constructs a lightweight CLI-mode App. The DB / Service /
// Repo fields are intentionally nil — handlers MUST go through Client
// (v2.2 Phase B; conventions § 0.4 "AppService is the only entry").
//
// Use this for every CLI command except `agent-center server` (which
// IS the server and uses NewApp + an open DB).
func NewClientApp(cfg config.Config, client *Client) *App {
	return &App{
		Config: cfg,
		Client: client,
		Clock:  clock.SystemClock{},
	}
}

// operatorActor is the actor stamped on the few remaining server-side
// system/operator operations that have no logged-in session (the reconciler,
// admin rate-limit sink, the webconsole's no-session deps fallback, and worker
// commands). v2.7 #162: replaces the old config-derived DefaultActor — the CLI
// data-management commands that needed a user identity were retired, and
// identity.default_user was removed, so a fixed system actor is correct here.
func (a *App) operatorActor() observability.Actor {
	// "system" is the canonical system actor (ADR-0033: bare "system" is valid;
	// "system:<x>" is NOT — the prefixed scopes are user:/supervisor:/worker:/agent:).
	return observability.Actor("system")
}

// OpenAndMigrate is a convenience that opens the DB pointed to by cfg
// and runs migrations. The caller is responsible for closing the DB.
func OpenAndMigrate(cfg config.Config) (*sql.DB, error) {
	db, err := persistence.Open(cfg.Server.SqlitePath)
	if err != nil {
		return nil, fmt.Errorf("open sqlite %q: %w", cfg.Server.SqlitePath, err)
	}
	if err := persistence.NewMigrator(db).Up(context.Background()); err != nil {
		_ = db.Close()
		return nil, fmt.Errorf("migrate: %w", err)
	}
	return db, nil
}

// writeOut writes a line to the given writer; small helper to keep
// handlers terse.
func writeOut(w io.Writer, s string) {
	fmt.Fprintln(w, s)
}
