package workforce

import (
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"
)

// Worker is the Workforce BC AR (workforce/01-worker).
//
// Invariants per workforce/01 § 7:
//  1. worker_id immutable
//  2. status ∈ {online, offline}
//  3. online ↔ offline reversible
//  4. version monotonically increments on each mutation
//
// All fields are unexported; mutators (`MarkOnline` / `MarkOffline` /
// `Heartbeat`) bump version and validate transitions.
type Worker struct {
	id               WorkerID
	status           WorkerStatus
	capabilities     []string
	lastHeartbeatAt  *time.Time
	workingSeconds   int64
	enrolledAt       time.Time
	onlineAt         *time.Time
	offlineAt        *time.Time
	offlineReason    OfflineReason
	offlineMessage   string
	createdAt        time.Time
	updatedAt        time.Time
	version          int
}

// NewWorkerInput captures the constructor arguments for NewWorker
// (workforce/01 § 2 enroll).
type NewWorkerInput struct {
	ID           WorkerID
	Capabilities []string
	EnrolledAt   time.Time
	CreatedAt    time.Time
}

// NewWorker constructs a freshly enrolled Worker in `offline` state with
// version=1 (workforce/01 § 1: initial state is offline).
func NewWorker(in NewWorkerInput) (*Worker, error) {
	if err := validateWorkerID(in.ID); err != nil {
		return nil, err
	}
	if in.EnrolledAt.IsZero() {
		return nil, errors.New("worker: enrolled_at required")
	}
	caps := dedupAndCopy(in.Capabilities)
	created := in.CreatedAt
	if created.IsZero() {
		created = in.EnrolledAt
	}
	return &Worker{
		id:           in.ID,
		status:       WorkerOffline,
		capabilities: caps,
		enrolledAt:   in.EnrolledAt.UTC(),
		createdAt:    created.UTC(),
		updatedAt:    created.UTC(),
		version:      1,
	}, nil
}

// RehydrateWorkerInput is used by Repository implementations to reconstruct
// a Worker from persistent storage. All fields are copied verbatim with no
// transition validation.
type RehydrateWorkerInput struct {
	ID              WorkerID
	Status          WorkerStatus
	Capabilities    []string
	LastHeartbeatAt *time.Time
	WorkingSeconds  int64
	EnrolledAt      time.Time
	OnlineAt        *time.Time
	OfflineAt       *time.Time
	OfflineReason   OfflineReason
	OfflineMessage  string
	CreatedAt       time.Time
	UpdatedAt       time.Time
	Version         int
}

// RehydrateWorker reconstructs a Worker from persisted state. Not for use
// in business code.
func RehydrateWorker(in RehydrateWorkerInput) (*Worker, error) {
	if err := validateWorkerID(in.ID); err != nil {
		return nil, err
	}
	if !in.Status.IsValid() {
		return nil, ErrWorkerInvalidStatus
	}
	if in.Version < 1 {
		return nil, errors.New("worker: version must be >= 1")
	}
	return &Worker{
		id:              in.ID,
		status:          in.Status,
		capabilities:    dedupAndCopy(in.Capabilities),
		lastHeartbeatAt: copyTimePtr(in.LastHeartbeatAt),
		workingSeconds:  in.WorkingSeconds,
		enrolledAt:      in.EnrolledAt.UTC(),
		onlineAt:        copyTimePtr(in.OnlineAt),
		offlineAt:       copyTimePtr(in.OfflineAt),
		offlineReason:   in.OfflineReason,
		offlineMessage:  in.OfflineMessage,
		createdAt:       in.CreatedAt.UTC(),
		updatedAt:       in.UpdatedAt.UTC(),
		version:         in.Version,
	}, nil
}

// Getters.

func (w *Worker) ID() WorkerID                 { return w.id }
func (w *Worker) Status() WorkerStatus         { return w.status }
func (w *Worker) Capabilities() []string       { return append([]string(nil), w.capabilities...) }
func (w *Worker) LastHeartbeatAt() *time.Time  { return copyTimePtr(w.lastHeartbeatAt) }
func (w *Worker) WorkingSeconds() int64        { return w.workingSeconds }
func (w *Worker) EnrolledAt() time.Time        { return w.enrolledAt }
func (w *Worker) OnlineAt() *time.Time         { return copyTimePtr(w.onlineAt) }
func (w *Worker) OfflineAt() *time.Time        { return copyTimePtr(w.offlineAt) }
func (w *Worker) OfflineReason() OfflineReason { return w.offlineReason }
func (w *Worker) OfflineMessage() string       { return w.offlineMessage }
func (w *Worker) CreatedAt() time.Time         { return w.createdAt }
func (w *Worker) UpdatedAt() time.Time         { return w.updatedAt }
func (w *Worker) Version() int                 { return w.version }

// CapabilitiesJSON marshals capabilities for storage.
func (w *Worker) CapabilitiesJSON() ([]byte, error) {
	if w.capabilities == nil {
		return []byte("[]"), nil
	}
	return json.Marshal(w.capabilities)
}

// MarkOnline transitions worker offline→online; no-op when already online.
func (w *Worker) MarkOnline(at time.Time) {
	if w.status == WorkerOnline {
		return
	}
	w.status = WorkerOnline
	at = at.UTC()
	w.onlineAt = &at
	w.offlineAt = nil
	w.offlineReason = ""
	w.offlineMessage = ""
	w.updatedAt = at
	w.version++
}

// MarkOffline transitions online→offline with reason+message (conventions §
// 16). No-op when already offline.
func (w *Worker) MarkOffline(at time.Time, reason OfflineReason, message string) error {
	if !reason.IsValid() {
		return fmt.Errorf("worker: invalid offline reason %q", reason)
	}
	if strings.TrimSpace(message) == "" {
		return errors.New("worker: offline message required (conventions § 16)")
	}
	if w.status == WorkerOffline {
		return nil
	}
	at = at.UTC()
	w.status = WorkerOffline
	w.offlineAt = &at
	w.offlineReason = reason
	w.offlineMessage = message
	w.updatedAt = at
	w.version++
	return nil
}

// Heartbeat records a heartbeat tick and accumulates working seconds.
func (w *Worker) Heartbeat(at time.Time, additionalWorkingSeconds int64) error {
	if additionalWorkingSeconds < 0 {
		return errors.New("worker: working seconds delta must be >= 0")
	}
	at = at.UTC()
	w.lastHeartbeatAt = &at
	w.workingSeconds += additionalWorkingSeconds
	w.updatedAt = at
	w.version++
	return nil
}

func validateWorkerID(id WorkerID) error {
	s := string(id)
	if strings.TrimSpace(s) == "" {
		return errors.New("worker: id required")
	}
	if len(s) > 128 {
		return errors.New("worker: id too long (max 128)")
	}
	for _, c := range s {
		switch {
		case c >= 'a' && c <= 'z':
		case c >= 'A' && c <= 'Z':
		case c >= '0' && c <= '9':
		case c == '-' || c == '_' || c == '.':
		default:
			return fmt.Errorf("worker: id %q contains invalid character %q", s, c)
		}
	}
	return nil
}

func dedupAndCopy(in []string) []string {
	if len(in) == 0 {
		return nil
	}
	seen := map[string]struct{}{}
	out := make([]string, 0, len(in))
	for _, s := range in {
		if _, ok := seen[s]; ok {
			continue
		}
		seen[s] = struct{}{}
		out = append(out, s)
	}
	return out
}

func copyTimePtr(t *time.Time) *time.Time {
	if t == nil {
		return nil
	}
	u := t.UTC()
	return &u
}
