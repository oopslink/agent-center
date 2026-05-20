package shim

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"sync/atomic"
	"time"

	"github.com/oopslink/agent-center/internal/agentadapter"
	"github.com/oopslink/agent-center/internal/clock"
)

// Config bundles shim configuration knobs.
type Config struct {
	ExecutionID   string
	ShimToken     string
	Adapter       agentadapter.Adapter
	SpawnRequest  agentadapter.SpawnRequest
	Dir           *Dir
	Spawner       Spawner            // defaults to OSSpawner
	Clock         clock.Clock        // defaults to SystemClock
	KillGrace     time.Duration      // defaults to 5s
	HelloDeadline time.Duration      // defaults to 60s for daemon-side use
}

// Shim is the per-execution shim runtime.
type Shim struct {
	cfg       Config
	process   Process
	startTime time.Time
	seq       atomic.Int64
}

// New constructs a Shim.
func New(cfg Config) (*Shim, error) {
	if cfg.ExecutionID == "" {
		return nil, errors.New("shim: execution_id required")
	}
	if cfg.ShimToken == "" {
		return nil, errors.New("shim: shim_token required")
	}
	if cfg.Adapter == nil {
		return nil, errors.New("shim: adapter required")
	}
	if cfg.Dir == nil {
		return nil, errors.New("shim: dir required")
	}
	if cfg.Spawner == nil {
		cfg.Spawner = OSSpawner{}
	}
	if cfg.Clock == nil {
		cfg.Clock = clock.SystemClock{}
	}
	if cfg.KillGrace == 0 {
		cfg.KillGrace = 5 * time.Second
	}
	if cfg.HelloDeadline == 0 {
		cfg.HelloDeadline = 60 * time.Second
	}
	return &Shim{cfg: cfg}, nil
}

// Start spawns the agent process, writes envelope.json + status.json +
// shim.pid, and returns the Process handle (caller streams events).
func (s *Shim) Start(ctx context.Context, envelopeJSON []byte) error {
	if err := s.cfg.Dir.WriteEnvelope(envelopeJSON); err != nil {
		return fmt.Errorf("shim: write envelope: %w", err)
	}
	s.startTime = s.cfg.Clock.Now()
	if err := s.cfg.Dir.WritePID(PIDFile{PID: os.Getpid(), StartTime: s.startTime}); err != nil {
		return fmt.Errorf("shim: write pid: %w", err)
	}
	if err := s.cfg.Dir.WriteStatus(Status{
		ExecutionID:   s.cfg.ExecutionID,
		Phase:         PhaseStarting,
		ShimPID:       os.Getpid(),
		ShimStartTime: s.startTime,
	}); err != nil {
		return fmt.Errorf("shim: write status: %w", err)
	}
	spec, err := s.cfg.Adapter.BuildCommand(s.cfg.SpawnRequest)
	if err != nil {
		return fmt.Errorf("shim: build command: %w", err)
	}
	proc, err := s.cfg.Spawner.Spawn(ctx, spec, nil, nil)
	if err != nil {
		return fmt.Errorf("shim: spawn: %w", err)
	}
	s.process = proc
	// status → running
	if err := s.cfg.Dir.WriteStatus(Status{
		ExecutionID:    s.cfg.ExecutionID,
		Phase:          PhaseRunning,
		ShimPID:        os.Getpid(),
		ShimStartTime:  s.startTime,
		AgentPID:       proc.PID(),
		AgentStartTime: s.cfg.Clock.Now(),
	}); err != nil {
		return fmt.Errorf("shim: write running status: %w", err)
	}
	return nil
}

// Process exposes the live process handle (for callers to Wait or Kill).
func (s *Shim) Process() Process { return s.process }

// StreamEvents reads the agent stdout line-by-line, parses through the
// adapter, and writes normalised AgentTraceEvents to events.jsonl with
// monotonically increasing seq. Returns when the reader hits EOF or ctx
// is canceled.
func (s *Shim) StreamEvents(ctx context.Context, reporter *agentadapter.UnknownEventReporter, onEvent func(agentadapter.AgentTraceEvent)) error {
	if s.process == nil {
		return errors.New("shim: process not started")
	}
	scanner := bufio.NewScanner(s.process.Stdout())
	scanner.Buffer(make([]byte, 64*1024), 1024*1024)
	for scanner.Scan() {
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}
		line := scanner.Bytes()
		copyLine := append([]byte(nil), line...)
		ev, err := s.cfg.Adapter.ParseEvent(copyLine)
		if err != nil {
			if reporter != nil && reporter.ReportParseFailure(s.cfg.ExecutionID) {
				return fmt.Errorf("shim: jsonl_parse_error threshold exceeded: %w", err)
			}
			continue
		}
		ev.Seq = s.seq.Add(1)
		ev.OccurredAt = s.cfg.Clock.Now()
		// persist to events.jsonl
		if err := writeEventLine(s.cfg.Dir, ev); err != nil {
			return fmt.Errorf("shim: write event: %w", err)
		}
		if onEvent != nil {
			onEvent(ev)
		}
	}
	if err := scanner.Err(); err != nil {
		return err
	}
	return nil
}

// Close finalises status.json to phase=done with the given exit code.
func (s *Shim) Close(exitCode int) error {
	return s.cfg.Dir.WriteStatus(Status{
		ExecutionID:    s.cfg.ExecutionID,
		Phase:          PhaseDone,
		ShimPID:        os.Getpid(),
		ShimStartTime:  s.startTime,
		AgentPID:       agentPID(s.process),
		ExitCode:       exitCode,
	})
}

// Seq returns the current event seq counter.
func (s *Shim) Seq() int64 { return s.seq.Load() }

// SetSeq overwrites the seq counter (used by Reconnect to resume from
// last_acked_seq).
func (s *Shim) SetSeq(seq int64) { s.seq.Store(seq) }

func writeEventLine(d *Dir, ev agentadapter.AgentTraceEvent) error {
	b, err := marshalEvent(ev)
	if err != nil {
		return err
	}
	return d.AppendEvent(b)
}

func marshalEvent(ev agentadapter.AgentTraceEvent) ([]byte, error) {
	return jsonMarshal(ev)
}

// jsonMarshal exists so tests can swap it out. encoding/json import would
// pull stdlib formatting; we keep this thin.
var jsonMarshal = func(v any) ([]byte, error) {
	return jsonMarshalImpl(v)
}

func agentPID(p Process) int {
	if p == nil {
		return 0
	}
	return p.PID()
}

// CopyAgentLog is a small helper that streams the process stdout to a
// writer (alternative to StreamEvents when an external scanner consumes
// the output). Returns io.EOF when the agent stdout closes.
func CopyAgentLog(w io.Writer, r io.Reader) error {
	_, err := io.Copy(w, r)
	return err
}
