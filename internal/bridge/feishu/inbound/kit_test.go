package inbound_test

import (
	"context"
	"database/sql"
	"testing"
	"time"

	"github.com/oopslink/agent-center/internal/bridge/feishu/inbound"
	"github.com/oopslink/agent-center/internal/clock"
	"github.com/oopslink/agent-center/internal/conversation"
	convservice "github.com/oopslink/agent-center/internal/conversation/service"
	convsqlite "github.com/oopslink/agent-center/internal/conversation/sqlite"
	"github.com/oopslink/agent-center/internal/conversation/identity"
	"github.com/oopslink/agent-center/internal/idgen"
	"github.com/oopslink/agent-center/internal/observability"
	obsqlite "github.com/oopslink/agent-center/internal/observability/sqlite"
	"github.com/oopslink/agent-center/internal/persistence"
	"github.com/oopslink/agent-center/internal/taskruntime"
	"github.com/oopslink/agent-center/internal/taskruntime/execution"
	"github.com/oopslink/agent-center/internal/taskruntime/inputrequest"
	trservice "github.com/oopslink/agent-center/internal/taskruntime/service"
	trsqlite "github.com/oopslink/agent-center/internal/taskruntime/sqlite"
	"github.com/oopslink/agent-center/internal/taskruntime/task"
)

type fixture struct {
	t          *testing.T
	db         *sql.DB
	clock      *clock.FakeClock
	idgen      idgen.Generator
	sink       *observability.EventSink
	events     *obsqlite.EventRepo
	identities identity.IdentityRepository
	bindings   identity.ChannelBindingRepository
	identReg   *identity.RegistrationService
	convs      conversation.ConversationRepository
	msgRepo    conversation.MessageRepository
	msgWriter  *convservice.MessageWriter
	tasks      task.Repository
	execs      execution.Repository
	irs        inputrequest.Repository
	taskSvc    *trservice.TaskService
	irSvc      *trservice.InputRequestService

	resolver *inbound.IdentityResolver
	parser   *inbound.SlashCommandParser
	slash    *inbound.SlashRouter
	card     *inbound.CardCallback
	router   *inbound.Router
}

func newFixture(t *testing.T) *fixture {
	t.Helper()
	path := t.TempDir() + "/inbound.db"
	db, err := persistence.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	if err := persistence.NewMigrator(db).Up(context.Background()); err != nil {
		t.Fatal(err)
	}
	fc := clock.NewFakeClock(time.Date(2026, 5, 22, 10, 0, 0, 0, time.UTC))
	gen := idgen.NewGenerator(fc)
	er, err := obsqlite.NewEventRepo(context.Background(), db)
	if err != nil {
		t.Fatal(err)
	}
	sink := observability.NewEventSink(er, er, gen, fc)
	idents := identity.NewSQLiteIdentityRepo(db)
	binds := identity.NewSQLiteChannelBindingRepo(db)
	reg := identity.NewRegistrationService(db, idents, binds, sink, gen, fc)
	cr := convsqlite.NewConversationRepo(db)
	mr := convsqlite.NewMessageRepo(db)
	writer := convservice.NewMessageWriter(db, cr, mr, sink, gen, fc)
	tr := trsqlite.NewTaskRepo(db)
	er2 := trsqlite.NewTaskExecutionRepo(db)
	ir := trsqlite.NewInputRequestRepo(db)
	tsvc := trservice.NewTaskService(db, tr, cr, er2, mr, sink, gen, fc)
	isvc := trservice.NewInputRequestService(db, ir, er2, tr, cr, mr, sink, gen, fc, "feishu")

	resolver, err := inbound.NewIdentityResolver(inbound.IdentityResolverDeps{
		Bindings:     binds,
		Identities:   idents,
		Registration: reg,
		Sink:         sink,
		Clock:        fc,
		Channel:      identity.Channel("feishu"),
		Actor:        observability.Actor("system"),
	})
	if err != nil {
		t.Fatal(err)
	}
	parser := inbound.NewSlashCommandParser()
	slash, err := inbound.NewSlashRouter(inbound.SlashRouterDeps{
		DB:        db,
		Clock:     fc,
		IDGen:     gen,
		Sink:      sink,
		Tasks:     tr,
		Execs:     er2,
		Convs:     cr,
		TaskSvc:   tsvc,
		IRSvc:     isvc,
		IRRepo:    ir,
		MsgWriter: writer,
		Actor:     observability.Actor("system"),
	})
	if err != nil {
		t.Fatal(err)
	}
	card, err := inbound.NewCardCallback(inbound.CardCallbackDeps{
		Clock:     fc,
		Sink:      sink,
		IRRepo:    ir,
		IRSvc:     isvc,
		Execs:     er2,
		Tasks:     tr,
		MsgWriter: writer,
		Actor:     observability.Actor("system"),
	})
	if err != nil {
		t.Fatal(err)
	}
	dedupe := inbound.NewDedupe(0, 0, fc)
	router, err := inbound.NewRouter(inbound.RouterDeps{
		Clock:     fc,
		IDGen:     gen,
		Sink:      sink,
		Dedupe:    dedupe,
		Resolver:  resolver,
		Parser:    parser,
		Slash:     slash,
		Card:      card,
		DB:        db,
		Convs:     cr,
		MsgWriter: writer,
		Actor:     observability.Actor("system"),
	})
	if err != nil {
		t.Fatal(err)
	}
	return &fixture{
		t:          t,
		db:         db,
		clock:      fc,
		idgen:      gen,
		sink:       sink,
		events:     er,
		identities: idents,
		bindings:   binds,
		identReg:   reg,
		convs:      cr,
		msgRepo:    mr,
		msgWriter:  writer,
		tasks:      tr,
		execs:      er2,
		irs:        ir,
		taskSvc:    tsvc,
		irSvc:      isvc,
		resolver:   resolver,
		parser:     parser,
		slash:      slash,
		card:       card,
		router:     router,
	}
}

// seedUser registers a user identity (used in most tests). Returns the
// IdentityID.
func (f *fixture) seedUser(t *testing.T, name string) identity.IdentityID {
	t.Helper()
	id := identity.IdentityID("user:" + name)
	res, err := f.identReg.RegisterIdentity(context.Background(), identity.RegisterIdentityCommand{
		ID:          id,
		Kind:        identity.KindUser,
		DisplayName: name,
		Actor:       observability.Actor("system"),
	})
	if err != nil {
		t.Fatalf("seed user: %v", err)
	}
	return res.Identity.ID()
}

// seedTaskWithIR builds a task with conversation + execution in working
// state + a pending InputRequest. Returns ids the caller needs.
func (f *fixture) seedTaskWithIR(t *testing.T) (taskID taskruntime.TaskID, execID taskruntime.TaskExecutionID, irID taskruntime.InputRequestID, convID conversation.ConversationID) {
	t.Helper()
	ctx := context.Background()
	res, err := f.taskSvc.Create(ctx, trservice.TaskCreateInput{
		ProjectID:        "demo",
		Title:            "test",
		WithConversation: true,
		ConversationTitle: "kit",
		Actor:            observability.Actor("system"),
	})
	if err != nil {
		t.Fatalf("seed task: %v", err)
	}
	// Seed execution in StatusWorking via direct repo write.
	now := f.clock.Now()
	e, err := execution.New(execution.NewInput{
		ID:            taskruntime.TaskExecutionID(f.idgen.NewULID()),
		TaskID:        res.TaskID,
		WorkerID:      "worker-1",
		AgentCLI:      "fake",
		WorkspaceMode: execution.WorkspaceWorktree,
		Now:           now,
	})
	if err != nil {
		t.Fatalf("seed exec: %v", err)
	}
	if err := e.StartWorking("/tmp/cwd", now); err != nil {
		t.Fatalf("start working: %v", err)
	}
	if err := f.execs.Save(ctx, e); err != nil {
		t.Fatalf("save exec: %v", err)
	}
	// Now create InputRequest via the IR service (writes IR + flips
	// execution to input_required + writes a Message).
	ir, err := f.irSvc.Create(ctx, trservice.CreateInput{
		ExecutionID: e.ID(),
		Question:    "pick one",
		Options:     []string{"A", "B"},
		Urgency:     inputrequest.UrgencyNormal,
		Actor:       observability.Actor("system"),
	})
	if err != nil {
		t.Fatalf("ir create: %v", err)
	}
	return res.TaskID, e.ID(), ir.InputRequestID, res.ConversationID
}

func (f *fixture) hasEvent(t *testing.T, etype observability.EventType) bool {
	t.Helper()
	since := time.Time{}
	evs, err := f.events.Find(context.Background(), observability.EventQueryFilter{
		EventType: &etype, Since: &since, Limit: 100,
	})
	if err != nil {
		t.Fatalf("find events: %v", err)
	}
	return len(evs) > 0
}
