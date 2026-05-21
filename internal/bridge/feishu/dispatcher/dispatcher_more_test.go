package dispatcher_test

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/oopslink/agent-center/internal/bridge/feishu/dispatcher"
	"github.com/oopslink/agent-center/internal/bridge/feishu/renderer"
	"github.com/oopslink/agent-center/internal/conversation"
	"github.com/oopslink/agent-center/internal/conversation/identity"
	"github.com/oopslink/agent-center/internal/observability"
)

// IssueByConversation lookup hook coverage.
func TestIssueByConversationLookup(t *testing.T) {
	k := newDispatcherKit(t)
	ctx := context.Background()
	conv := k.seedConv(t, conversation.ConversationKindIssue, "oc_lk_issue")
	svc, err := dispatcher.NewService(dispatcher.Deps{
		DB: k.db, Clock: k.clock, IDGen: k.idgen, Events: k.events, Sink: k.sink,
		Cursor: k.cursor, Conversations: k.conv, Messages: k.msgs, Bindings: k.binds,
		Ledger: k.ledger, Client: k.client, Renderer: renderer.New(),
		IssueByConversation: func(ctx context.Context, _ conversation.ConversationID) (string, string, error) {
			return "Issue #7", "Need to discuss", nil
		},
	}, dispatcher.Config{PollInterval: 0, Channel: "feishu", Actor: observability.Actor("system")})
	if err != nil {
		t.Fatal(err)
	}
	k.emitOpened(t, conv)
	if _, err := svc.RunOnce(ctx); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(k.client.cardCalls[0].payload, "Issue #7") {
		t.Fatalf("subject ref missing: %s", k.client.cardCalls[0].payload)
	}
}

// FallbackTarget for actor with bound preferred channel.
func TestFallbackTargetUsesPreferredBinding(t *testing.T) {
	k := newDispatcherKit(t)
	ctx := context.Background()
	// Register + bind a preferred channel for the actor.
	if _, err := k.idents.FindByID(ctx, "user:hayang"); err != nil {
		// register via repo directly to avoid coupling to other tests.
		idObj, _ := identity.NewIdentity(identity.NewIdentityInput{
			ID: "user:hayang", Kind: identity.KindUser, DisplayName: "H", CreatedAt: k.clock.Now(),
		})
		if err := k.idents.Save(ctx, idObj); err != nil {
			t.Fatal(err)
		}
		bind, _ := identity.NewChannelBinding(identity.NewChannelBindingInput{
			ID: k.idgen.NewULID(), IdentityID: "user:hayang", Channel: "feishu",
			VendorUserID: "ou_fb", Preferred: true, BoundAt: k.clock.Now(),
		})
		if err := k.binds.Save(ctx, bind); err != nil {
			t.Fatal(err)
		}
	}
	// Seed a conversation WITHOUT thread_key → dispatcher falls back to binding.
	convAR, _ := conversation.NewConversation(conversation.NewConversationInput{
		ID: conversation.ConversationID("C-FB"), Kind: conversation.ConversationKindTask,
		OpenedAt: k.clock.Now(),
	})
	if err := k.conv.Save(ctx, convAR); err != nil {
		t.Fatal(err)
	}
	if _, err := k.sink.Emit(ctx, observability.EmitCommand{
		EventType: "conversation.opened",
		Refs:      observability.EventRefs{ConversationID: "C-FB"},
		Actor:     observability.Actor("user:hayang"),
		Payload:   map[string]any{"kind": "task"},
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := k.dispatcher.RunOnce(ctx); err != nil {
		t.Fatal(err)
	}
	if len(k.client.cardCalls) != 1 {
		t.Fatalf("expected 1 send, got %d", len(k.client.cardCalls))
	}
	// Vendor target should have populated VendorUserID via binding.
	if k.client.cardCalls[0].target.VendorUserID != "ou_fb" {
		t.Fatalf("target: %+v", k.client.cardCalls[0].target)
	}
}

// RunOnce returns underlying cursor errors.
func TestRunOnceCursorLoadErrorPropagates(t *testing.T) {
	k := newDispatcherKit(t)
	svc, err := dispatcher.NewService(dispatcher.Deps{
		DB: k.db, Clock: k.clock, IDGen: k.idgen, Events: k.events, Sink: k.sink,
		Cursor: sabotagedCursor{}, Conversations: k.conv, Messages: k.msgs,
		Bindings: k.binds, Ledger: k.ledger, Client: k.client, Renderer: renderer.New(),
	}, dispatcher.Config{Channel: "feishu", Actor: observability.Actor("system")})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := svc.RunOnce(context.Background()); err == nil {
		t.Fatal("want err")
	}
}

// Ensure poll interval default applies.
func TestNewServiceAppliesDefaults(t *testing.T) {
	k := newDispatcherKit(t)
	svc, err := dispatcher.NewService(dispatcher.Deps{
		DB: k.db, Clock: nil, IDGen: k.idgen, Events: k.events, Sink: k.sink,
		Cursor: k.cursor, Conversations: k.conv, Messages: k.msgs,
		Bindings: k.binds, Ledger: k.ledger, Client: k.client, Renderer: renderer.New(),
	}, dispatcher.Config{PollInterval: 0, BatchSize: 0, Channel: "", Actor: ""})
	if err != nil {
		t.Fatal(err)
	}
	if svc == nil {
		t.Fatal("nil svc")
	}
}

// Sleep helper to keep noise low.
var _ = time.Second
