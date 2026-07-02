package workforce

import (
	"encoding/json"
	"strings"
)

// SystemInfo is the worker-reported host + build identity value object
// (task T752 / Worker Profile). It is collected worker-side (os.Hostname,
// runtime.GOOS/GOARCH, the executable path, and the linker-injected build
// version) and uploaded on every online via the capabilities report, so the
// Environment → Workers → Profile page can show real host facts instead of the
// v2.9 "Coming in v2.9" placeholders.
//
// All fields are optional: an older worker (pre-T752) reports none, and the
// zero value marshals to an empty JSON object — the Profile page falls back to
// its deferred placeholder per field, so absence stays honest (no fake values).
type SystemInfo struct {
	Hostname           string `json:"hostname,omitempty"`
	OS                 string `json:"os,omitempty"`
	Arch               string `json:"arch,omitempty"`
	AgentCenterVersion string `json:"agent_center_version,omitempty"`
	InstallPath        string `json:"install_path,omitempty"`
	// WorkerVersion is the worker process's OWN build identity (distinct from
	// AgentCenterVersion: the same binary can be a specific commit-level build
	// of a given agent-center release).
	WorkerVersion string `json:"worker_version,omitempty"`
}

// IsZero reports whether no field is set (worker reported nothing).
func (s SystemInfo) IsZero() bool {
	return strings.TrimSpace(s.Hostname) == "" &&
		strings.TrimSpace(s.OS) == "" &&
		strings.TrimSpace(s.Arch) == "" &&
		strings.TrimSpace(s.AgentCenterVersion) == "" &&
		strings.TrimSpace(s.InstallPath) == "" &&
		strings.TrimSpace(s.WorkerVersion) == ""
}

// systemInfoJSON marshals a SystemInfo for storage. The zero value serialises
// to "{}" (never null) so the column always holds valid JSON.
func systemInfoJSON(s SystemInfo) ([]byte, error) {
	return json.Marshal(s)
}

// ParseSystemInfo reconstructs a SystemInfo from its stored JSON. An empty
// string yields the zero value (older rows predate the column). Exported for
// repository round-trip in the sqlite package.
func ParseSystemInfo(raw string) (SystemInfo, error) {
	var si SystemInfo
	if strings.TrimSpace(raw) == "" {
		return si, nil
	}
	if err := json.Unmarshal([]byte(raw), &si); err != nil {
		return SystemInfo{}, err
	}
	return si, nil
}
