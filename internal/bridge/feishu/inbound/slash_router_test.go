package inbound_test

import (
	"context"
	"testing"

	"github.com/oopslink/agent-center/internal/bridge/feishu/inbound"
	"github.com/oopslink/agent-center/internal/conversation/identity"
	"github.com/oopslink/agent-center/internal/observability"
	trservice "github.com/oopslink/agent-center/internal/taskruntime/service"
)

func TestSlashRouter_Track_TaskExists(t *testing.T) {
	f := newFixture(t)
	user := f.seedUser(t, "hayang")

	// Seed task without conversation.
	res, err := f.taskSvc.Create(context.Background(), trservice.TaskCreateInput{
		ProjectID: "demo",
		Title:     "test",
		Actor:     observability.Actor("system"),
	})
	if err != nil {
		t.Fatalf("create task: %v", err)
	}
	cmd := &inbound.SlashCommand{
		Verb: inbound.SlashVerbTrack,
		Args: []string{string(res.TaskID)},
		Raw:  "/track " + string(res.TaskID),
	}
	dec, err := f.slash.Route(context.Background(), cmd, inbound.SlashRouteContext{
		IdentityID:      user,
		VendorThreadKey: "thread-1",
		MessageContext:  inbound.MessageContextDM,
		VendorMsgRef:    "ref-1",
	})
	if err != nil {
		t.Fatalf("route: %v", err)
	}
	if dec.Kind != inbound.RouteDecisionSlashRoute {
		t.Errorf("decision: %v", dec)
	}
	if !f.hasEvent(t, "bridge.slash_command_received") {
		t.Error("bridge.slash_command_received not emitted")
	}
}

func TestSlashRouter_Track_TaskNotFound(t *testing.T) {
	f := newFixture(t)
	user := f.seedUser(t, "hayang")
	cmd := &inbound.SlashCommand{
		Verb: inbound.SlashVerbTrack, Args: []string{"T-missing"}, Raw: "/track T-missing",
	}
	dec, err := f.slash.Route(context.Background(), cmd, inbound.SlashRouteContext{
		IdentityID: user, VendorThreadKey: "thread-x", MessageContext: inbound.MessageContextDM,
		VendorMsgRef: "ref-1",
	})
	if err != nil {
		t.Fatalf("route: %v", err)
	}
	if dec.Kind != inbound.RouteDecisionRejectSlash {
		t.Errorf("decision: %v", dec)
	}
	if dec.Reason != "task_not_found" {
		t.Errorf("reason: %s", dec.Reason)
	}
	if !f.hasEvent(t, "bridge.slash_command_rejected") {
		t.Error("rejected event not emitted")
	}
}

func TestSlashRouter_Track_MissingArgs(t *testing.T) {
	f := newFixture(t)
	user := f.seedUser(t, "hayang")
	cmd := &inbound.SlashCommand{
		Verb: inbound.SlashVerbTrack, Args: nil, Raw: "/track",
	}
	dec, _ := f.slash.Route(context.Background(), cmd, inbound.SlashRouteContext{
		IdentityID: user, VendorThreadKey: "thread-x", MessageContext: inbound.MessageContextDM,
	})
	if dec.Kind != inbound.RouteDecisionRejectSlash {
		t.Errorf("decision: %v", dec)
	}
}

func TestSlashRouter_Answer_IRNotFound(t *testing.T) {
	f := newFixture(t)
	user := f.seedUser(t, "hayang")
	cmd := &inbound.SlashCommand{
		Verb: inbound.SlashVerbAnswer, Args: []string{"I-missing", "B"},
		Raw: "/answer I-missing B",
	}
	dec, _ := f.slash.Route(context.Background(), cmd, inbound.SlashRouteContext{
		IdentityID: user, VendorThreadKey: "t", MessageContext: inbound.MessageContextDM,
	})
	if dec.Kind != inbound.RouteDecisionRejectSlash || dec.Reason != "input_request_not_found" {
		t.Errorf("decision: %v", dec)
	}
}

func TestSlashRouter_Answer_MissingArgs(t *testing.T) {
	f := newFixture(t)
	user := f.seedUser(t, "hayang")
	cmd := &inbound.SlashCommand{
		Verb: inbound.SlashVerbAnswer, Args: []string{"I-7"},
		Raw: "/answer I-7",
	}
	dec, _ := f.slash.Route(context.Background(), cmd, inbound.SlashRouteContext{
		IdentityID: user, VendorThreadKey: "t", MessageContext: inbound.MessageContextDM,
	})
	if dec.Kind != inbound.RouteDecisionRejectSlash {
		t.Errorf("decision: %v", dec)
	}
}

func TestSlashRouter_DispatchDeferred(t *testing.T) {
	f := newFixture(t)
	user := f.seedUser(t, "hayang")
	cmd := &inbound.SlashCommand{
		Verb: inbound.SlashVerbDispatch, Args: []string{"project=x"},
		Raw: "/dispatch project=x",
	}
	dec, _ := f.slash.Route(context.Background(), cmd, inbound.SlashRouteContext{
		IdentityID: user, VendorThreadKey: "t", MessageContext: inbound.MessageContextDM,
	})
	if dec.Kind != inbound.RouteDecisionRejectSlash || dec.Reason != "feature_deferred" {
		t.Errorf("decision: %v", dec)
	}
}

func TestSlashRouter_UnknownVerb(t *testing.T) {
	f := newFixture(t)
	user := f.seedUser(t, "hayang")
	cmd := &inbound.SlashCommand{
		Verb: "weird", Args: []string{"x"}, Raw: "/weird x",
	}
	dec, _ := f.slash.Route(context.Background(), cmd, inbound.SlashRouteContext{
		IdentityID: user, VendorThreadKey: "t", MessageContext: inbound.MessageContextDM,
	})
	if dec.Kind != inbound.RouteDecisionRejectSlash {
		t.Errorf("decision: %v", dec)
	}
}

func TestSlashRouter_NewSlashRouter_MissingDeps(t *testing.T) {
	_, err := inbound.NewSlashRouter(inbound.SlashRouterDeps{})
	if err == nil {
		t.Fatal("want missing-dep error")
	}
}

func TestSlashRouter_NilCommand(t *testing.T) {
	f := newFixture(t)
	_, err := f.slash.Route(context.Background(), nil, inbound.SlashRouteContext{})
	if err == nil {
		t.Fatal("want nil-cmd error")
	}
}

func TestSlashRouter_BadIdentity(t *testing.T) {
	f := newFixture(t)
	_, err := f.slash.Route(context.Background(), &inbound.SlashCommand{Verb: inbound.SlashVerbTrack},
		inbound.SlashRouteContext{IdentityID: identity.IdentityID("bogus:format")})
	if err == nil {
		t.Fatal("want bad-identity error")
	}
}

func TestSlashRouter_EphemeralReplierCalled(t *testing.T) {
	f := newFixture(t)
	user := f.seedUser(t, "hayang")
	fake := &fakeReplier{}
	router, err := inbound.NewSlashRouter(inbound.SlashRouterDeps{
		DB: f.db, Clock: f.clock, IDGen: f.idgen, Sink: f.sink,
		Tasks: f.tasks, Execs: f.execs, Convs: f.convs,
		TaskSvc: f.taskSvc, IRSvc: f.irSvc, IRRepo: f.irs,
		MsgWriter: f.msgWriter, Ephemeral: fake,
		Actor: observability.Actor("system"),
	})
	if err != nil {
		t.Fatal(err)
	}
	cmd := &inbound.SlashCommand{
		Verb: inbound.SlashVerbTrack, Args: []string{"T-none"}, Raw: "/track T-none",
	}
	router.Route(context.Background(), cmd, inbound.SlashRouteContext{
		IdentityID: user, VendorThreadKey: "t", MessageContext: inbound.MessageContextDM,
	})
	if fake.calls == 0 {
		t.Error("ephemeral replier was not invoked on reject")
	}
}

type fakeReplier struct{ calls int }

func (r *fakeReplier) ReplyEphemeral(ctx context.Context, t, u, m string) error {
	r.calls++
	return nil
}
