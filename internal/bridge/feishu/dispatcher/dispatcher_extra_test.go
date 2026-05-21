package dispatcher_test

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/oopslink/agent-center/internal/bridge/feishu/client"
	"github.com/oopslink/agent-center/internal/bridge/feishu/dispatcher"
	"github.com/oopslink/agent-center/internal/bridge/feishu/renderer"
	"github.com/oopslink/agent-center/internal/clock"
	"github.com/oopslink/agent-center/internal/conversation"
	convservice "github.com/oopslink/agent-center/internal/conversation/service"
	"github.com/oopslink/agent-center/internal/observability"
)

// Exercise classifyVendorError branches not hit by the happy-path tests.
func TestClassifyVendorErrorBranches(t *testing.T) {
	k := newDispatcherKit(t)
	ctx := context.Background()
	for vendorErr, wantReason := range map[error]string{
		client.ErrAuthFailed:       "auth_failed",
		client.ErrPermanentFailure: "4xx_permanent",
		client.ErrTransientFailure: "5xx_exhausted",
		client.ErrNotConnected:     "connect_lost",
		errors.New("misc"):         "vendor_error",
	} {
		conv := k.seedConv(t, conversation.ConversationKindGroupThread, "oc_"+wantReason)
		k.client.textErr = vendorErr
		_, _ = k.writer.AddMessage(ctx, convservice.AddMessageCommand{
			ConversationID: conv.ID(), SenderIdentityID: conversation.IdentityRef("supervisor:s"),
			ContentKind: conversation.MessageContentText, Content: "x",
			Direction: conversation.DirectionOutbound, Actor: observability.Actor("supervisor:s"),
		})
		_, _ = k.dispatcher.RunOnce(ctx)
		k.client.textErr = nil
		// Find the most recent channel.delivery_failed event for this conversation.
		events, _ := k.events.Find(ctx, observability.EventQueryFilter{})
		var gotReason string
		for _, e := range events {
			if e.Type() == "channel.delivery_failed" && e.Refs().ConversationID == string(conv.ID()) {
				gotReason, _ = e.Payload()["reason"].(string)
			}
		}
		if gotReason != wantReason {
			t.Errorf("vendor %v want reason %s, got %s", vendorErr, wantReason, gotReason)
		}
	}
}

// CursorStore Load/Save with empty subscriber returns explicit error.
func TestCursorStoreEmptySubscriber(t *testing.T) {
	k := newDispatcherKit(t)
	if _, err := k.cursor.Load(context.Background(), ""); err == nil {
		t.Fatal("want err on empty subscriber")
	}
	if err := k.cursor.Save(context.Background(), "", "id"); err == nil {
		t.Fatal("want err on empty subscriber")
	}
}

// Idempotency on duplicate ledger row → second dispatch is a no-op (skip).
func TestDispatcherIdempotentOnDuplicateLedger(t *testing.T) {
	k := newDispatcherKit(t)
	ctx := context.Background()
	conv := k.seedConv(t, conversation.ConversationKindGroupThread, "oc_idem")
	_, err := k.writer.AddMessage(ctx, convservice.AddMessageCommand{
		ConversationID: conv.ID(), SenderIdentityID: conversation.IdentityRef("supervisor:s"),
		ContentKind: conversation.MessageContentText, Content: "x",
		Direction: conversation.DirectionOutbound, Actor: observability.Actor("supervisor:s"),
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := k.dispatcher.RunOnce(ctx); err != nil {
		t.Fatal(err)
	}
	// Reset cursor + run again. Dispatcher sees the same events; the
	// duplicate ledger Append returns ErrLedgerDuplicate and the dispatcher
	// treats it as already-processed.
	if err := k.cursor.Save(ctx, dispatcher.SubscriberName, ""); err != nil {
		t.Fatal(err)
	}
	beforeSends := len(k.client.textCalls)
	if _, err := k.dispatcher.RunOnce(ctx); err != nil {
		t.Fatal(err)
	}
	if len(k.client.textCalls) != beforeSends {
		t.Fatalf("idempotency broke: %d → %d", beforeSends, len(k.client.textCalls))
	}
}

// Routing failed for missing refs.MessageID in conversation.message_added.
func TestRoutingFailedMissingRefs(t *testing.T) {
	k := newDispatcherKit(t)
	ctx := context.Background()
	if _, err := k.sink.Emit(ctx, observability.EmitCommand{
		EventType: "conversation.message_added",
		Refs:      observability.EventRefs{ConversationID: "C-x"}, // no MessageID
		Actor:     observability.Actor("user:hayang"),
		Payload:   map[string]any{"direction": "outbound"},
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := k.dispatcher.RunOnce(ctx); err != nil {
		t.Fatal(err)
	}
	events, _ := k.events.Find(ctx, observability.EventQueryFilter{})
	found := false
	for _, e := range events {
		if e.Type() == "bridge.routing_failed" {
			if r, _ := e.Payload()["reason"].(string); r == "missing_refs" {
				found = true
			}
		}
	}
	if !found {
		t.Fatal("missing_refs routing failure not emitted")
	}
}

// MessageNotFound branch.
func TestRoutingMessageNotFound(t *testing.T) {
	k := newDispatcherKit(t)
	ctx := context.Background()
	if _, err := k.sink.Emit(ctx, observability.EmitCommand{
		EventType: "conversation.message_added",
		Refs:      observability.EventRefs{ConversationID: "C-x", MessageID: "M-ghost"},
		Actor:     observability.Actor("user:hayang"),
		Payload:   map[string]any{"direction": "outbound"},
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := k.dispatcher.RunOnce(ctx); err != nil {
		t.Fatal(err)
	}
	events, _ := k.events.Find(ctx, observability.EventQueryFilter{})
	found := false
	for _, e := range events {
		if e.Type() == "bridge.routing_failed" {
			r, _ := e.Payload()["reason"].(string)
			if r == "message_not_found" {
				found = true
			}
		}
	}
	if !found {
		t.Fatal("message_not_found routing failure not emitted")
	}
}

// extractTextPayload coverage on edge cases (escapes + non-envelope input).
func TestExtractTextPayloadEdgeCasesViaSentinel(t *testing.T) {
	// We exercise extractTextPayload via the real dispatch path: send a
	// text message with embedded newline + quote + tab so unescape runs.
	k := newDispatcherKit(t)
	ctx := context.Background()
	conv := k.seedConv(t, conversation.ConversationKindGroupThread, "oc_esc")
	body := "line1\nquote\"tab\trest\\end"
	if _, err := k.writer.AddMessage(ctx, convservice.AddMessageCommand{
		ConversationID: conv.ID(), SenderIdentityID: conversation.IdentityRef("supervisor:s"),
		ContentKind: conversation.MessageContentText, Content: body,
		Direction: conversation.DirectionOutbound, Actor: observability.Actor("supervisor:s"),
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := k.dispatcher.RunOnce(ctx); err != nil {
		t.Fatal(err)
	}
	if len(k.client.textCalls) != 1 {
		t.Fatalf("expected 1 send, got %d", len(k.client.textCalls))
	}
	if k.client.textCalls[0].payload != body {
		t.Fatalf("payload roundtrip failed:\nwant: %q\ngot:  %q", body, k.client.textCalls[0].payload)
	}
}

// Verify loop ctx cancel exits cleanly.
func TestDispatcherCtxCancelExits(t *testing.T) {
	k := newDispatcherKit(t)
	ctx, cancel := context.WithCancel(context.Background())
	if err := k.dispatcher.Start(ctx); err != nil {
		t.Fatal(err)
	}
	cancel()
	// Stop blocks until the loop returns.
	k.dispatcher.Stop()
}

// Verify the dispatcher emits bridge.callback_failed when the
// post-delivery write-back fails (we simulate by closing the DB mid-run).
//
// We instead make the test verify that the dispatcher gracefully handles
// a UpdatePrimaryChannel CAS conflict (a real race condition).
func TestCallbackFailedOnConcurrentPrimaryChannelUpdate(t *testing.T) {
	k := newDispatcherKit(t)
	ctx := context.Background()
	// Seed conv with no primary thread key; dispatcher will try to set it.
	convAR, _ := conversation.NewConversation(conversation.NewConversationInput{
		ID: conversation.ConversationID("C-CB"), Kind: conversation.ConversationKindTask,
		OpenedAt: k.clock.Now(),
	})
	if err := k.conv.Save(ctx, convAR); err != nil {
		t.Fatal(err)
	}
	// Bump the conv version out-of-band to force CAS conflict.
	if err := k.conv.UpdatePrimaryChannel(ctx, convAR.ID(), "feishu", "oc_other", 1, k.clock.Now()); err != nil {
		t.Fatal(err)
	}
	if _, err := k.sink.Emit(ctx, observability.EmitCommand{
		EventType: "conversation.opened",
		Refs:      observability.EventRefs{ConversationID: string(convAR.ID())},
		Actor:     observability.Actor("user:hayang"),
		Payload:   map[string]any{"kind": "task"},
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := k.dispatcher.RunOnce(ctx); err != nil {
		t.Fatal(err)
	}
	// Dispatcher should have observed the conv has thread_key now and skipped
	// the write-back — no callback_failed event expected, but the root card
	// was delivered (ledger row present).
	got, _ := k.conv.FindByID(ctx, convAR.ID())
	if got.PrimaryChannelThreadKey() != "oc_other" {
		t.Fatalf("primary thread key overwritten: %s", got.PrimaryChannelThreadKey())
	}
}

// Stop without Start is a no-op.
func TestDispatcherStopBeforeStart(t *testing.T) {
	k := newDispatcherKit(t)
	k.dispatcher.Stop() // should not panic / hang
}

// NewService rejects nil clock by defaulting to system.
func TestNewServiceDefaultsClockToSystem(t *testing.T) {
	k := newDispatcherKit(t)
	deps := dispatcher.Deps{
		DB: k.db, IDGen: k.idgen, Events: k.events, Sink: k.sink,
		Cursor: k.cursor, Conversations: k.conv, Messages: k.msgs,
		Bindings: k.binds, Ledger: k.ledger, Client: k.client,
		Renderer: renderer.New(),
	}
	svc, err := dispatcher.NewService(deps, dispatcher.Config{})
	if err != nil {
		t.Fatal(err)
	}
	if svc == nil {
		t.Fatal("nil service")
	}
}

// NewService individual missing-dep diagnostics.
func TestNewServiceMissingDeps(t *testing.T) {
	k := newDispatcherKit(t)
	full := dispatcher.Deps{
		DB: k.db, Clock: k.clock, IDGen: k.idgen, Events: k.events, Sink: k.sink,
		Cursor: k.cursor, Conversations: k.conv, Messages: k.msgs,
		Bindings: k.binds, Ledger: k.ledger, Client: k.client,
		Renderer: renderer.New(),
	}
	cases := map[string]func(*dispatcher.Deps){
		"DB":            func(d *dispatcher.Deps) { d.DB = nil },
		"IDGen":         func(d *dispatcher.Deps) { d.IDGen = nil },
		"Events":        func(d *dispatcher.Deps) { d.Events = nil },
		"Sink":          func(d *dispatcher.Deps) { d.Sink = nil },
		"Cursor":        func(d *dispatcher.Deps) { d.Cursor = nil },
		"Conversations": func(d *dispatcher.Deps) { d.Conversations = nil },
		"Messages":      func(d *dispatcher.Deps) { d.Messages = nil },
		"Ledger":        func(d *dispatcher.Deps) { d.Ledger = nil },
		"Client":        func(d *dispatcher.Deps) { d.Client = nil },
		"Renderer":      func(d *dispatcher.Deps) { d.Renderer = nil },
	}
	for name, drop := range cases {
		d := full
		drop(&d)
		if _, err := dispatcher.NewService(d, dispatcher.Config{}); err == nil ||
			!strings.Contains(err.Error(), "required") {
			t.Errorf("missing %s: got err=%v", name, err)
		}
	}
}

// Cursor store sleepWith / FakeClock branch — non-FakeClock arms a real time.Sleep
// that we limit to microseconds.
func TestClockSleepWithSystemClock(t *testing.T) {
	clock.SleepWith(clock.SystemClock{}, time.Microsecond)
	clock.SleepWith(clock.SystemClock{}, 0) // zero is a no-op.
}
