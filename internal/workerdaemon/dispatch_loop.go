package workerdaemon

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"
	"sync"
	"time"

	"github.com/oopslink/agent-center/internal/agentadapter"
	"github.com/oopslink/agent-center/internal/clock"
	"github.com/oopslink/agent-center/internal/shim"
	"github.com/oopslink/agent-center/internal/taskruntime/dispatch"
	"github.com/oopslink/agent-center/internal/taskruntime/execution"
)

// MappingResolver tells the daemon whether a worker is mapped to a
// project + which base_path to use.
type MappingResolver interface {
	ResolveBasePath(ctx context.Context, workerID, projectID string) (string, error)
}

// StaticMappingResolver is a v1 in-memory resolver for tests / single-
// worker setups.
type StaticMappingResolver struct {
	mu       sync.RWMutex
	mappings map[string]string // key = workerID|projectID → base_path
}

// NewStaticMappingResolver constructs an empty resolver.
func NewStaticMappingResolver() *StaticMappingResolver {
	return &StaticMappingResolver{mappings: map[string]string{}}
}

// Set records a mapping.
func (r *StaticMappingResolver) Set(workerID, projectID, basePath string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.mappings[workerID+"|"+projectID] = basePath
}

// ResolveBasePath returns the mapped base_path or NackMappingMissing.
func (r *StaticMappingResolver) ResolveBasePath(_ context.Context, workerID, projectID string) (string, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	bp, ok := r.mappings[workerID+"|"+projectID]
	if !ok {
		return "", errMappingMissing
	}
	return bp, nil
}

var errMappingMissing = errors.New("workerdaemon: project mapping missing")

// IsMappingMissing tests for the mapping-missing sentinel.
func IsMappingMissing(err error) bool { return errors.Is(err, errMappingMissing) }

// DispatchUploader receives the daemon's outcome (ACK/NACK + later events
// + shim_no_hello / shim_crashed). Production wires this to gRPC; tests
// inject a fake.
type DispatchUploader interface {
	SendAck(ctx context.Context, ack dispatch.DispatchAck) error
	SendNack(ctx context.Context, nack dispatch.DispatchNack) error
	NotifyShimNoHello(ctx context.Context, executionID string) error
	NotifyShimCrashed(ctx context.Context, executionID string) error
	NotifyWorking(ctx context.Context, executionID, cwd, branch string) error
}

// NoopUploader discards calls.
type NoopUploader struct{}

func (NoopUploader) SendAck(context.Context, dispatch.DispatchAck) error   { return nil }
func (NoopUploader) SendNack(context.Context, dispatch.DispatchNack) error { return nil }
func (NoopUploader) NotifyShimNoHello(context.Context, string) error       { return nil }
func (NoopUploader) NotifyShimCrashed(context.Context, string) error       { return nil }
func (NoopUploader) NotifyWorking(context.Context, string, string, string) error {
	return nil
}

// DispatchLoopConfig captures the daemon's dispatch loop settings.
type DispatchLoopConfig struct {
	WorkerID         string
	ExecBaseDir      string // ~/.agent-center-worker/exec
	HelloTimeout     time.Duration
	SupportedClis    map[string]bool
}

// DispatchLoop is the daemon's per-envelope handler (02-task-execution §
// 9.2 11-step sequence).
type DispatchLoop struct {
	cfg       DispatchLoopConfig
	resolver  MappingResolver
	registry  *agentadapter.Registry
	workspace *WorkspaceManager
	uploader  DispatchUploader
	clock     clock.Clock
	spawner   shim.Spawner
}

// NewDispatchLoop constructs the loop.
func NewDispatchLoop(cfg DispatchLoopConfig, resolver MappingResolver, registry *agentadapter.Registry, ws *WorkspaceManager, uploader DispatchUploader, clk clock.Clock, sp shim.Spawner) *DispatchLoop {
	if registry == nil {
		registry = agentadapter.DefaultRegistry
	}
	if clk == nil {
		clk = clock.SystemClock{}
	}
	if sp == nil {
		sp = shim.OSSpawner{}
	}
	if cfg.HelloTimeout == 0 {
		cfg.HelloTimeout = 60 * time.Second
	}
	if uploader == nil {
		uploader = NoopUploader{}
	}
	return &DispatchLoop{
		cfg: cfg, resolver: resolver, registry: registry, workspace: ws,
		uploader: uploader, clock: clk, spawner: sp,
	}
}

// HandleEnvelope runs the 11-step dispatch sequence for one envelope.
// Returns ack/nack/error so the daemon can plumb back to the center.
func (d *DispatchLoop) HandleEnvelope(ctx context.Context, env dispatch.DispatchEnvelope) error {
	if err := env.Validate(); err != nil {
		return d.nackAndReturn(ctx, env, execution.NackEnvelopeVersionUnsupported, err.Error())
	}
	// Step 2: per-execution dir idempotency
	dirRoot := d.cfg.ExecBaseDir
	if dirRoot == "" {
		dirRoot = filepath.Join(".", "exec")
	}
	dir, err := shim.NewDir(dirRoot, string(env.ExecutionID))
	if err != nil {
		return fmt.Errorf("daemon: per-exec dir: %w", err)
	}
	// If status.json exists and phase != done, treat as in-flight → re-ACK
	// without re-spawning.
	if status, statusErr := dir.ReadStatus(); statusErr == nil {
		switch status.Phase {
		case shim.PhaseRunning, shim.PhaseStarting:
			return d.uploader.SendAck(ctx, dispatch.DispatchAck{
				ExecutionID: env.ExecutionID,
				Accepted:    true,
				Message:     "re-ack: shim already in-flight",
				AckedAt:     d.clock.Now(),
			})
		case shim.PhaseDone:
			return d.uploader.SendAck(ctx, dispatch.DispatchAck{
				ExecutionID: env.ExecutionID,
				Accepted:    true,
				Message:     "re-ack: shim already terminated",
				AckedAt:     d.clock.Now(),
			})
		}
	}
	// Step 3: validate envelope
	if !d.cliSupported(env.AgentCLI) {
		return d.nackAndReturn(ctx, env, execution.NackAgentCliUnsupported,
			fmt.Sprintf("worker does not support agent_cli=%s", env.AgentCLI))
	}
	if d.resolver == nil {
		return d.nackAndReturn(ctx, env, execution.NackMappingMissing, "no mapping resolver configured")
	}
	basePath, err := d.resolver.ResolveBasePath(ctx, env.WorkerID, env.ProjectID)
	if err != nil {
		if IsMappingMissing(err) {
			return d.nackAndReturn(ctx, env, execution.NackMappingMissing,
				fmt.Sprintf("worker %s has no mapping for project %s", env.WorkerID, env.ProjectID))
		}
		return err
	}
	// Step 5: ACK first (envelope accepted)
	if err := d.uploader.SendAck(ctx, dispatch.DispatchAck{
		ExecutionID: env.ExecutionID,
		Accepted:    true,
		AckedAt:     d.clock.Now(),
	}); err != nil {
		return err
	}
	// Step 6: workspace prep
	prep, err := d.workspace.Prepare(ctx, PrepareInput{
		BasePath:      basePath,
		WorkspaceMode: env.WorkspaceMode,
		ExecutionID:   string(env.ExecutionID),
		BaseBranch:    env.BaseBranch,
	})
	if err != nil {
		// Workspace setup failed — send back as NACK-like worktree_path_busy
		return d.nackAndReturn(ctx, env, execution.NackWorktreePathBusy, err.Error())
	}
	// emit working
	if err := d.uploader.NotifyWorking(ctx, string(env.ExecutionID), prep.CWD, prep.BranchName); err != nil {
		return err
	}
	return nil
}

func (d *DispatchLoop) cliSupported(cli string) bool {
	if len(d.cfg.SupportedClis) == 0 {
		// Fall back: check adapter is registered (e.g. claude-code self-
		// registers).
		_, ok := d.registry.Get(cli)
		return ok
	}
	return d.cfg.SupportedClis[cli]
}

func (d *DispatchLoop) nackAndReturn(ctx context.Context, env dispatch.DispatchEnvelope, reason execution.NackSubReason, msg string) error {
	return d.uploader.SendNack(ctx, dispatch.DispatchNack{
		ExecutionID: env.ExecutionID,
		Accepted:    false,
		Reason:      reason,
		Message:     msg,
		AckedAt:     d.clock.Now(),
	})
}
