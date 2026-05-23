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
// Invariants per workforce/01 § 7 + ADR-0023:
//  1. worker_id immutable
//  2. status ∈ {online, offline}
//  3. online ↔ offline reversible
//  4. version monotonically increments on each mutation
//  5. concurrency / discovery / capabilities serve dispatch + auto-probe
//
// All fields are unexported; mutators bump version and validate transitions.
type Worker struct {
	id              WorkerID
	status          WorkerStatus
	capabilities    []Capability
	concurrency     WorkerConcurrency
	discovery       WorkerDiscovery
	lastHeartbeatAt *time.Time
	workingSeconds  int64
	enrolledAt      time.Time
	onlineAt        *time.Time
	offlineAt       *time.Time
	offlineReason   OfflineReason
	offlineMessage  string
	createdAt       time.Time
	updatedAt       time.Time
	version         int
}

// NewWorkerInput captures the constructor arguments for NewWorker
// (workforce/01 § 2 enroll). Capabilities accepts a list of CLI names for
// convenience; they are auto-promoted to Capability{Detected:true,Enabled:true}.
// For richer initialisation use CapabilityList directly.
type NewWorkerInput struct {
	ID             WorkerID
	Capabilities   []string
	CapabilityList []Capability
	Concurrency    *WorkerConcurrency
	Discovery      *WorkerDiscovery
	EnrolledAt     time.Time
	CreatedAt      time.Time
}

// NewWorker constructs a freshly enrolled Worker in `offline` state with
// version=1.
func NewWorker(in NewWorkerInput) (*Worker, error) {
	if err := validateWorkerID(in.ID); err != nil {
		return nil, err
	}
	if in.EnrolledAt.IsZero() {
		return nil, errors.New("worker: enrolled_at required")
	}
	caps := buildCapabilityList(in.CapabilityList, in.Capabilities)
	concurrency := DefaultWorkerConcurrency()
	if in.Concurrency != nil {
		concurrency = *in.Concurrency
	}
	discovery := DefaultWorkerDiscovery()
	if in.Discovery != nil {
		discovery = *in.Discovery
	}
	created := in.CreatedAt
	if created.IsZero() {
		created = in.EnrolledAt
	}
	return &Worker{
		id:           in.ID,
		status:       WorkerOffline,
		capabilities: caps,
		concurrency:  concurrency,
		discovery:    discovery,
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
	Capabilities    []string // legacy convenience: list of CLI names
	CapabilityList  []Capability
	Concurrency     *WorkerConcurrency
	Discovery       *WorkerDiscovery
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
	caps := buildCapabilityList(in.CapabilityList, in.Capabilities)
	concurrency := DefaultWorkerConcurrency()
	if in.Concurrency != nil {
		concurrency = *in.Concurrency
	}
	discovery := DefaultWorkerDiscovery()
	if in.Discovery != nil {
		discovery = *in.Discovery
	}
	return &Worker{
		id:              in.ID,
		status:          in.Status,
		capabilities:    caps,
		concurrency:     concurrency,
		discovery:       discovery,
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

// Capabilities returns the list of agent CLI names (for backward
// compatibility). Only entries with both Detected=true and Enabled=true are
// returned — i.e. capabilities currently dispatchable.
func (w *Worker) Capabilities() []string {
	out := make([]string, 0, len(w.capabilities))
	for _, c := range w.capabilities {
		if c.Detected && c.Enabled {
			out = append(out, c.AgentCLI)
		}
	}
	return out
}

// CapabilityList returns a copy of the full Capability list (with
// detected / enabled flags). v2 API per ADR-0023 § 4.
func (w *Worker) CapabilityList() []Capability {
	return append([]Capability(nil), w.capabilities...)
}

// Concurrency returns Worker.concurrency (ADR-0023 § 3).
func (w *Worker) Concurrency() WorkerConcurrency { return w.concurrency }

// Discovery returns Worker.discovery (ADR-0023 § 3).
func (w *Worker) Discovery() WorkerDiscovery { return w.discovery }

// CapabilitiesJSON marshals capabilities for storage (v2 schema: rich VO list).
func (w *Worker) CapabilitiesJSON() ([]byte, error) {
	if len(w.capabilities) == 0 {
		return []byte("[]"), nil
	}
	return json.Marshal(w.capabilities)
}

// ConcurrencyJSON marshals the concurrency VO for storage.
func (w *Worker) ConcurrencyJSON() ([]byte, error) {
	return json.Marshal(w.concurrency)
}

// DiscoveryJSON marshals the discovery VO for storage.
func (w *Worker) DiscoveryJSON() ([]byte, error) {
	return json.Marshal(w.discovery)
}

// ApplyConfig updates concurrency + discovery (per ADR-0023 service path).
// Bumps version. Either field may be nil to leave unchanged.
func (w *Worker) ApplyConfig(at time.Time, newConcurrency *WorkerConcurrency, newDiscovery *WorkerDiscovery) {
	if newConcurrency != nil {
		w.concurrency = *newConcurrency
	}
	if newDiscovery != nil {
		w.discovery = *newDiscovery
	}
	w.updatedAt = at.UTC()
	w.version++
}

// ApplyCapabilities replaces the capability list (worker probe upload).
// Existing user `Enabled` choices are preserved where the CLI name matches.
// Bumps version.
func (w *Worker) ApplyCapabilities(at time.Time, detected []Capability) {
	// Preserve user-controlled `Enabled` flag from prior list.
	enabledByCLI := map[string]bool{}
	for _, c := range w.capabilities {
		enabledByCLI[c.AgentCLI] = c.Enabled
	}
	out := make([]Capability, 0, len(detected))
	seen := map[string]struct{}{}
	for _, c := range detected {
		if _, dup := seen[c.AgentCLI]; dup {
			continue
		}
		seen[c.AgentCLI] = struct{}{}
		// Default Enabled = previous user choice OR Detected (first time).
		enabled := c.Detected
		if prev, ok := enabledByCLI[c.AgentCLI]; ok {
			enabled = prev
		}
		out = append(out, Capability{
			AgentCLI: c.AgentCLI,
			Detected: c.Detected,
			Enabled:  enabled,
		})
	}
	w.capabilities = out
	w.updatedAt = at.UTC()
	w.version++
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

// buildCapabilityList merges the legacy []string Capabilities and the new
// []Capability CapabilityList input fields. Rich list takes priority; legacy
// strings are auto-promoted to {Detected:true, Enabled:true}.
func buildCapabilityList(rich []Capability, legacy []string) []Capability {
	if len(rich) > 0 {
		out := make([]Capability, 0, len(rich))
		seen := map[string]struct{}{}
		for _, c := range rich {
			if _, dup := seen[c.AgentCLI]; dup {
				continue
			}
			seen[c.AgentCLI] = struct{}{}
			out = append(out, c)
		}
		return out
	}
	if len(legacy) == 0 {
		return nil
	}
	out := make([]Capability, 0, len(legacy))
	seen := map[string]struct{}{}
	for _, s := range legacy {
		if _, dup := seen[s]; dup {
			continue
		}
		seen[s] = struct{}{}
		out = append(out, Capability{AgentCLI: s, Detected: true, Enabled: true})
	}
	return out
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

func copyTimePtr(t *time.Time) *time.Time {
	if t == nil {
		return nil
	}
	u := t.UTC()
	return &u
}
