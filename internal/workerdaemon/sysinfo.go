package workerdaemon

import (
	"os"
	"runtime"
	"strings"

	"github.com/oopslink/agent-center/internal/workforce"
)

// collectSystemInfo gathers the worker host + build identity reported to the
// center on every online (T752 Worker Profile). Host facts come from the OS;
// the two version strings are threaded in from the CLI build seams (the daemon
// package cannot see the linker-injected version directly).
//
// Every field is best-effort: a probe that fails (e.g. os.Hostname error) is
// simply left blank so the Profile page falls back to its per-field placeholder
// rather than showing a fake value.
func collectSystemInfo(agentCenterVersion, workerVersion string) workforce.SystemInfo {
	info := workforce.SystemInfo{
		OS:                 runtime.GOOS,
		Arch:               runtime.GOARCH,
		AgentCenterVersion: strings.TrimSpace(agentCenterVersion),
		WorkerVersion:      strings.TrimSpace(workerVersion),
	}
	if h, err := os.Hostname(); err == nil {
		info.Hostname = strings.TrimSpace(h)
	}
	// The install path is the running worker binary's absolute path (the daemon
	// runs inside the unified agent-center binary — this is where it was
	// launched from, i.e. the install location).
	if exe, err := os.Executable(); err == nil {
		info.InstallPath = strings.TrimSpace(exe)
	}
	return info
}
