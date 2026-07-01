// Package workforce hosts the Workforce BC tactical types:
//   - Aggregate Roots: Worker, BootstrapToken
//   - Value Objects: typed IDs, enum statuses, etc.
//   - Repository interfaces + sentinel errors (architecture layer)
//
// Per workforce/00-overview § 1 + § 5.
package workforce

import "errors"

// Typed identifiers (conventions § 0.3: typed alias, never bare string).
type (
	// WorkerID is the user-chosen unique identifier configured in worker.yaml.
	WorkerID string
)

// String returns the typed value as a plain string.
func (id WorkerID) String() string { return string(id) }

// WorkerStatus is the 2-state Worker status machine
// (workforce/01-worker § 1).
type WorkerStatus string

const (
	WorkerOnline  WorkerStatus = "online"
	WorkerOffline WorkerStatus = "offline"
)

// IsValid reports whether s matches a known status.
func (s WorkerStatus) IsValid() bool {
	switch s {
	case WorkerOnline, WorkerOffline:
		return true
	}
	return false
}

// String returns the underlying status string.
func (s WorkerStatus) String() string { return string(s) }

// OfflineReason categorises why a worker went offline.
type OfflineReason string

const (
	OfflineReasonHeartbeatTimeout OfflineReason = "heartbeat_timeout"
	OfflineReasonDisconnect       OfflineReason = "disconnect"
	OfflineReasonShutdown         OfflineReason = "shutdown"
)

// IsValid reports the enum membership.
func (r OfflineReason) IsValid() bool {
	switch r {
	case OfflineReasonHeartbeatTimeout, OfflineReasonDisconnect, OfflineReasonShutdown:
		return true
	}
	return false
}

// Capability is a Worker capability VO (v2; ADR-0023 § 4 + ADR-0030 § 4).
// Each entry represents one agent CLI: installation state (detected by
// worker auto-probe), user enable flag, version string (from probe), and
// per-CLI feature flags used by DispatchService feature-check (per ADR-0030
// § 5). All `Supports*` fields default to false (legacy probe path leaves
// them off until adapter Probe upgrades them).
type Capability struct {
	AgentCLI        string `json:"agent_cli"`
	Detected        bool   `json:"detected"`
	Enabled         bool   `json:"enabled"`
	Version         string `json:"version,omitempty"`
	SupportsMCP     bool   `json:"supports_mcp,omitempty"`
	SupportsSkills  bool   `json:"supports_skills,omitempty"`
	SupportsSession bool   `json:"supports_session,omitempty"`
}

// WorkerConcurrency captures Worker.concurrency (ADR-0023 § 3).
type WorkerConcurrency struct {
	PerAgentType int `json:"per_agent_type"`
}

// DefaultWorkerConcurrency mirrors the v2 default (2) per ADR-0023.
func DefaultWorkerConcurrency() WorkerConcurrency {
	return WorkerConcurrency{PerAgentType: 2}
}

// WorkerDiscovery captures Worker.discovery (ADR-0023 § 3).
// ScanInterval is stored as a duration string ("1h" / "30m") for human
// readability — workforce layer parses on use.
type WorkerDiscovery struct {
	ScanPaths    []string `json:"scan_paths"`
	Exclude      []string `json:"exclude"`
	ScanInterval string   `json:"scan_interval"`
}

// DefaultWorkerDiscovery mirrors the v2 default.
func DefaultWorkerDiscovery() WorkerDiscovery {
	return WorkerDiscovery{
		ScanPaths:    nil,
		Exclude:      nil,
		ScanInterval: "1h",
	}
}

// Workforce BC sentinel errors (architecture layer; impl returns these).
var (
	// Worker
	ErrWorkerNotFound           = errors.New("workforce: worker not found")
	ErrWorkerAlreadyExists      = errors.New("workforce: worker already enrolled")
	ErrWorkerVersionConflict    = errors.New("workforce: worker version conflict (optimistic lock)")
	ErrWorkerInvalidStatus      = errors.New("workforce: invalid worker status")
	ErrWorkerCapabilityNotFound = errors.New("workforce: worker capability (agent_cli) not found in detected list")
)
