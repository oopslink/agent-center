package cli

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"io"

	"github.com/oopslink/agent-center/internal/blobstore"
	"github.com/oopslink/agent-center/internal/clock"
	"github.com/oopslink/agent-center/internal/cognition"
	"github.com/oopslink/agent-center/internal/cognition/decision"
	"github.com/oopslink/agent-center/internal/cognition/scheduler"
	"github.com/oopslink/agent-center/internal/config"
	"github.com/oopslink/agent-center/internal/conversation"
	"github.com/oopslink/agent-center/internal/conversation/identity"
	convsqlite "github.com/oopslink/agent-center/internal/conversation/sqlite"
	convservice "github.com/oopslink/agent-center/internal/conversation/service"
	"github.com/oopslink/agent-center/internal/discussion"
	disqlite "github.com/oopslink/agent-center/internal/discussion/sqlite"
	disservice "github.com/oopslink/agent-center/internal/discussion/service"
	"github.com/oopslink/agent-center/internal/idgen"
	"github.com/oopslink/agent-center/internal/observability"
	"github.com/oopslink/agent-center/internal/observability/projection"
	"github.com/oopslink/agent-center/internal/observability/query"
	obsqlite "github.com/oopslink/agent-center/internal/observability/sqlite"
	"github.com/oopslink/agent-center/internal/persistence"
	cognitiondb "github.com/oopslink/agent-center/internal/persistence/cognition"
	"github.com/oopslink/agent-center/internal/taskruntime/dispatch"
	"github.com/oopslink/agent-center/internal/taskruntime/execution"
	"github.com/oopslink/agent-center/internal/taskruntime/inputrequest"
	"github.com/oopslink/agent-center/internal/taskruntime/kill"
	trservice "github.com/oopslink/agent-center/internal/taskruntime/service"
	trsqlite "github.com/oopslink/agent-center/internal/taskruntime/sqlite"
	"github.com/oopslink/agent-center/internal/taskruntime/task"
	"github.com/oopslink/agent-center/internal/workforce"
	wfsqlite "github.com/oopslink/agent-center/internal/workforce/sqlite"
	wfservice "github.com/oopslink/agent-center/internal/workforce/service"
	"github.com/oopslink/agent-center/internal/secretmgmt"
	secretservice "github.com/oopslink/agent-center/internal/secretmgmt/service"
	secretsqlite "github.com/oopslink/agent-center/internal/secretmgmt/sqlite"
)

// App carries everything CLI handlers need.
type App struct {
	Config config.Config
	DB     *sql.DB
	Clock  clock.Clock
	IDGen  idgen.Generator

	WorkerRepo   workforce.WorkerRepository
	MappingRepo  workforce.WorkerProjectMappingRepository
	ProposalRepo workforce.WorkerProjectProposalRepository
	ProjectRepo  workforce.ProjectRepository
	ConvRepo     conversation.ConversationRepository
	MsgRepo      conversation.MessageRepository
	EventRepo    *obsqlite.EventRepo
	Sink         *observability.EventSink

	EnrollSvc     *wfservice.WorkerEnrollService
	DiscoverySvc  *wfservice.ProjectDiscoveryService
	AcceptanceSvc *wfservice.ProposalAcceptanceService
	ProjectSvc    *wfservice.ProjectCRUDService
	MessageWriter      *convservice.MessageWriter
	ChannelMgmtSvc     *convservice.ChannelManagementService
	ParticipantMgmtSvc *convservice.ParticipantManagementService
	CarryOverSvc       *convservice.CarryOverService
	ConvRefRepo        conversation.ConversationMessageReferenceRepository
	DerivationSvc      *convservice.MessageDerivationService

	// Workforce — AgentInstance (P10 § 3.8 + F5)
	AgentInstanceRepo workforce.AgentInstanceRepository
	AgentMgmtSvc      *wfservice.AgentInstanceManagementService

	// SecretManagement (P11 § 3.7b)
	UserSecretRepo secretmgmt.UserSecretRepository
	UserSecretSvc  *secretservice.UserSecretService

	// TaskRuntime
	TaskRepo         task.Repository
	ExecRepo         execution.Repository
	IRRepo           inputrequest.Repository
	ArtifactRepo     execution.ArtifactRepository
	TaskSvc          *trservice.TaskService
	IRSvc            *trservice.InputRequestService
	ArtifactSvc      *trservice.ArtifactService
	ExecSvc          *trservice.ExecutionService
	DispatchSvc      *dispatch.Service
	KillCoordinator  *kill.Coordinator
	IssueSpawn       *dispatch.IssueConcludeSpawn

	// Discussion
	IssueRepo                       discussion.IssueRepository
	IssueLifecycleSvc               *disservice.IssueLifecycleService
	IssueCommentSvc                 *disservice.IssueCommentService
	IssueBindConversationSvc        *disservice.IssueBindConversationService
	IssueLinkConversationSvc        *disservice.IssueLinkConversationService
	IssueConversationOpener         *disservice.IssueConversationOpener

	// Observability Phase 4
	ProjectionRepo  projection.Repository
	ProjectionSvc   *projection.TaskExecutionProjectionService
	QuerySvc        *query.Service
	FleetSvc        *query.FleetSnapshotService
	StatsSvc        *query.StatsService
	LogsSvc         *query.LogsService
	BlobStore       blobstore.BlobStore

	// Identity (Phase 5; ChannelBinding removed per ADR-0031/0033 in P10 § 3.9 + § 3.2)
	IdentityRepo         identity.IdentityRepository
	IdentityRegistration *identity.RegistrationService

	// Cognition (Phase 6)
	InvocationRepo    cognition.SupervisorInvocationRepository
	DecisionRepo      cognition.DecisionRecordRepository
	DecisionRecorder  *decision.Recorder
	SupervisorSpawner *scheduler.Spawner
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
	mr := wfsqlite.NewMappingRepo(db)
	prRepo := wfsqlite.NewProposalRepo(db)
	pjRepo := wfsqlite.NewProjectRepo(db)
	cr := convsqlite.NewConversationRepo(db)
	mgRepo := convsqlite.NewMessageRepo(db)

	disc := wfservice.NewProjectDiscoveryService(pjRepo, sink, clk)
	acc := wfservice.NewProposalAcceptanceService(db, prRepo, mr, pjRepo, disc, sink, gen, clk)
	pjSvc := wfservice.NewProjectCRUDService(db, pjRepo, mr, sink, clk)
	enroll := wfservice.NewWorkerEnrollService(db, wr, sink, clk)
	writer := convservice.NewMessageWriter(db, cr, mgRepo, sink, gen, clk)
	channelMgmt := convservice.NewChannelManagementService(db, cr, sink, gen, clk)
	participantMgmt := convservice.NewParticipantManagementService(db, cr, sink, clk)
	convRefRepo := convsqlite.NewReferenceRepo(db)
	carryOver := convservice.NewCarryOverService(db, cr, mgRepo, convRefRepo, sink, gen, clk)

	// TaskRuntime
	taskRepo := trsqlite.NewTaskRepo(db)
	execRepo := trsqlite.NewTaskExecutionRepo(db)
	irRepo := trsqlite.NewInputRequestRepo(db)
	artifactRepo := trsqlite.NewArtifactRepo(db)
	taskSvc := trservice.NewTaskService(db, taskRepo, cr, execRepo, mgRepo, sink, gen, clk).
		WithProjectExistenceChecker(projectCheckerAdapter{repo: pjRepo})
	irSvc := trservice.NewInputRequestService(db, irRepo, execRepo, taskRepo, cr, mgRepo, sink, gen, clk, cfg.Notification.DefaultChannel)
	artifactSvc := trservice.NewArtifactService(db, artifactRepo, execRepo, sink, gen, clk)
	execSvc := trservice.NewExecutionService(db, execRepo, taskRepo, cr, mgRepo, sink, gen, clk)
	dispatchSvc := dispatch.NewService(db, taskRepo, execRepo, sink, dispatch.NoopSender{}, clk, gen, dispatch.DispatchConfig{
		MaxExecutionsPerTask: cfg.Execution.MaxExecutionsPerTask,
		DispatchAckTimeout:   cfg.Execution.DispatchAckTimeout(),
	})
	killCoord := kill.NewCoordinator(db, execRepo, taskRepo, irRepo, sink, kill.NoopKillSender{}, clk)
	issueSpawn := dispatch.NewIssueConcludeSpawn(db, taskRepo, sink, gen, clk)

	// Discussion BC
	issueRepo := disqlite.NewIssueRepo(db)
	convOpener := disservice.NewIssueConversationOpener(cr, sink, gen, clk)
	issueLifecycle := disservice.NewIssueLifecycleService(db, issueRepo, convOpener, writer, sink, gen, clk).
		WithProjectExistenceChecker(projectCheckerAdapter{repo: pjRepo}).
		WithSpawnerAndCommenter(issueSpawn, writer)
	issueComment := disservice.NewIssueCommentService(issueRepo, cr, mgRepo, writer, issueLifecycle, clk)
	issueBind := disservice.NewIssueBindConversationService(db, issueRepo, cr, convOpener, sink, clk)
	issueLink := disservice.NewIssueLinkConversationService(db, issueRepo, cr, clk)

	// P10 F2: CV4 派生入口 — MessageDerivationService wraps existing
	// IssueLifecycle / TaskService through adapter shims so `issue open
	// --from-conversation` and `task new --from-conversation` work
	// end-to-end with carry-over refs.
	derivationSvc := convservice.NewMessageDerivationService(db, cr, mgRepo, carryOver,
		&issueOpenerShim{svc: issueLifecycle},
		&taskCreatorShim{svc: taskSvc},
		sink, clk)

	// Observability Phase 4
	projRepo := obsqlite.NewProjectionRepo(db)
	projSvc := projection.NewTaskExecutionProjectionService(projRepo, sink, nil, clk)
	deps := query.Deps{
		Events:        er,
		Projection:    projRepo,
		Tasks:         taskRepo,
		Executions:    execRepo,
		Artifacts:     artifactRepo,
		InputReqs:     irRepo,
		Issues:        issueRepo,
		Conversations: cr,
		Messages:      mgRepo,
		Workers:       wr,
		Mappings:      mr,
		Proposals:     prRepo,
		Projects:      pjRepo,
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

	// Phase 5: Identity (ChannelBinding removed per ADR-0031/0033).
	identityRepo := identity.NewSQLiteIdentityRepo(db)
	identityReg := identity.NewRegistrationService(db, identityRepo, sink, gen, clk)
	// P10 § 3.8: auto-provision the `system` identity at center startup.
	if err := identityReg.EnsureSystemIdentity(context.Background(), observability.Actor("system")); err != nil {
		return nil, fmt.Errorf("ensure system identity: %w", err)
	}

	// P10 F5: AgentInstance management — Create flow auto-registers
	// Identity[kind=agent].id='agent:<id>' in the same tx via the
	// IdentityRegistrar port (cross-aggregate invariant ADR-0033 § 4).
	aiRepo := wfsqlite.NewAgentInstanceRepo(db)
	agentMgmt := wfservice.NewAgentInstanceManagementService(db, aiRepo, gen, sink, clk).
		WithIdentityRegistrar(identityReg)

	// P11 § 3.7b: UserSecret management — wired iff master key file is
	// configured. Without master key the CLI handlers refuse with
	// ExitNotImplemented (handler-side gate).
	userSecretRepo := secretsqlite.NewUserSecretRepo(db)
	var userSecretSvc *secretservice.UserSecretService
	if cfg.SecretManagement.MasterKeyFile != "" {
		mk, err := secretmgmt.LoadMasterKey(cfg.SecretManagement.MasterKeyFile, cfg.SecretManagement.SkipPermsCheck)
		if err != nil {
			return nil, fmt.Errorf("load master key: %w", err)
		}
		userSecretSvc = secretservice.NewUserSecretService(db, userSecretRepo, gen, sink, clk, mk)
	}

	// Phase 6: Cognition (Supervisor + DecisionRecord).
	cognitiondbInv := cognitiondb.NewInvocationRepo(db)
	cognitiondbDec := cognitiondb.NewDecisionRepo(db)
	decisionRecorder, decErr := decision.NewRecorder(cognitiondbDec, clk, gen)
	if decErr != nil {
		return nil, fmt.Errorf("decision recorder: %w", decErr)
	}

	return &App{
		Config:        cfg,
		DB:            db,
		Clock:         clk,
		IDGen:         gen,
		WorkerRepo:    wr,
		MappingRepo:   mr,
		ProposalRepo:  prRepo,
		ProjectRepo:   pjRepo,
		ConvRepo:      cr,
		MsgRepo:       mgRepo,
		EventRepo:     er,
		Sink:          sink,
		EnrollSvc:       enroll,
		DiscoverySvc:    disc,
		AcceptanceSvc:   acc,
		ProjectSvc:      pjSvc,
		MessageWriter:      writer,
		ChannelMgmtSvc:     channelMgmt,
		ParticipantMgmtSvc: participantMgmt,
		CarryOverSvc:       carryOver,
		ConvRefRepo:        convRefRepo,
		DerivationSvc:      derivationSvc,

		AgentInstanceRepo: aiRepo,
		AgentMgmtSvc:      agentMgmt,

		UserSecretRepo: userSecretRepo,
		UserSecretSvc:  userSecretSvc,
		TaskRepo:        taskRepo,
		ExecRepo:        execRepo,
		IRRepo:          irRepo,
		ArtifactRepo:    artifactRepo,
		TaskSvc:         taskSvc,
		IRSvc:           irSvc,
		ArtifactSvc:     artifactSvc,
		ExecSvc:         execSvc,
		DispatchSvc:     dispatchSvc,
		KillCoordinator: killCoord,
		IssueSpawn:      issueSpawn,

		IssueRepo:                issueRepo,
		IssueLifecycleSvc:        issueLifecycle,
		IssueCommentSvc:          issueComment,
		IssueBindConversationSvc: issueBind,
		IssueLinkConversationSvc: issueLink,
		IssueConversationOpener:  convOpener,

		ProjectionRepo: projRepo,
		ProjectionSvc:  projSvc,
		QuerySvc:       querySvc,
		FleetSvc:       fleetSvc,
		StatsSvc:       statsSvc,
		LogsSvc:        logsSvc,
		BlobStore:      bs,

		IdentityRepo:         identityRepo,
		IdentityRegistration: identityReg,

		InvocationRepo:   cognitiondbInv,
		DecisionRepo:     cognitiondbDec,
		DecisionRecorder: decisionRecorder,
		// SupervisorSpawner is wired by `server` mode only (it requires
		// the actual subprocess binary path + memory dir); CLI invocations
		// of `supervisor retrigger` from a tester / one-shot context can
		// inject one via SetSupervisorSpawner.
	}, nil
}

// SetSupervisorSpawner installs (or replaces) the spawner. Tests + the
// server boot path use this to avoid wiring a spawner for every short-
// lived CLI invocation.
func (a *App) SetSupervisorSpawner(sp *scheduler.Spawner) {
	a.SupervisorSpawner = sp
}

// DefaultActor returns the configured single-user identity wrapped in the
// observability.Actor type.
func (a *App) DefaultActor() observability.Actor {
	return observability.Actor("user:" + a.Config.Identity.DefaultUser)
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

// projectCheckerAdapter adapts workforce.ProjectRepository to the
// taskruntime/service.ProjectExistenceChecker port. Used to enforce
// task.project_id referential integrity at the application layer per
// conventions § 9.w.
type projectCheckerAdapter struct {
	repo workforce.ProjectRepository
}

// ProjectExists reports whether a project with the given id is present.
func (a projectCheckerAdapter) ProjectExists(ctx context.Context, projectID string) (bool, error) {
	if _, err := a.repo.FindByID(ctx, workforce.ProjectID(projectID)); err != nil {
		if errors.Is(err, workforce.ErrProjectNotFound) {
			return false, nil
		}
		return false, err
	}
	return true, nil
}
