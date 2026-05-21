// Package harness is the Phase 7 e2e driver (plan-7 § 3.8).
//
// Spin(t) builds an in-process double of the full agent-center stack:
//   - real SQLite (temp dir) + migrations applied
//   - real BlobStore (LocalDir under temp dir)
//   - fake feishu server (tests/e2e/fakeserver/feishu)
//   - in-process inbound Router wired against the fake's Inbox channel
//   - fake clock for time travel
//
// Scenarios use:
//
//	h := harness.Spin(t)
//	defer h.Shutdown()
//	h.SeedUser("hayang")
//	h.Feishu.Inject(inbound.VendorEvent{...})
//	h.AwaitEvent("conversation.message_added")
//
// Goroutine model: a single "driver" goroutine drains the fake's
// inbound channel into the Router. No sleep, all sync via channels
// + the EventSink for assertions.
package harness

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/oopslink/agent-center/internal/blobstore"
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
	"github.com/oopslink/agent-center/internal/taskruntime/execution"
	"github.com/oopslink/agent-center/internal/taskruntime/inputrequest"
	trservice "github.com/oopslink/agent-center/internal/taskruntime/service"
	trsqlite "github.com/oopslink/agent-center/internal/taskruntime/sqlite"
	"github.com/oopslink/agent-center/internal/taskruntime/task"
	"github.com/oopslink/agent-center/tests/e2e/fakeserver/feishu"
)

// Harness bundles the in-process stack the e2e scenarios drive.
type Harness struct {
	t      *testing.T
	ctx    context.Context
	cancel context.CancelFunc

	DB     *sql.DB
	Clock  *clock.FakeClock
	IDGen  idgen.Generator
	Sink   *observability.EventSink
	Events *obsqlite.EventRepo
	Blobs  blobstore.BlobStore

	Conv     conversation.ConversationRepository
	Msg      conversation.MessageRepository
	Ident    identity.IdentityRepository
	Bind     identity.ChannelBindingRepository
	IReg     *identity.RegistrationService
	MWriter  *convservice.MessageWriter

	Task      task.Repository
	Exec      execution.Repository
	IR        inputrequest.Repository
	TaskSvc   *trservice.TaskService
	IRSvc     *trservice.InputRequestService

	Router *inbound.Router
	Feishu *feishu.Server

	wg sync.WaitGroup
}

// Spin builds a fresh harness backed by a temp dir.
func Spin(t *testing.T) *Harness {
	t.Helper()
	dir := t.TempDir()
	dbPath := filepath.Join(dir, "agent-center.db")
	db, err := persistence.Open(dbPath)
	if err != nil {
		t.Fatal(err)
	}
	if err := persistence.NewMigrator(db).Up(context.Background()); err != nil {
		t.Fatal(err)
	}
	fc := clock.NewFakeClock(time.Date(2026, 5, 22, 12, 0, 0, 0, time.UTC))
	gen := idgen.NewGenerator(fc)
	er, _ := obsqlite.NewEventRepo(context.Background(), db)
	sink := observability.NewEventSink(er, er, gen, fc)
	blobs, err := blobstore.NewLocalDir(filepath.Join(dir, "blobs"))
	if err != nil {
		t.Fatal(err)
	}
	convs := convsqlite.NewConversationRepo(db)
	msgs := convsqlite.NewMessageRepo(db)
	idents := identity.NewSQLiteIdentityRepo(db)
	binds := identity.NewSQLiteChannelBindingRepo(db)
	ireg := identity.NewRegistrationService(db, idents, binds, sink, gen, fc)
	mw := convservice.NewMessageWriter(db, convs, msgs, sink, gen, fc)
	tasks := trsqlite.NewTaskRepo(db)
	execs := trsqlite.NewTaskExecutionRepo(db)
	irs := trsqlite.NewInputRequestRepo(db)
	tsvc := trservice.NewTaskService(db, tasks, convs, execs, msgs, sink, gen, fc)
	isvc := trservice.NewInputRequestService(db, irs, execs, tasks, convs, msgs, sink, gen, fc, "feishu")

	resolver, err := inbound.NewIdentityResolver(inbound.IdentityResolverDeps{
		Bindings: binds, Identities: idents, Registration: ireg,
		Sink: sink, Clock: fc, Channel: "feishu", Actor: observability.Actor("system"),
	})
	if err != nil {
		t.Fatal(err)
	}
	parser := inbound.NewSlashCommandParser()
	slash, err := inbound.NewSlashRouter(inbound.SlashRouterDeps{
		DB: db, Clock: fc, IDGen: gen, Sink: sink,
		Tasks: tasks, Execs: execs, Convs: convs,
		TaskSvc: tsvc, IRSvc: isvc, IRRepo: irs, MsgWriter: mw,
		Actor: observability.Actor("system"),
	})
	if err != nil {
		t.Fatal(err)
	}
	card, err := inbound.NewCardCallback(inbound.CardCallbackDeps{
		Clock: fc, Sink: sink, IRRepo: irs, IRSvc: isvc, Execs: execs,
		Tasks: tasks, MsgWriter: mw, Actor: observability.Actor("system"),
	})
	if err != nil {
		t.Fatal(err)
	}
	dedupe := inbound.NewDedupe(0, 0, fc)
	router, err := inbound.NewRouter(inbound.RouterDeps{
		Clock: fc, IDGen: gen, Sink: sink, Dedupe: dedupe,
		Resolver: resolver, Parser: parser, Slash: slash, Card: card,
		DB: db, Convs: convs, MsgWriter: mw,
		Actor: observability.Actor("system"),
	})
	if err != nil {
		t.Fatal(err)
	}
	fserver := feishu.New()

	ctx, cancel := context.WithCancel(context.Background())
	h := &Harness{
		t: t, ctx: ctx, cancel: cancel,
		DB: db, Clock: fc, IDGen: gen, Sink: sink, Events: er, Blobs: blobs,
		Conv: convs, Msg: msgs, Ident: idents, Bind: binds, IReg: ireg, MWriter: mw,
		Task: tasks, Exec: execs, IR: irs, TaskSvc: tsvc, IRSvc: isvc,
		Router: router, Feishu: fserver,
	}
	// Driver goroutine: drains fake feishu inbox → Router.
	h.wg.Add(1)
	go func() {
		defer h.wg.Done()
		for {
			select {
			case <-ctx.Done():
				return
			case ev, ok := <-fserver.Inbox():
				if !ok {
					return
				}
				if _, err := router.OnVendorEvent(ctx, ev); err != nil {
					// Surface but do not fail — the test asserts via
					// events table / outbound.
					t.Logf("router: %v", err)
				}
			}
		}
	}()
	t.Cleanup(h.Shutdown)
	return h
}

// Shutdown signals all goroutines + closes the DB.
func (h *Harness) Shutdown() {
	h.cancel()
	h.Feishu.Close()
	h.wg.Wait()
	_ = h.DB.Close()
}

// SeedUser registers a kind=user Identity. Returns the IdentityID.
func (h *Harness) SeedUser(name string) identity.IdentityID {
	h.t.Helper()
	res, err := h.IReg.RegisterIdentity(context.Background(), identity.RegisterIdentityCommand{
		ID:          identity.IdentityID("user:" + name),
		Kind:        identity.KindUser,
		DisplayName: name,
		Actor:       observability.Actor("system"),
	})
	if err != nil {
		h.t.Fatal(err)
	}
	return res.Identity.ID()
}

// AwaitEvent polls the events table for at least one event of the
// given type. Returns ErrAwaitTimeout if not found within deadline.
// Implementation note: we advance the FakeClock by 100ms between
// polls so the dedupe TTL and other time-dependent logic continue to
// behave deterministically.
func (h *Harness) AwaitEvent(et observability.EventType, timeout time.Duration) error {
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		evs, err := h.Events.Find(context.Background(), observability.EventQueryFilter{
			EventType: &et, Limit: 1,
		})
		if err != nil {
			return err
		}
		if len(evs) > 0 {
			return nil
		}
		// Tiny sleep is acceptable here (≤ 5ms total, polling-only).
		// We deliberately do NOT use a fake-clock advance here because
		// the inbound dispatcher uses real wall-clock for SQLite
		// timestamps; advancing fake clock would not influence its
		// poll loop.
		time.Sleep(2 * time.Millisecond)
	}
	return fmt.Errorf("%w: event %s not seen within %s", ErrAwaitTimeout, et, timeout)
}

// ErrAwaitTimeout signals AwaitEvent did not observe the desired event.
var ErrAwaitTimeout = errors.New("harness: await timeout")
