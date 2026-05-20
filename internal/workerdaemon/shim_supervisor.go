package workerdaemon

import (
	"context"
	"sync"
	"time"

	"github.com/oopslink/agent-center/internal/clock"
	"github.com/oopslink/agent-center/internal/shim"
)

// ShimRecord describes one active shim the daemon is tracking.
type ShimRecord struct {
	ExecutionID    string
	ShimPID        int
	ShimStartTime  time.Time
	HelloReceived  bool
	SpawnedAt      time.Time
}

// ShimSupervisor watches active shims for hello-timeout and crashed
// detection.
type ShimSupervisor struct {
	mu          sync.Mutex
	records     map[string]*ShimRecord
	startTimer  shim.ProcessStartTimer
	clock       clock.Clock
	helloDeadline time.Duration
	uploader    DispatchUploader
}

// NewShimSupervisor constructs a supervisor.
func NewShimSupervisor(startTimer shim.ProcessStartTimer, clk clock.Clock, helloDeadline time.Duration, uploader DispatchUploader) *ShimSupervisor {
	if clk == nil {
		clk = clock.SystemClock{}
	}
	if helloDeadline == 0 {
		helloDeadline = 60 * time.Second
	}
	if uploader == nil {
		uploader = NoopUploader{}
	}
	return &ShimSupervisor{
		records: map[string]*ShimRecord{},
		startTimer: startTimer, clock: clk, helloDeadline: helloDeadline,
		uploader: uploader,
	}
}

// Register adds a shim to the supervisor.
func (s *ShimSupervisor) Register(rec ShimRecord) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.records[rec.ExecutionID] = &rec
}

// MarkHelloReceived flips the hello flag for an execution.
func (s *ShimSupervisor) MarkHelloReceived(executionID string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if rec, ok := s.records[executionID]; ok {
		rec.HelloReceived = true
	}
}

// Remove removes a shim from tracking (called on ShimGoodbye).
func (s *ShimSupervisor) Remove(executionID string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.records, executionID)
}

// CheckOnce performs one supervisor sweep. It uses the clock to check the
// hello-deadline and the startTimer to verify shim process is still alive
// with matching start_time. Returns the executions that transitioned this
// tick.
type CheckResult struct {
	NoHello []string
	Crashed []string
}

// Check sweeps registered shims.
func (s *ShimSupervisor) Check(ctx context.Context) (CheckResult, error) {
	s.mu.Lock()
	now := s.clock.Now()
	pending := make([]*ShimRecord, 0, len(s.records))
	for _, rec := range s.records {
		pending = append(pending, rec)
	}
	s.mu.Unlock()

	var res CheckResult
	for _, rec := range pending {
		// Hello deadline
		if !rec.HelloReceived && now.Sub(rec.SpawnedAt) > s.helloDeadline {
			res.NoHello = append(res.NoHello, rec.ExecutionID)
			if err := s.uploader.NotifyShimNoHello(ctx, rec.ExecutionID); err != nil {
				return res, err
			}
			s.Remove(rec.ExecutionID)
			continue
		}
		// Liveness probe (only after hello received; before that we wait
		// for the deadline above).
		if rec.HelloReceived && s.startTimer != nil {
			alive, err := s.checkAlive(rec)
			if err == nil && !alive {
				res.Crashed = append(res.Crashed, rec.ExecutionID)
				if uerr := s.uploader.NotifyShimCrashed(ctx, rec.ExecutionID); uerr != nil {
					return res, uerr
				}
				s.Remove(rec.ExecutionID)
			}
		}
	}
	return res, nil
}

func (s *ShimSupervisor) checkAlive(rec *ShimRecord) (bool, error) {
	got, err := s.startTimer.GetStartTime(rec.ShimPID)
	if err != nil {
		return false, err
	}
	if got.IsZero() {
		return false, nil
	}
	if absDuration(got.Sub(rec.ShimStartTime)) > time.Second {
		return false, nil
	}
	return true, nil
}

func absDuration(d time.Duration) time.Duration {
	if d < 0 {
		return -d
	}
	return d
}

// Snapshot returns a copy of the records map (test introspection).
func (s *ShimSupervisor) Snapshot() map[string]ShimRecord {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make(map[string]ShimRecord, len(s.records))
	for k, v := range s.records {
		out[k] = *v
	}
	return out
}
