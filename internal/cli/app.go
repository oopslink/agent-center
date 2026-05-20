package cli

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"io"

	"github.com/oopslink/agent-center/internal/clock"
	"github.com/oopslink/agent-center/internal/config"
	"github.com/oopslink/agent-center/internal/conversation"
	convsqlite "github.com/oopslink/agent-center/internal/conversation/sqlite"
	convservice "github.com/oopslink/agent-center/internal/conversation/service"
	"github.com/oopslink/agent-center/internal/idgen"
	"github.com/oopslink/agent-center/internal/observability"
	obsqlite "github.com/oopslink/agent-center/internal/observability/sqlite"
	"github.com/oopslink/agent-center/internal/persistence"
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
	MessageWriter *convservice.MessageWriter

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
	IssueSpawnStub   *dispatch.IssueConcludeSpawn
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

	// TaskRuntime
	taskRepo := trsqlite.NewTaskRepo(db)
	execRepo := trsqlite.NewTaskExecutionRepo(db)
	irRepo := trsqlite.NewInputRequestRepo(db)
	artifactRepo := trsqlite.NewArtifactRepo(db)
	taskSvc := trservice.NewTaskService(db, taskRepo, cr, execRepo, mgRepo, sink, gen, clk)
	irSvc := trservice.NewInputRequestService(db, irRepo, execRepo, taskRepo, cr, mgRepo, sink, gen, clk, cfg.Notification.DefaultChannel)
	artifactSvc := trservice.NewArtifactService(db, artifactRepo, execRepo, sink, gen, clk)
	execSvc := trservice.NewExecutionService(db, execRepo, taskRepo, cr, mgRepo, sink, gen, clk)
	dispatchSvc := dispatch.NewService(db, taskRepo, execRepo, sink, dispatch.NoopSender{}, clk, gen, dispatch.DispatchConfig{
		MaxExecutionsPerTask: cfg.Execution.MaxExecutionsPerTask,
		DispatchAckTimeout:   cfg.Execution.DispatchAckTimeout(),
	})
	killCoord := kill.NewCoordinator(db, execRepo, taskRepo, irRepo, sink, kill.NoopKillSender{}, clk)
	issueSpawnStub := dispatch.NewIssueConcludeSpawn(db, taskRepo, sink, gen, clk)

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
		MessageWriter:   writer,
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
		IssueSpawnStub:  issueSpawnStub,
	}, nil
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
