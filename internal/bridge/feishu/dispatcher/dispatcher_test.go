package dispatcher_test

import (
	"context"
	"database/sql"
	"errors"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/oopslink/agent-center/internal/bridge/feishu/client"
	"github.com/oopslink/agent-center/internal/bridge/feishu/dispatcher"
	"github.com/oopslink/agent-center/internal/bridge/feishu/ledger"
	"github.com/oopslink/agent-center/internal/bridge/feishu/renderer"
	"github.com/oopslink/agent-center/internal/clock"
	"github.com/oopslink/agent-center/internal/conversation"
	convsqlite "github.com/oopslink/agent-center/internal/conversation/sqlite"
	convservice "github.com/oopslink/agent-center/internal/conversation/service"
	"github.com/oopslink/agent-center/internal/conversation/identity"
	"github.com/oopslink/agent-center/internal/idgen"
	"github.com/oopslink/agent-center/internal/observability"
	obsqlite "github.com/oopslink/agent-center/internal/observability/sqlite"
	"github.com/oopslink/agent-center/internal/persistence"
	"github.com/oopslink/agent-center/internal/taskruntime"
	"github.com/oopslink/agent-center/internal/taskruntime/inputrequest"
	trsqlite "github.com/oopslink/agent-center/internal/taskruntime/sqlite"
	tasktype "github.com/oopslink/agent-center/internal/taskruntime/task"
)

// fakeClient is an in-process Client implementation for dispatcher tests.
type fakeClient struct {
	mu sync.Mutex

	textCalls []sendCall
	cardCalls []sendCall

	textErr error
	cardErr error
	// Per-call queued errors override the sticky textErr/cardErr.
	queue []error

	nextRef int
}

type sendCall struct {
	target  client.Target
	payload string
}

func (f *fakeClient) Connect(ctx context.Context) error { return nil }

func (f *fakeClient) SendTextMessage(ctx context.Context, t client.Target, body string) (client.SendResult, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if err := f.pickErr(); err != nil {
		return client.SendResult{}, err
	}
	if f.textErr != nil {
		return client.SendResult{}, f.textErr
	}
	f.textCalls = append(f.textCalls, sendCall{t, body})
	f.nextRef++
	return client.SendResult{
		VendorMsgRef: "vm_" + ulidNum(f.nextRef),
		ThreadKey:    chooseThread(t, f.nextRef),
	}, nil
}

func (f *fakeClient) SendInteractiveCard(ctx context.Context, t client.Target, cardJSON string) (client.SendResult, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if err := f.pickErr(); err != nil {
		return client.SendResult{}, err
	}
	if f.cardErr != nil {
		return client.SendResult{}, f.cardErr
	}
	f.cardCalls = append(f.cardCalls, sendCall{t, cardJSON})
	f.nextRef++
	return client.SendResult{
		VendorMsgRef:  "vm_" + ulidNum(f.nextRef),
		CardMessageID: "card_" + ulidNum(f.nextRef),
		ThreadKey:     chooseThread(t, f.nextRef),
	}, nil
}

func (f *fakeClient) UpdateCard(ctx context.Context, cardMessageID, cardJSON string) error {
	return client.ErrUpdateCardNotSupported
}
func (f *fakeClient) Close() error                          { return nil }
func (f *fakeClient) ConnectionStatus() client.ConnectionStatus { return client.StatusConnected }
func (f *fakeClient) OnEvent(handler func(client.VendorEvent)) {}

func (f *fakeClient) pickErr() error {
	if len(f.queue) > 0 {
		err := f.queue[0]
		f.queue = f.queue[1:]
		return err
	}
	return nil
}

func chooseThread(t client.Target, n int) string {
	if t.ThreadKey != "" {
		return t.ThreadKey
	}
	return "oc_fake_" + ulidNum(n)
}

func ulidNum(n int) string {
	const c = "0123456789ABCDEFGHIJKLMNOPQRSTUV"
	out := make([]byte, 6)
	for i := 5; i >= 0; i-- {
		out[i] = c[n%len(c)]
		n /= len(c)
	}
	return string(out)
}

// dispatcherKit groups all collaborators.
type dispatcherKit struct {
	t          *testing.T
	db         *sql.DB
	clock      *clock.FakeClock
	idgen      idgen.Generator
	events     *obsqlite.EventRepo
	sink       *observability.EventSink
	conv       *convsqlite.ConversationRepo
	msgs       *convsqlite.MessageRepo
	idents     *identity.SQLiteIdentityRepo
	binds      *identity.SQLiteChannelBindingRepo
	ledger     *ledger.SQLiteRepo
	cursor     *dispatcher.SQLiteCursorStore
	irRepo     *trsqlite.InputRequestRepo
	client     *fakeClient
	dispatcher *dispatcher.Service
	writer     *convservice.MessageWriter
}

func newDispatcherKit(t *testing.T) *dispatcherKit {
	t.Helper()
	path := t.TempDir() + "/dispatcher.db"
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
	conv := convsqlite.NewConversationRepo(db)
	msgs := convsqlite.NewMessageRepo(db)
	idents := identity.NewSQLiteIdentityRepo(db)
	binds := identity.NewSQLiteChannelBindingRepo(db)
	ledg := ledger.NewSQLiteRepo(db, fc)
	cursor := dispatcher.NewSQLiteCursorStore(db, fc)
	irRepo := trsqlite.NewInputRequestRepo(db)
	fakeCli := &fakeClient{}
	writer := convservice.NewMessageWriter(db, conv, msgs, sink, gen, fc)

	svc, err := dispatcher.NewService(dispatcher.Deps{
		DB: db, Clock: fc, IDGen: gen, Events: er, Sink: sink, Cursor: cursor,
		Conversations: conv, Messages: msgs, Bindings: binds, InputRequests: irRepo,
		Ledger: ledg, Client: fakeCli, Renderer: renderer.New(),
	}, dispatcher.Config{
		PollInterval: 5 * time.Millisecond,
		BatchSize:    50,
		Channel:      "feishu",
		Actor:        observability.Actor("system"),
	})
	if err != nil {
		t.Fatal(err)
	}
	return &dispatcherKit{
		t: t, db: db, clock: fc, idgen: gen, events: er, sink: sink,
		conv: conv, msgs: msgs, idents: idents, binds: binds,
		ledger: ledg, cursor: cursor, irRepo: irRepo,
		client: fakeCli, dispatcher: svc, writer: writer,
	}
}

func (k *dispatcherKit) seedConv(t *testing.T, kind conversation.ConversationKind, threadKey string) *conversation.Conversation {
	t.Helper()
	conv, err := conversation.NewConversation(conversation.NewConversationInput{
		ID: conversation.ConversationID(k.idgen.NewULID()),
		Kind: kind, Title: "Some " + string(kind),
		PrimaryChannelHint:      "",
		PrimaryChannelThreadKey: threadKey,
		OpenedAt:                k.clock.Now(),
	})
	if err != nil {
		t.Fatal(err)
	}
	if threadKey != "" {
		if err := conv.SetPrimaryChannel("feishu", threadKey, k.clock.Now()); err != nil {
			t.Fatal(err)
		}
	}
	if err := k.conv.Save(context.Background(), conv); err != nil {
		t.Fatal(err)
	}
	return conv
}

func (k *dispatcherKit) emitOpened(t *testing.T, conv *conversation.Conversation) {
	t.Helper()
	_, err := k.sink.Emit(context.Background(), observability.EmitCommand{
		EventType: "conversation.opened",
		Refs:      observability.EventRefs{ConversationID: string(conv.ID())},
		Actor:     observability.Actor("user:hayang"),
		Payload: map[string]any{
			"conversation_id": string(conv.ID()),
			"kind":            string(conv.Kind()),
		},
	})
	if err != nil {
		t.Fatal(err)
	}
}

func TestRouteConversationOpenedTask(t *testing.T) {
	k := newDispatcherKit(t)
	ctx := context.Background()
	conv := k.seedConv(t, conversation.ConversationKindTask, "oc_thread_1")
	k.emitOpened(t, conv)
	if _, err := k.dispatcher.RunOnce(ctx); err != nil {
		t.Fatal(err)
	}
	if len(k.client.cardCalls) != 1 {
		t.Fatalf("expected 1 card call, got %d", len(k.client.cardCalls))
	}
	rec, err := k.ledger.FindByMessageID(ctx, "root-card:"+string(conv.ID()))
	if err != nil || rec.Status() != ledger.StatusDelivered {
		t.Fatalf("ledger not delivered: %+v err=%v", rec, err)
	}
	// channel.delivered event present.
	events, _ := k.events.Find(ctx, observability.EventQueryFilter{})
	hasDelivered := false
	for _, e := range events {
		if e.Type() == "channel.delivered" && e.Refs().ConversationID == string(conv.ID()) {
			hasDelivered = true
		}
	}
	if !hasDelivered {
		t.Fatal("channel.delivered missing")
	}
}

func TestRouteConversationOpenedIssue(t *testing.T) {
	k := newDispatcherKit(t)
	ctx := context.Background()
	conv := k.seedConv(t, conversation.ConversationKindIssue, "oc_thread_2")
	k.emitOpened(t, conv)
	if _, err := k.dispatcher.RunOnce(ctx); err != nil {
		t.Fatal(err)
	}
	if len(k.client.cardCalls) != 1 {
		t.Fatalf("expected 1 issue root card, got %d", len(k.client.cardCalls))
	}
	if !strings.Contains(k.client.cardCalls[0].payload, "issue") {
		t.Fatalf("issue label missing: %s", k.client.cardCalls[0].payload)
	}
}

func TestRouteConversationOpenedSkipsNonTaskKind(t *testing.T) {
	k := newDispatcherKit(t)
	ctx := context.Background()
	conv := k.seedConv(t, conversation.ConversationKindDM, "oc_dm")
	k.emitOpened(t, conv)
	if _, err := k.dispatcher.RunOnce(ctx); err != nil {
		t.Fatal(err)
	}
	if len(k.client.cardCalls) != 0 {
		t.Fatalf("dm should be skipped; sent %d", len(k.client.cardCalls))
	}
}

func TestRouteMessageAddedOutboundText(t *testing.T) {
	k := newDispatcherKit(t)
	ctx := context.Background()
	conv := k.seedConv(t, conversation.ConversationKindGroupThread, "oc_thread_3")
	// Use the writer so message + event land in same tx.
	res, err := k.writer.AddMessage(ctx, convservice.AddMessageCommand{
		ConversationID: conv.ID(), SenderIdentityID: conversation.IdentityRef("supervisor:s-1"),
		ContentKind: conversation.MessageContentText, Content: "hi from supervisor",
		Direction: conversation.DirectionOutbound,
		Actor:     observability.Actor("supervisor:s-1"),
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := k.dispatcher.RunOnce(ctx); err != nil {
		t.Fatal(err)
	}
	if len(k.client.textCalls) != 1 || k.client.textCalls[0].payload != "hi from supervisor" {
		t.Fatalf("text call mismatch %+v", k.client.textCalls)
	}
	// vendor_msg_ref backfilled.
	msg, _ := k.msgs.FindByID(ctx, res.MessageID)
	if msg.VendorMsgRef() == "" {
		t.Fatal("vendor_msg_ref not backfilled")
	}
}

func TestRouteMessageAddedAgentFindingWithInputRequest(t *testing.T) {
	k := newDispatcherKit(t)
	ctx := context.Background()
	conv := k.seedConv(t, conversation.ConversationKindTask, "oc_thread_ir")
	// Seed an InputRequest.
	ir, err := inputrequest.New(inputrequest.NewInput{
		ID:              taskruntime.InputRequestID("IR-1"),
		TaskExecutionID: taskruntime.TaskExecutionID("E-1"),
		Question:        "Pick one",
		Options:         []string{"A", "B", "C"},
		Now:             k.clock.Now(),
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := k.irRepo.Save(ctx, ir); err != nil {
		t.Fatal(err)
	}
	_, err = k.writer.AddMessage(ctx, convservice.AddMessageCommand{
		ConversationID: conv.ID(), SenderIdentityID: conversation.IdentityRef("agent:a-1"),
		ContentKind: conversation.MessageContentAgentFinding,
		Content:     "needs your call",
		InputRequestRef: "IR-1",
		Direction:   conversation.DirectionOutbound,
		Actor:       observability.Actor("agent:a-1"),
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := k.dispatcher.RunOnce(ctx); err != nil {
		t.Fatal(err)
	}
	if len(k.client.cardCalls) != 1 {
		t.Fatalf("expected 1 card call, got %d", len(k.client.cardCalls))
	}
	if !strings.Contains(k.client.cardCalls[0].payload, "input_request_respond") {
		t.Fatalf("buttons missing: %s", k.client.cardCalls[0].payload)
	}
}

func TestRouteMessageAddedAgentFindingMissingInputRequest(t *testing.T) {
	k := newDispatcherKit(t)
	ctx := context.Background()
	conv := k.seedConv(t, conversation.ConversationKindTask, "oc_x")
	_, err := k.writer.AddMessage(ctx, convservice.AddMessageCommand{
		ConversationID: conv.ID(), SenderIdentityID: conversation.IdentityRef("agent:a-1"),
		ContentKind: conversation.MessageContentAgentFinding,
		Content:     "needs your call",
		InputRequestRef: "IR-ghost",
		Direction:   conversation.DirectionOutbound,
		Actor:       observability.Actor("agent:a-1"),
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := k.dispatcher.RunOnce(ctx); err != nil {
		t.Fatal(err)
	}
	// channel.delivery_failed should fire with reason=input_request_not_found.
	events, _ := k.events.Find(ctx, observability.EventQueryFilter{})
	found := false
	for _, e := range events {
		if e.Type() == "channel.delivery_failed" {
			reason, _ := e.Payload()["reason"].(string)
			if reason == "input_request_not_found" {
				found = true
			}
		}
	}
	if !found {
		t.Fatal("delivery_failed missing")
	}
}

func TestRouteSkipsInboundMessages(t *testing.T) {
	k := newDispatcherKit(t)
	ctx := context.Background()
	conv := k.seedConv(t, conversation.ConversationKindGroupThread, "oc_in")
	_, err := k.writer.AddMessage(ctx, convservice.AddMessageCommand{
		ConversationID: conv.ID(), SenderIdentityID: conversation.IdentityRef("user:hayang"),
		ContentKind: conversation.MessageContentText, Content: "user typed",
		Direction: conversation.DirectionInbound,
		Actor:     observability.Actor("user:hayang"),
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := k.dispatcher.RunOnce(ctx); err != nil {
		t.Fatal(err)
	}
	if len(k.client.textCalls) != 0 {
		t.Fatal("inbound should not be sent")
	}
}

func TestRouteInputRequestEventsEmitAudit(t *testing.T) {
	k := newDispatcherKit(t)
	ctx := context.Background()
	for _, et := range []observability.EventType{"input_request.responded", "input_request.timed_out", "input_request.canceled"} {
		if _, err := k.sink.Emit(ctx, observability.EmitCommand{
			EventType: et,
			Refs:      observability.EventRefs{InputRequestID: "IR-x"},
			Actor:     observability.Actor("system"),
			Payload:   map[string]any{},
		}); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := k.dispatcher.RunOnce(ctx); err != nil {
		t.Fatal(err)
	}
	events, _ := k.events.Find(ctx, observability.EventQueryFilter{})
	channelDeliveredAudit := 0
	for _, e := range events {
		if e.Type() == "channel.delivered" {
			if k, _ := e.Payload()["audit_kind"].(string); k != "" {
				channelDeliveredAudit++
			}
		}
	}
	if channelDeliveredAudit != 3 {
		t.Fatalf("expected 3 audit deliveries, got %d", channelDeliveredAudit)
	}
}

func TestDeliveryFailedTransientExhausted(t *testing.T) {
	k := newDispatcherKit(t)
	ctx := context.Background()
	conv := k.seedConv(t, conversation.ConversationKindGroupThread, "oc_fail")
	k.client.textErr = client.ErrTransientFailure
	_, _ = k.writer.AddMessage(ctx, convservice.AddMessageCommand{
		ConversationID: conv.ID(), SenderIdentityID: conversation.IdentityRef("supervisor:s-1"),
		ContentKind: conversation.MessageContentText, Content: "x",
		Direction: conversation.DirectionOutbound,
		Actor:     observability.Actor("supervisor:s-1"),
	})
	if _, err := k.dispatcher.RunOnce(ctx); err != nil {
		t.Fatal(err)
	}
	events, _ := k.events.Find(ctx, observability.EventQueryFilter{})
	gotReason := ""
	for _, e := range events {
		if e.Type() == "channel.delivery_failed" {
			gotReason, _ = e.Payload()["reason"].(string)
		}
	}
	if gotReason != "5xx_exhausted" {
		t.Fatalf("want reason=5xx_exhausted, got %q", gotReason)
	}
}

func TestDeliveryFailedPermanent(t *testing.T) {
	k := newDispatcherKit(t)
	ctx := context.Background()
	conv := k.seedConv(t, conversation.ConversationKindGroupThread, "oc_perm")
	k.client.textErr = client.ErrPermanentFailure
	_, _ = k.writer.AddMessage(ctx, convservice.AddMessageCommand{
		ConversationID: conv.ID(), SenderIdentityID: conversation.IdentityRef("supervisor:s-1"),
		ContentKind: conversation.MessageContentText, Content: "x",
		Direction: conversation.DirectionOutbound,
		Actor:     observability.Actor("supervisor:s-1"),
	})
	_, _ = k.dispatcher.RunOnce(ctx)
	events, _ := k.events.Find(ctx, observability.EventQueryFilter{})
	gotReason := ""
	for _, e := range events {
		if e.Type() == "channel.delivery_failed" {
			gotReason, _ = e.Payload()["reason"].(string)
		}
	}
	if gotReason != "4xx_permanent" {
		t.Fatalf("want reason=4xx_permanent, got %q", gotReason)
	}
}

func TestRoutingFailedConversationMissing(t *testing.T) {
	k := newDispatcherKit(t)
	ctx := context.Background()
	if _, err := k.sink.Emit(ctx, observability.EmitCommand{
		EventType: "conversation.opened",
		Refs:      observability.EventRefs{ConversationID: "C-ghost"},
		Actor:     observability.Actor("user:hayang"),
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := k.dispatcher.RunOnce(ctx); err != nil {
		t.Fatal(err)
	}
	events, _ := k.events.Find(ctx, observability.EventQueryFilter{})
	hasRoutingFailed := false
	for _, e := range events {
		if e.Type() == "bridge.routing_failed" {
			hasRoutingFailed = true
		}
	}
	if !hasRoutingFailed {
		t.Fatal("bridge.routing_failed missing")
	}
}

func TestCursorPersistsAcrossRunOnce(t *testing.T) {
	k := newDispatcherKit(t)
	ctx := context.Background()
	conv := k.seedConv(t, conversation.ConversationKindTask, "oc_thread_persist")
	k.emitOpened(t, conv)
	if _, err := k.dispatcher.RunOnce(ctx); err != nil {
		t.Fatal(err)
	}
	first := k.client.cardCalls
	// Second run with no new events: cursor advances so no further sends.
	if _, err := k.dispatcher.RunOnce(ctx); err != nil {
		t.Fatal(err)
	}
	if len(k.client.cardCalls) != len(first) {
		t.Fatalf("dispatcher re-sent: %d vs %d", len(k.client.cardCalls), len(first))
	}
	cur, err := k.cursor.Load(ctx, dispatcher.SubscriberName)
	if err != nil || cur == "" {
		t.Fatalf("cursor not persisted: %q err=%v", cur, err)
	}
}

func TestDispatchLoopEventEmitsLoopFailure(t *testing.T) {
	k := newDispatcherKit(t)
	// Replace cursor store with a sabotaged one to force RunOnce err.
	k.dispatcher.Stop()
	svc, err := dispatcher.NewService(dispatcher.Deps{
		DB: k.db, Clock: k.clock, IDGen: k.idgen, Events: k.events, Sink: k.sink,
		Cursor: sabotagedCursor{},
		Conversations: k.conv, Messages: k.msgs, Bindings: k.binds, Ledger: k.ledger,
		Client: k.client, Renderer: renderer.New(),
	}, dispatcher.Config{PollInterval: time.Millisecond, Channel: "feishu", Actor: observability.Actor("system")})
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	if err := svc.Start(ctx); err != nil {
		t.Fatal(err)
	}
	// Let the loop iterate at least once, then stop.
	waitFor(t, 500*time.Millisecond, func() bool {
		events, _ := k.events.Find(ctx, observability.EventQueryFilter{})
		for _, e := range events {
			if e.Type() == "bridge.feishu.dispatch_loop_failed" {
				return true
			}
		}
		return false
	})
	svc.Stop()
}

type sabotagedCursor struct{}

func (sabotagedCursor) Load(ctx context.Context, subscriber string) (string, error) {
	return "", errors.New("sabotage: cursor unavailable")
}
func (sabotagedCursor) Save(ctx context.Context, subscriber, lastEventID string) error {
	return errors.New("sabotage")
}

func waitFor(t *testing.T, deadline time.Duration, check func() bool) {
	t.Helper()
	timeout := time.NewTimer(deadline)
	defer timeout.Stop()
	tick := time.NewTicker(5 * time.Millisecond)
	defer tick.Stop()
	for {
		if check() {
			return
		}
		select {
		case <-timeout.C:
			t.Fatalf("condition not met within %s", deadline)
		case <-tick.C:
		}
	}
}

func TestNewServiceValidatesDeps(t *testing.T) {
	t.Parallel()
	d := dispatcher.Deps{}
	if _, err := dispatcher.NewService(d, dispatcher.Config{}); err == nil {
		t.Fatal("want err on empty deps")
	}
}

func TestStartStopIdempotent(t *testing.T) {
	k := newDispatcherKit(t)
	ctx := context.Background()
	if err := k.dispatcher.Start(ctx); err != nil {
		t.Fatal(err)
	}
	if err := k.dispatcher.Start(ctx); err != nil {
		t.Fatalf("re-Start should be idempotent: %v", err)
	}
	k.dispatcher.Stop()
	k.dispatcher.Stop()
}

func TestTaskByConversationLookup(t *testing.T) {
	k := newDispatcherKit(t)
	ctx := context.Background()
	conv := k.seedConv(t, conversation.ConversationKindTask, "oc_lk")
	// Rebuild dispatcher with TaskByConversation hook.
	svc, err := dispatcher.NewService(dispatcher.Deps{
		DB: k.db, Clock: k.clock, IDGen: k.idgen, Events: k.events, Sink: k.sink,
		Cursor: k.cursor, Conversations: k.conv, Messages: k.msgs, Bindings: k.binds,
		Ledger: k.ledger, Client: k.client, Renderer: renderer.New(),
		TaskByConversation: func(ctx context.Context, _ conversation.ConversationID) (string, string, error) {
			return "Task #42", "Test task", nil
		},
	}, dispatcher.Config{PollInterval: 0, Channel: "feishu", Actor: observability.Actor("system")})
	if err != nil {
		t.Fatal(err)
	}
	k.emitOpened(t, conv)
	if _, err := svc.RunOnce(ctx); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(k.client.cardCalls[0].payload, "Task #42") {
		t.Fatalf("subject ref missing: %s", k.client.cardCalls[0].payload)
	}
}

// Silence unused imports for the tasktype import (used only for type
// hints in helper functions; keep here for future test growth).
var _ = tasktype.ErrTaskNotFound
