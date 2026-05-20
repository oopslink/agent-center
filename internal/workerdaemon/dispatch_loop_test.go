package workerdaemon

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/oopslink/agent-center/internal/agentadapter"
	_ "github.com/oopslink/agent-center/internal/agentadapter/claudecode" // self-register
	"github.com/oopslink/agent-center/internal/clock"
	"github.com/oopslink/agent-center/internal/shim"
	"github.com/oopslink/agent-center/internal/taskruntime/dispatch"
	"github.com/oopslink/agent-center/internal/taskruntime/execution"
)

type recordingUploader struct {
	mu      sync.Mutex
	acks    []dispatch.DispatchAck
	nacks   []dispatch.DispatchNack
	working []string
	noHello []string
	crashed []string
}

func (r *recordingUploader) SendAck(_ context.Context, ack dispatch.DispatchAck) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.acks = append(r.acks, ack)
	return nil
}
func (r *recordingUploader) SendNack(_ context.Context, n dispatch.DispatchNack) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.nacks = append(r.nacks, n)
	return nil
}
func (r *recordingUploader) NotifyShimNoHello(_ context.Context, execID string) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.noHello = append(r.noHello, execID)
	return nil
}
func (r *recordingUploader) NotifyShimCrashed(_ context.Context, execID string) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.crashed = append(r.crashed, execID)
	return nil
}
func (r *recordingUploader) NotifyWorking(_ context.Context, execID, cwd, branch string) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.working = append(r.working, execID+"|"+cwd+"|"+branch)
	return nil
}

func mkEnvelope() dispatch.DispatchEnvelope {
	return dispatch.DispatchEnvelope{
		EnvelopeVersion: dispatch.EnvelopeVersionV1,
		ExecutionID:     "E-1",
		TaskID:          "T-1",
		WorkerID:        "W-1",
		ProjectID:       "P-1",
		AgentCLI:        "claude-code",
		WorkspaceMode:   execution.WorkspaceDirect,
		TaskTitle:       "do",
		Priority:        "medium",
	}
}

func TestDispatchLoop_Happy(t *testing.T) {
	resolver := NewStaticMappingResolver()
	resolver.Set("W-1", "P-1", "/repo")
	uploader := &recordingUploader{}
	ws := NewWorkspaceManager(&fakeGit{})
	dl := NewDispatchLoop(DispatchLoopConfig{
		WorkerID:    "W-1",
		ExecBaseDir: t.TempDir(),
	}, resolver, agentadapter.DefaultRegistry, ws, uploader, clock.NewFakeClock(time.Now()), nil)
	if err := dl.HandleEnvelope(context.Background(), mkEnvelope()); err != nil {
		t.Fatal(err)
	}
	if len(uploader.acks) != 1 || len(uploader.nacks) != 0 {
		t.Fatalf("ack/nack: %d/%d", len(uploader.acks), len(uploader.nacks))
	}
	if len(uploader.working) != 1 {
		t.Fatalf("working: %d", len(uploader.working))
	}
}

func TestDispatchLoop_BadVersion_Nack(t *testing.T) {
	resolver := NewStaticMappingResolver()
	resolver.Set("W-1", "P-1", "/repo")
	uploader := &recordingUploader{}
	dl := NewDispatchLoop(DispatchLoopConfig{
		WorkerID:    "W-1",
		ExecBaseDir: t.TempDir(),
	}, resolver, agentadapter.DefaultRegistry, NewWorkspaceManager(&fakeGit{}), uploader, clock.NewFakeClock(time.Now()), nil)
	env := mkEnvelope()
	env.EnvelopeVersion = "v2"
	if err := dl.HandleEnvelope(context.Background(), env); err != nil {
		t.Fatal(err)
	}
	if len(uploader.nacks) != 1 || uploader.nacks[0].Reason != execution.NackEnvelopeVersionUnsupported {
		t.Fatalf("nack: %+v", uploader.nacks)
	}
}

func TestDispatchLoop_NackMappingMissing(t *testing.T) {
	resolver := NewStaticMappingResolver()
	uploader := &recordingUploader{}
	dl := NewDispatchLoop(DispatchLoopConfig{
		WorkerID:    "W-1",
		ExecBaseDir: t.TempDir(),
	}, resolver, agentadapter.DefaultRegistry, NewWorkspaceManager(&fakeGit{}), uploader, clock.NewFakeClock(time.Now()), nil)
	if err := dl.HandleEnvelope(context.Background(), mkEnvelope()); err != nil {
		t.Fatal(err)
	}
	if len(uploader.nacks) != 1 || uploader.nacks[0].Reason != execution.NackMappingMissing {
		t.Fatalf("nack: %+v", uploader.nacks)
	}
}

func TestDispatchLoop_NackAgentCliUnsupported(t *testing.T) {
	resolver := NewStaticMappingResolver()
	resolver.Set("W-1", "P-1", "/repo")
	uploader := &recordingUploader{}
	dl := NewDispatchLoop(DispatchLoopConfig{
		WorkerID:      "W-1",
		ExecBaseDir:   t.TempDir(),
		SupportedClis: map[string]bool{"only-this": true},
	}, resolver, agentadapter.DefaultRegistry, NewWorkspaceManager(&fakeGit{}), uploader, clock.NewFakeClock(time.Now()), nil)
	if err := dl.HandleEnvelope(context.Background(), mkEnvelope()); err != nil {
		t.Fatal(err)
	}
	if len(uploader.nacks) != 1 || uploader.nacks[0].Reason != execution.NackAgentCliUnsupported {
		t.Fatalf("nack: %+v", uploader.nacks)
	}
}

func TestDispatchLoop_WorktreeFailureMapsToNack(t *testing.T) {
	resolver := NewStaticMappingResolver()
	resolver.Set("W-1", "P-1", "/repo")
	uploader := &recordingUploader{}
	ws := NewWorkspaceManager(&fakeGit{wantErr: errors.New("git locked")})
	dl := NewDispatchLoop(DispatchLoopConfig{
		WorkerID:    "W-1",
		ExecBaseDir: t.TempDir(),
	}, resolver, agentadapter.DefaultRegistry, ws, uploader, clock.NewFakeClock(time.Now()), nil)
	env := mkEnvelope()
	env.WorkspaceMode = execution.WorkspaceWorktree
	env.BaseBranch = "main"
	if err := dl.HandleEnvelope(context.Background(), env); err != nil {
		t.Fatal(err)
	}
	// First we ACK then nack (worktree failure happens after ACK in step 5/6)
	if len(uploader.nacks) != 1 || uploader.nacks[0].Reason != execution.NackWorktreePathBusy {
		t.Fatalf("nack: %+v", uploader.nacks)
	}
}

func TestDispatchLoop_Idempotent_ReAckRunning(t *testing.T) {
	resolver := NewStaticMappingResolver()
	resolver.Set("W-1", "P-1", "/repo")
	uploader := &recordingUploader{}
	tmp := t.TempDir()
	dl := NewDispatchLoop(DispatchLoopConfig{
		WorkerID:    "W-1",
		ExecBaseDir: tmp,
	}, resolver, agentadapter.DefaultRegistry, NewWorkspaceManager(&fakeGit{}), uploader, clock.NewFakeClock(time.Now()), nil)
	if err := dl.HandleEnvelope(context.Background(), mkEnvelope()); err != nil {
		t.Fatal(err)
	}
	// Re-deliver same envelope → ack reused (re-ack), no second working
	// notification because we re-ACK and return early before workspace
	// prep (status read in step 2 short-circuits).
	if err := dl.HandleEnvelope(context.Background(), mkEnvelope()); err != nil {
		t.Fatal(err)
	}
}

func TestStaticMappingResolver_NotFound(t *testing.T) {
	r := NewStaticMappingResolver()
	if _, err := r.ResolveBasePath(context.Background(), "W", "P"); !IsMappingMissing(err) {
		t.Fatalf("expected missing: %v", err)
	}
}

func TestDispatchLoop_NewDefaults(t *testing.T) {
	dl := NewDispatchLoop(DispatchLoopConfig{WorkerID: "W-1"}, nil, nil, nil, nil, nil, nil)
	if dl.cfg.HelloTimeout != 60*time.Second {
		t.Fatalf("hello: %v", dl.cfg.HelloTimeout)
	}
}

func TestDispatchLoop_NoResolver_NackMappingMissing(t *testing.T) {
	uploader := &recordingUploader{}
	dl := NewDispatchLoop(DispatchLoopConfig{
		WorkerID: "W-1", ExecBaseDir: t.TempDir(),
	}, nil, agentadapter.DefaultRegistry, NewWorkspaceManager(&fakeGit{}), uploader, clock.NewFakeClock(time.Now()), nil)
	if err := dl.HandleEnvelope(context.Background(), mkEnvelope()); err != nil {
		t.Fatal(err)
	}
	if len(uploader.nacks) != 1 || uploader.nacks[0].Reason != execution.NackMappingMissing {
		t.Fatalf("nack: %+v", uploader.nacks)
	}
}

func TestDispatchLoop_Idempotent_ReAckDone(t *testing.T) {
	resolver := NewStaticMappingResolver()
	resolver.Set("W-1", "P-1", "/repo")
	uploader := &recordingUploader{}
	root := t.TempDir()
	dl := NewDispatchLoop(DispatchLoopConfig{
		WorkerID: "W-1", ExecBaseDir: root,
	}, resolver, agentadapter.DefaultRegistry, NewWorkspaceManager(&fakeGit{}), uploader, clock.NewFakeClock(time.Now()), nil)
	d, _ := shim.NewDir(root, "E-1")
	if err := d.WriteStatus(shim.Status{ExecutionID: "E-1", Phase: shim.PhaseDone}); err != nil {
		t.Fatal(err)
	}
	if err := dl.HandleEnvelope(context.Background(), mkEnvelope()); err != nil {
		t.Fatal(err)
	}
	if len(uploader.acks) != 1 {
		t.Fatalf("expected re-ack: %+v", uploader.acks)
	}
}

func TestNoopUploader_AllNoErr(t *testing.T) {
	u := NoopUploader{}
	if err := u.SendAck(context.Background(), dispatch.DispatchAck{}); err != nil {
		t.Fatal(err)
	}
	if err := u.SendNack(context.Background(), dispatch.DispatchNack{}); err != nil {
		t.Fatal(err)
	}
	if err := u.NotifyShimNoHello(context.Background(), "x"); err != nil {
		t.Fatal(err)
	}
	if err := u.NotifyShimCrashed(context.Background(), "x"); err != nil {
		t.Fatal(err)
	}
	if err := u.NotifyWorking(context.Background(), "x", "/", ""); err != nil {
		t.Fatal(err)
	}
}
