package dispatch

import (
	"context"
	"errors"
	"testing"
)

// fakeResolver is a programmable AgentResolver for v2 dispatch tests.
type fakeResolver struct {
	calls int
	last  string
	out   AgentResolution
	err   error
}

func (f *fakeResolver) Resolve(ctx context.Context, agentInstanceID string) (AgentResolution, error) {
	f.calls++
	f.last = agentInstanceID
	if f.err != nil {
		return AgentResolution{}, f.err
	}
	out := f.out
	if out.AgentInstanceID == "" {
		out.AgentInstanceID = agentInstanceID
	}
	return out, nil
}

// NoopSender — trivial smoke for the default sender wiring.
func TestNoopSender_Send(t *testing.T) {
	if err := (NoopSender{}).Send(context.Background(), DispatchEnvelope{}); err != nil {
		t.Fatalf("NoopSender.Send should not error, got %v", err)
	}
}

// NewService with nil sender should default to NoopSender (clock too).
func TestNewService_NilSenderAndClock(t *testing.T) {
	svc := NewService(nil, nil, nil, nil, nil, nil, nil, DispatchConfig{})
	if svc == nil {
		t.Fatal("expected service")
	}
	if svc.sender == nil {
		t.Fatal("nil sender should be replaced with NoopSender")
	}
	if svc.clock == nil {
		t.Fatal("nil clock should be replaced with SystemClock")
	}
	if svc.cfg.MaxExecutionsPerTask == 0 {
		t.Fatal("zero MaxExecutionsPerTask should default to 3")
	}
	if svc.cfg.DispatchAckTimeout == 0 {
		t.Fatal("zero DispatchAckTimeout should default")
	}
}

func TestService_WithAgentResolver_Chain(t *testing.T) {
	// Sanity: WithAgentResolver returns the service for chaining.
	svc := &Service{}
	fr := &fakeResolver{}
	if svc.WithAgentResolver(fr) != svc {
		t.Fatal("WithAgentResolver should return the same service for chaining")
	}
	if svc.agentResolver != fr {
		t.Fatal("WithAgentResolver should set agentResolver")
	}
}

func TestDispatch_V2_NoResolverWired_Errors(t *testing.T) {
	// Construct a minimal Service; agentResolver is nil by default.
	svc := &Service{cfg: DefaultConfig()}
	_, err := svc.Dispatch(context.Background(), DispatchInput{
		TaskID:          "T-1",
		AgentInstanceID: "01HAI",
		Actor:           "user:hayang",
	})
	if !errors.Is(err, ErrAgentResolverNotConfigured) {
		t.Fatalf("expected ErrAgentResolverNotConfigured, got %v", err)
	}
}

func TestDispatch_V2_ResolverError_Propagates(t *testing.T) {
	svc := (&Service{cfg: DefaultConfig()}).WithAgentResolver(&fakeResolver{
		err: ErrAgentResolutionUnknownAgent,
	})
	_, err := svc.Dispatch(context.Background(), DispatchInput{
		TaskID:          "T-1",
		AgentInstanceID: "01HAI-MISSING",
		Actor:           "user:hayang",
	})
	if !errors.Is(err, ErrAgentResolutionUnknownAgent) {
		t.Fatalf("expected ErrAgentResolutionUnknownAgent, got %v", err)
	}
}

func TestDispatch_V2_BadActor(t *testing.T) {
	svc := (&Service{cfg: DefaultConfig()}).WithAgentResolver(&fakeResolver{})
	_, err := svc.Dispatch(context.Background(), DispatchInput{
		TaskID:          "T-1",
		AgentInstanceID: "01HAI",
		Actor:           "bogus:x",
	})
	if err == nil {
		t.Fatal("expected actor validation error")
	}
}

// V1 path: empty AgentInstanceID should bypass resolver entirely. Use the
// full test harness so the call succeeds and only the resolver-call-count
// assertion remains.
func TestDispatch_V1Path_DoesNotCallResolver(t *testing.T) {
	h := setup(t)
	fr := &fakeResolver{}
	h.svc.WithAgentResolver(fr)
	seedTask(t, h, "T-V1")
	if _, err := h.svc.Dispatch(context.Background(), DispatchInput{
		TaskID:   "T-V1",
		WorkerID: "W-1",
		AgentCLI: "claude-code",
		Actor:    "user:hayang",
	}); err != nil {
		t.Fatalf("v1 dispatch: %v", err)
	}
	if fr.calls != 0 {
		t.Fatalf("resolver should not be called in v1 path, called %d times", fr.calls)
	}
}

// V2 happy path: resolver returns FeatureOK=true; envelope is V2 with
// AgentInstanceID populated; sender receives V2 envelope.
func TestDispatch_V2_Happy(t *testing.T) {
	h := setup(t)
	fr := &fakeResolver{out: AgentResolution{
		AgentInstanceID: "01HAI",
		WorkerID:        "W-1",
		AgentCLI:        "claude-code",
		HomeDir:         "~/.agent-center-worker/agents/01HAI/",
		FeatureOK:       true,
	}}
	h.svc.WithAgentResolver(fr)
	seedTask(t, h, "T-V2")
	res, err := h.svc.Dispatch(context.Background(), DispatchInput{
		TaskID:          "T-V2",
		AgentInstanceID: "01HAI",
		Actor:           "user:hayang",
	})
	if err != nil {
		t.Fatalf("v2 dispatch: %v", err)
	}
	if fr.calls != 1 {
		t.Fatalf("resolver should be called once, got %d", fr.calls)
	}
	if fr.last != "01HAI" {
		t.Fatalf("resolver agent_instance_id: %s", fr.last)
	}
	envs := h.sender.Snapshot()
	if len(envs) != 1 {
		t.Fatalf("send count: %d", len(envs))
	}
	if envs[0].EnvelopeVersion != EnvelopeVersionV2 {
		t.Fatalf("envelope_version: %s", envs[0].EnvelopeVersion)
	}
	if envs[0].AgentInstanceID != "01HAI" {
		t.Fatalf("agent_instance_id: %s", envs[0].AgentInstanceID)
	}
	if envs[0].AgentCLI != "claude-code" {
		t.Fatalf("agent_cli (denormalised): %s", envs[0].AgentCLI)
	}
	if envs[0].WorkerID != "W-1" {
		t.Fatalf("worker_id: %s", envs[0].WorkerID)
	}
	_ = res
}

// V2 feature-fail path: resolver returns FeatureOK=false; dispatch returns
// error with reason+message; audit event emitted in separate tx.
func TestDispatch_V2_FeatureFail_EmitsAuditAndErrors(t *testing.T) {
	h := setup(t)
	fr := &fakeResolver{out: AgentResolution{
		AgentInstanceID: "01HAI",
		WorkerID:        "W-1",
		AgentCLI:        "claude-code",
		FeatureOK:       false,
		FeatureReason:   "feature_unsupported",
		FeatureMessage:  "adapter does not support MCP",
	}}
	h.svc.WithAgentResolver(fr)
	seedTask(t, h, "T-V2F")
	_, err := h.svc.Dispatch(context.Background(), DispatchInput{
		TaskID:          "T-V2F",
		AgentInstanceID: "01HAI",
		Actor:           "user:hayang",
	})
	if err == nil {
		t.Fatal("expected feature-fail error")
	}
	if len(h.sender.Snapshot()) != 0 {
		t.Fatalf("sender should not be called on feature-fail; got %d", len(h.sender.Snapshot()))
	}
	// audit event should be present
	gotTypes := eventTypes(t, h)
	found := false
	for _, ty := range gotTypes {
		if ty == "task_execution.dispatch_rejected" {
			found = true
		}
	}
	if !found {
		t.Fatalf("expected dispatch_rejected audit event, got %v", gotTypes)
	}
}
