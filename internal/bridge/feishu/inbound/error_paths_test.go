package inbound_test

import (
	"context"
	"errors"
	"testing"

	"github.com/oopslink/agent-center/internal/bridge/feishu/inbound"
	"github.com/oopslink/agent-center/internal/conversation"
	"github.com/oopslink/agent-center/internal/conversation/identity"
	"github.com/oopslink/agent-center/internal/observability"
	"github.com/oopslink/agent-center/internal/taskruntime"
	"github.com/oopslink/agent-center/internal/taskruntime/execution"
	"github.com/oopslink/agent-center/internal/taskruntime/inputrequest"
	trservice "github.com/oopslink/agent-center/internal/taskruntime/service"
	"github.com/oopslink/agent-center/internal/taskruntime/task"
)

// fakeTaskRepo lets us inject lookup errors.
type fakeTaskRepo struct {
	task.Repository
	findErr error
}

func (f *fakeTaskRepo) FindByID(ctx context.Context, id taskruntime.TaskID) (*task.Task, error) {
	if f.findErr != nil {
		return nil, f.findErr
	}
	return f.Repository.FindByID(ctx, id)
}

// fakeConvRepo lets us inject ErrConversation* errors.
type fakeConvRepo struct {
	conversation.ConversationRepository
	findErr error
}

func (f *fakeConvRepo) FindByChannelAndThreadKey(ctx context.Context, ch, key string) (*conversation.Conversation, error) {
	if f.findErr != nil {
		return nil, f.findErr
	}
	return f.ConversationRepository.FindByChannelAndThreadKey(ctx, ch, key)
}

// fakeIdentRepo lets us inject errors on Find (identities listing).
type fakeIdentRepo struct {
	identity.IdentityRepository
	findErr error
}

func (f *fakeIdentRepo) Find(ctx context.Context, filter identity.IdentityFilter) ([]*identity.Identity, error) {
	if f.findErr != nil {
		return nil, f.findErr
	}
	return f.IdentityRepository.Find(ctx, filter)
}

// fakeIRRepo lets us inject lookup errors on InputRequest.FindByID.
type fakeIRRepo struct {
	inputrequest.Repository
	findErr error
}

func (f *fakeIRRepo) FindByID(ctx context.Context, id taskruntime.InputRequestID) (*inputrequest.InputRequest, error) {
	if f.findErr != nil {
		return nil, f.findErr
	}
	return f.Repository.FindByID(ctx, id)
}

// fakeExecRepo lets us inject lookup errors on TaskExecution.FindByID.
type fakeExecRepo struct {
	execution.Repository
	findErr error
}

func (f *fakeExecRepo) FindByID(ctx context.Context, id taskruntime.TaskExecutionID) (*execution.TaskExecution, error) {
	if f.findErr != nil {
		return nil, f.findErr
	}
	return f.Repository.FindByID(ctx, id)
}

// TestSlashRouter_TrackTaskLookupError covers the non-NotFound error
// path in routeTrack.
func TestSlashRouter_TrackTaskLookupError(t *testing.T) {
	f := newFixture(t)
	user := f.seedUser(t, "hayang")
	ftr := &fakeTaskRepo{Repository: f.tasks, findErr: errors.New("db down")}
	sr, err := inbound.NewSlashRouter(inbound.SlashRouterDeps{
		DB: f.db, Clock: f.clock, IDGen: f.idgen, Sink: f.sink,
		Tasks: ftr, Execs: f.execs, Convs: f.convs,
		TaskSvc: f.taskSvc, IRSvc: f.irSvc, IRRepo: f.irs,
		MsgWriter: f.msgWriter, Actor: observability.Actor("system"),
	})
	if err != nil {
		t.Fatal(err)
	}
	cmd := &inbound.SlashCommand{Verb: inbound.SlashVerbTrack, Args: []string{"T-1"}, Raw: "/track T-1"}
	_, err = sr.Route(context.Background(), cmd, inbound.SlashRouteContext{
		IdentityID: user, VendorThreadKey: "t", MessageContext: inbound.MessageContextDM,
	})
	if err == nil {
		t.Fatal("want db down error")
	}
}

// TestSlashRouter_FindOrCreateError covers the
// findOrCreateThreadConversation error path.
func TestSlashRouter_FindOrCreateError(t *testing.T) {
	f := newFixture(t)
	user := f.seedUser(t, "hayang")
	// Seed a task so the lookup succeeds; then have convs error on
	// FindByChannelAndThreadKey.
	tres, _ := f.taskSvc.Create(context.Background(), trservice.TaskCreateInput{
		ProjectID: "demo", Title: "test", Actor: observability.Actor("system"),
	})
	fcr := &fakeConvRepo{ConversationRepository: f.convs, findErr: errors.New("conv db down")}
	sr, err := inbound.NewSlashRouter(inbound.SlashRouterDeps{
		DB: f.db, Clock: f.clock, IDGen: f.idgen, Sink: f.sink,
		Tasks: f.tasks, Execs: f.execs, Convs: fcr,
		TaskSvc: f.taskSvc, IRSvc: f.irSvc, IRRepo: f.irs,
		MsgWriter: f.msgWriter, Actor: observability.Actor("system"),
	})
	if err != nil {
		t.Fatal(err)
	}
	cmd := &inbound.SlashCommand{
		Verb: inbound.SlashVerbTrack, Args: []string{string(tres.TaskID)},
		Raw: "/track " + string(tres.TaskID),
	}
	_, err = sr.Route(context.Background(), cmd, inbound.SlashRouteContext{
		IdentityID: user, VendorThreadKey: "t", MessageContext: inbound.MessageContextDM,
	})
	if err == nil {
		t.Fatal("want findOrCreate error")
	}
}

// TestSlashRouter_AnswerIRLookupError covers routeAnswer's non-NotFound
// error branch.
func TestSlashRouter_AnswerIRLookupError(t *testing.T) {
	f := newFixture(t)
	user := f.seedUser(t, "hayang")
	fir := &fakeIRRepo{Repository: f.irs, findErr: errors.New("ir db down")}
	sr, err := inbound.NewSlashRouter(inbound.SlashRouterDeps{
		DB: f.db, Clock: f.clock, IDGen: f.idgen, Sink: f.sink,
		Tasks: f.tasks, Execs: f.execs, Convs: f.convs,
		TaskSvc: f.taskSvc, IRSvc: f.irSvc, IRRepo: fir,
		MsgWriter: f.msgWriter, Actor: observability.Actor("system"),
	})
	if err != nil {
		t.Fatal(err)
	}
	cmd := &inbound.SlashCommand{Verb: inbound.SlashVerbAnswer, Args: []string{"I-1", "B"}, Raw: "/answer I-1 B"}
	_, err = sr.Route(context.Background(), cmd, inbound.SlashRouteContext{
		IdentityID: user, VendorThreadKey: "t", MessageContext: inbound.MessageContextDM,
	})
	if err == nil {
		t.Fatal("want ir lookup error")
	}
}

// TestCardCallback_IRLookupError covers handleInputRequestRespond non-
// NotFound branch.
func TestCardCallback_IRLookupError(t *testing.T) {
	f := newFixture(t)
	user := f.seedUser(t, "hayang")
	fir := &fakeIRRepo{Repository: f.irs, findErr: errors.New("ir db down")}
	cc, err := inbound.NewCardCallback(inbound.CardCallbackDeps{
		Clock: f.clock, Sink: f.sink, IRRepo: fir, IRSvc: f.irSvc,
		Execs: f.execs, Tasks: f.tasks, MsgWriter: f.msgWriter,
		Actor: observability.Actor("system"),
	})
	if err != nil {
		t.Fatal(err)
	}
	ev := inbound.CardActionEvent{
		CardMessageID: "om-1",
		ActionValue: map[string]any{
			"action": "input_request_respond", "input_request_id": "x", "option_text": "A",
		},
	}
	_, err = cc.Handle(context.Background(), ev, user)
	if err == nil {
		t.Fatal("want ir lookup error")
	}
}

// TestCardCallback_ExecLookupError covers conversationForExecution's
// non-NotFound branch.
func TestCardCallback_ExecLookupError(t *testing.T) {
	f := newFixture(t)
	user := f.seedUser(t, "hayang")
	_, _, irID, _ := f.seedTaskWithIR(t)
	fer := &fakeExecRepo{Repository: f.execs, findErr: errors.New("exec db down")}
	cc, err := inbound.NewCardCallback(inbound.CardCallbackDeps{
		Clock: f.clock, Sink: f.sink, IRRepo: f.irs, IRSvc: f.irSvc,
		Execs: fer, Tasks: f.tasks, MsgWriter: f.msgWriter,
		Actor: observability.Actor("system"),
	})
	if err != nil {
		t.Fatal(err)
	}
	ev := inbound.CardActionEvent{
		CardMessageID: "om-1",
		ActionValue: map[string]any{
			"action": "input_request_respond", "input_request_id": string(irID),
			"option_text": "A",
		},
	}
	_, err = cc.Handle(context.Background(), ev, user)
	if err == nil {
		t.Fatal("want exec lookup error")
	}
}

// TestSlashRouter_AnswerExecNotFoundReturnsEmpty covers the
// "exec not found → no conv, no trace message" path.
func TestSlashRouter_AnswerExecNotFoundReturnsEmpty(t *testing.T) {
	f := newFixture(t)
	user := f.seedUser(t, "hayang")
	_, _, irID, _ := f.seedTaskWithIR(t)
	// Wrap the IR repo to make FindByID return our IR but use a
	// task ID that doesn't have a matching task (we use a fakeIRRepo
	// with the same IR but tampered TaskExecutionID — easier path is
	// to use a fake execs repo returning ErrTaskExecutionNotFound).
	fer := &fakeExecRepo{Repository: f.execs, findErr: execution.ErrTaskExecutionNotFound}
	sr, _ := inbound.NewSlashRouter(inbound.SlashRouterDeps{
		DB: f.db, Clock: f.clock, IDGen: f.idgen, Sink: f.sink,
		Tasks: f.tasks, Execs: fer, Convs: f.convs,
		TaskSvc: f.taskSvc, IRSvc: f.irSvc, IRRepo: f.irs,
		MsgWriter: f.msgWriter, Actor: observability.Actor("system"),
	})
	cmd := &inbound.SlashCommand{
		Verb: inbound.SlashVerbAnswer, Args: []string{string(irID), "B"},
		Raw: "/answer " + string(irID) + " B",
	}
	dec, err := sr.Route(context.Background(), cmd, inbound.SlashRouteContext{
		IdentityID: user, VendorThreadKey: "t", MessageContext: inbound.MessageContextDM,
		VendorMsgRef: "ref-1",
	})
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if dec.Kind != inbound.RouteDecisionSlashRoute {
		t.Errorf("decision: %v", dec)
	}
	if dec.ConversationID != "" {
		t.Errorf("conversation should be empty: %s", dec.ConversationID)
	}
}

// TestCardCallback_ExecNotFoundCleanReturn covers the IR-found-but-
// execution-not-found path in CardCallback.
func TestCardCallback_ExecNotFoundCleanReturn(t *testing.T) {
	f := newFixture(t)
	user := f.seedUser(t, "hayang")
	_, _, irID, _ := f.seedTaskWithIR(t)
	fer := &fakeExecRepo{Repository: f.execs, findErr: execution.ErrTaskExecutionNotFound}
	cc, _ := inbound.NewCardCallback(inbound.CardCallbackDeps{
		Clock: f.clock, Sink: f.sink, IRRepo: f.irs, IRSvc: f.irSvc,
		Execs: fer, Tasks: f.tasks, MsgWriter: f.msgWriter,
		Actor: observability.Actor("system"),
	})
	ev := inbound.CardActionEvent{
		CardMessageID: "om-1",
		ActionValue: map[string]any{
			"action": "input_request_respond", "input_request_id": string(irID),
			"option_text": "A",
		},
	}
	dec, err := cc.Handle(context.Background(), ev, user)
	if err != nil {
		t.Fatal(err)
	}
	if dec.Kind != inbound.RouteDecisionCardCallback {
		t.Errorf("decision: %v", dec)
	}
}

// TestResolver_IdentityListDBError covers the inner Find error path in autoBind.
func TestResolver_IdentityListDBError(t *testing.T) {
	f := newFixture(t)
	fi := &fakeIdentRepo{IdentityRepository: f.identities, findErr: errors.New("ident db down")}
	r, err := inbound.NewIdentityResolver(inbound.IdentityResolverDeps{
		Bindings: f.bindings, Identities: fi, Registration: f.identReg,
		Sink: f.sink, Clock: f.clock, Channel: "feishu",
		Actor: observability.Actor("system"),
	})
	if err != nil {
		t.Fatal(err)
	}
	_, err = r.Resolve(context.Background(), "ou-new")
	if err == nil {
		t.Fatal("want ident list error")
	}
}

// TestSlashRouter_FindOrCreateConversationDBError exercises the non-
// NotFound conv lookup error path in slash router (different code
// path from the top-level Router).
func TestSlashRouter_FindOrCreateConversationDBError(t *testing.T) {
	f := newFixture(t)
	user := f.seedUser(t, "hayang")
	tres, _ := f.taskSvc.Create(context.Background(), trservice.TaskCreateInput{
		ProjectID: "demo", Title: "test", Actor: observability.Actor("system"),
	})
	fcr := &fakeConvRepo{ConversationRepository: f.convs, findErr: errors.New("conv db down")}
	sr, err := inbound.NewSlashRouter(inbound.SlashRouterDeps{
		DB: f.db, Clock: f.clock, IDGen: f.idgen, Sink: f.sink,
		Tasks: f.tasks, Execs: f.execs, Convs: fcr,
		TaskSvc: f.taskSvc, IRSvc: f.irSvc, IRRepo: f.irs,
		MsgWriter: f.msgWriter, Actor: observability.Actor("system"),
	})
	if err != nil {
		t.Fatal(err)
	}
	cmd := &inbound.SlashCommand{
		Verb: inbound.SlashVerbTrack, Args: []string{string(tres.TaskID)},
		Raw: "/track " + string(tres.TaskID),
	}
	_, err = sr.Route(context.Background(), cmd, inbound.SlashRouteContext{
		IdentityID: user, VendorThreadKey: "t", MessageContext: inbound.MessageContextDM,
	})
	if err == nil {
		t.Fatal("want conv db error")
	}
}

// TestRouter_FindOrCreateConversationDBError exercises the non-
// NotFound conv lookup error path in router.findOrCreateConversation.
func TestRouter_FindOrCreateConversationDBError(t *testing.T) {
	f := newFixture(t)
	f.seedUser(t, "hayang")
	fcr := &fakeConvRepo{ConversationRepository: f.convs, findErr: errors.New("conv db down")}
	router, err := inbound.NewRouter(inbound.RouterDeps{
		Clock: f.clock, IDGen: f.idgen, Sink: f.sink,
		Dedupe: inbound.NewDedupe(0, 0, f.clock),
		Resolver: f.resolver, Parser: f.parser, Slash: f.slash, Card: f.card,
		DB: f.db, Convs: fcr, MsgWriter: f.msgWriter,
		Actor: observability.Actor("system"),
	})
	if err != nil {
		t.Fatal(err)
	}
	_, err = router.OnVendorEvent(context.Background(), inbound.VendorEvent{
		Kind: inbound.VendorEventMessageReceive,
		VendorMsgRef: "ref-1", VendorThreadKey: "t", VendorUserID: "ou-1",
		Context: inbound.MessageContextDM, Text: "hi",
	})
	if err == nil {
		t.Fatal("want conv db error")
	}
}

// TestSlashRouter_AnswerExecLookupError covers
// SlashRouter.conversationForExecution's non-NotFound branch.
func TestSlashRouter_AnswerExecLookupError(t *testing.T) {
	f := newFixture(t)
	user := f.seedUser(t, "hayang")
	_, _, irID, _ := f.seedTaskWithIR(t)
	fer := &fakeExecRepo{Repository: f.execs, findErr: errors.New("exec db down")}
	sr, err := inbound.NewSlashRouter(inbound.SlashRouterDeps{
		DB: f.db, Clock: f.clock, IDGen: f.idgen, Sink: f.sink,
		Tasks: f.tasks, Execs: fer, Convs: f.convs,
		TaskSvc: f.taskSvc, IRSvc: f.irSvc, IRRepo: f.irs,
		MsgWriter: f.msgWriter, Actor: observability.Actor("system"),
	})
	if err != nil {
		t.Fatal(err)
	}
	cmd := &inbound.SlashCommand{
		Verb: inbound.SlashVerbAnswer, Args: []string{string(irID), "B"},
		Raw: "/answer " + string(irID) + " B",
	}
	_, err = sr.Route(context.Background(), cmd, inbound.SlashRouteContext{
		IdentityID: user, VendorThreadKey: "t", MessageContext: inbound.MessageContextDM,
		VendorMsgRef: "ref-1",
	})
	if err == nil {
		t.Fatal("want exec lookup error")
	}
}

