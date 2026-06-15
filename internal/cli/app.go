package cli

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"io"
	"strings"

	"github.com/oopslink/agent-center/internal/admintoken"
	admintokensvc "github.com/oopslink/agent-center/internal/admintoken/service"
	admintokensqlite "github.com/oopslink/agent-center/internal/admintoken/sqlite"
	agentpkg "github.com/oopslink/agent-center/internal/agent"
	agentsvc "github.com/oopslink/agent-center/internal/agent/service"
	agentsql "github.com/oopslink/agent-center/internal/agent/sqlite"
	"github.com/oopslink/agent-center/internal/blobstore"
	"github.com/oopslink/agent-center/internal/clock"
	"github.com/oopslink/agent-center/internal/config"
	"github.com/oopslink/agent-center/internal/conversation"
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
	pmservice "github.com/oopslink/agent-center/internal/projectmanager/service"
	pmsql "github.com/oopslink/agent-center/internal/projectmanager/sqlite"
	"github.com/oopslink/agent-center/internal/secretmgmt"
	secretservice "github.com/oopslink/agent-center/internal/secretmgmt/service"
	secretsqlite "github.com/oopslink/agent-center/internal/secretmgmt/sqlite"
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

	EnrollSvc *wfservice.WorkerEnrollService
	// WorkerConfigSvc backs the operator per-CLI capability toggle (v2.7 #147).
	WorkerConfigSvc *wfservice.WorkerConfigService

	// v2.7 ProjectManager BC AppService facade (ADR-0046) — backs the nested
	// /api/projects/{project_id}/... routes + produces the outbox events the
	// server-runtime relay projects into Conversation/Agent.
	PMService *pmservice.Service

	// AgentService is the v2.7 Agent BC AppService facade (C3).
	AgentService *agentsvc.Service

	// AgentRepo is the raw Agent repository (v2.7 D2-f s4). The worker boot-resume
	// admin endpoint enumerates a worker's agents (ListByWorker) — a worker-level
	// read with no fitting AppService method, so the repo is exposed directly.
	AgentRepo agentpkg.Repository

	// AgentWorkItemRepo is the raw Agent WorkItem repository (C2). The admin
	// agent-tools surface (v2.7 D2-b2 request_input) needs Update + WaitInput
	// composed inside an outer tx — the AppService only exposes read-only
	// ListWorkItems.
	AgentWorkItemRepo agentpkg.WorkItemRepository

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

	// OutboxRepo is the cross-BC outbox emitter (v2.7 D2-e-ii). The admin
	// request_input handler uses it to emit `agent.awaiting_input` in the same tx
	// as the WorkItem entering waiting_input (the batch-flush trigger).
	OutboxRepo outbox.Repository

	// Workforce — AgentInstance (P10 § 3.8 + F5)
	AgentInstanceRepo workforce.AgentInstanceRepository
	AgentMgmtSvc      *wfservice.AgentInstanceManagementService

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
	IdentityAgentProvisionSvc   *identity.AgentIdentityProvisionService
	IdentityOrgUpdateSvc        *identity.OrganizationUpdateService

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

	// Observability Phase 4
	deps := query.Deps{
		Events:        er,
		Conversations: cr,
		Messages:      mgRepo,
		Workers:       wr,
		// v2.7 #107 Phase-2 fleet repoint: new-model read deps.
		WorkItemProjections: obsqlite.NewAgentWorkItemProjectionRepo(db),
		WorkItems:           agentsql.NewWorkItemRepo(db),
		PMTasks:             pmsql.NewTaskRepo(db),
		PMProjects:          pmsql.NewProjectRepo(db),
		PMIssues:            pmsql.NewIssueRepo(db),
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
	identityAgentProvisionSvc := identity.NewAgentIdentityProvisionServiceWithSink(db, idIdentityRepo, idMemberRepo, sink)
	identityOrgUpdateSvc := identity.NewOrganizationUpdateServiceWithSink(db, idOrgRepo, sink)
	var (
		identitySigninSvc *identity.SigninService
		identityAuthSvc   *identity.AuthService
	)

	// Workforce — AgentInstance management.
	aiRepo := wfsqlite.NewAgentInstanceRepo(db)
	agentMgmt := wfservice.NewAgentInstanceManagementService(db, aiRepo, gen, sink, clk)

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
	)
	if cfg.SecretManagement.MasterKeyFile != "" {
		mk, err := secretmgmt.LoadMasterKey(cfg.SecretManagement.MasterKeyFile, cfg.SecretManagement.SkipPermsCheck)
		if err != nil {
			return nil, fmt.Errorf("load master key: %w", err)
		}
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

	pmSvc := pmservice.New(pmservice.Deps{
		DB:           db,
		Projects:     pmsql.NewProjectRepo(db),
		Members:      pmsql.NewProjectMemberRepo(db),
		Issues:       pmsql.NewIssueRepo(db),
		Tasks:        pmsql.NewTaskRepo(db),
		TaskSubs:     pmsql.NewTaskSubscriberRepo(db),
		IssueSubs:    pmsql.NewIssueSubscriberRepo(db),
		CodeRepoRefs: pmsql.NewCodeRepoRefRepo(db),
		Plans:        pmsql.NewPlanRepo(db),        // v2.9 #283/#285: Plan aggregate + DAG + dispatch records
		Findings:     pmsql.NewPlanFindingRepo(db), // v2.10 ADR-0053: plan-scoped shared findings (DeLM shared context)
		Outbox:       outboxsql.NewOutboxRepo(db),
		IDGen:        gen,
		Clock:        clk,
		AgentDir:     agentpkg.NewOrgDirectory(agentRepo),
		OrgSeq:       pmsql.NewOrgSequenceRepo(db), // v2.7.1 #245: per-org T<n>/I<n> allocation
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

	// Shared agent WorkItem repo: the Agent BC AppService owns it, and the
	// admin agent-tools surface (v2.7 D2-b2) needs the raw repo to do
	// Update + WaitInput inside an outer tx (the AppService only exposes a
	// read-only ListWorkItems).
	// v2.7 #111 locus B: the shared WorkItem repo emits agent.work_item_transitioned
	// for every status change, drained from the AR and appended in the persisting
	// tx via the outbox sink. Wiring the sink here (composition root) keeps the
	// sqlite adapter free of any outbox dependency.
	workItemTransitionSink := agentsvc.NewOutboxWorkItemTransitionSink(outboxsql.NewOutboxRepo(db), gen)
	agentWorkItemRepo := agentsql.NewWorkItemRepoWithSink(db, workItemTransitionSink)
	agentActivityRepo := agentsql.NewActivityEventRepo(db)

	agentSvc := agentsvc.New(agentsvc.Deps{
		DB:        db,
		Agents:    agentRepo,
		WorkItems: agentWorkItemRepo,
		Activity:  agentActivityRepo,
		Workers:   wr,
		Outbox:    outboxsql.NewOutboxRepo(db),
		IDGen:     gen,
		Clock:     clk,
	})

	// v2.7 D1 Environment BC (ADR-0050, task #102): control-channel
	// AppService over the env sqlite repos (migration 0044).
	envControlSvc := envservice.New(envservice.Deps{
		DB:      db,
		Workers: envsql.NewWorkerRepo(db),
		Events:  envsql.NewControlEventRepo(db),
		IDGen:   gen,
		Clock:   clk,
	})

	// v2.7 D5 slice-1: the shared SSE down-push bus. Created here so it is the
	// SAME instance the projector's ControlLog publishes to (webconsole_wiring.go)
	// and the stream endpoint subscribes from (admin_wiring.go → HandlerDeps).
	controlStreamBus := controlstream.NewBus()

	return &App{
		Config:             cfg,
		DB:                 db,
		Clock:              clk,
		IDGen:              gen,
		PMService:          pmSvc,
		AgentService:       agentSvc,
		AgentRepo:          agentRepo,
		AgentWorkItemRepo:  agentWorkItemRepo,
		AgentActivityRepo:  agentActivityRepo,
		EnvControlSvc:      envControlSvc,
		ControlStreamBus:   controlStreamBus,
		WorkerRepo:         wr,
		PMProjectRepo:      pmProjRepo,
		ConvRepo:           cr,
		MsgRepo:            mgRepo,
		EventRepo:          er,
		Sink:               sink,
		EnrollSvc:          enroll,
		WorkerConfigSvc:    workerConfig,
		MessageWriter:      writer,
		ChannelMgmtSvc:     channelMgmt,
		ParticipantMgmtSvc: participantMgmt,
		CarryOverSvc:       carryOver,
		ConvRefRepo:        convRefRepo,
		ReadStateRepo:      readStateRepo,
		ReadStateSvc:       readStateSvc,
		InboxSvc:           inboxSvc,
		FollowStateRepo:    followStateRepo,
		FollowStateSvc:     followStateSvc,
		OutboxRepo:         outboxRepo,

		AgentInstanceRepo: aiRepo,
		AgentMgmtSvc:      agentMgmt,

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
		IdentityAgentProvisionSvc:   identityAgentProvisionSvc,
		IdentityOrgUpdateSvc:        identityOrgUpdateSvc,

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
