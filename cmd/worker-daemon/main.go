// Command worker-daemon is the standalone worker-daemon binary.
//
// v2.7 (b) cutover: the worker daemon now ships INSIDE the unified `agent-center`
// binary as `agent-center worker run` (so its os.Executable() can route the
// `worker agent-supervisor` / `worker mcp-host` subcommands it spawns — the
// spawn-bug fix). This standalone binary is now a THIN WRAPPER over the shared
// workerdaemon.RunDaemon bootstrap and is RETIRING — it is kept only so existing
// build/dev invocations keep working during the cutover; the deploy path (install)
// switches to `agent-center worker run` and this binary is removed in the
// follow-up slice. New deployments should use `agent-center worker run`.
//
// Flags are kept in strict parity with `agent-center worker run`.
package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/oopslink/agent-center/internal/workerdaemon"
)

func main() {
	var (
		cfgPath    = flag.String("config", "", "path to agent-center.yaml")
		workerID   = flag.String("worker-id", "", "worker identity (required)")
		workerName = flag.String("worker-name", "",
			"operator-facing friendly label set at enroll time (v2.4-D-X1); blank defaults to worker-id server-side")
		fakeAgent    = flag.String("fake-agent", "", "override path for the 'fakeagent' agent_cli (e2e tests)")
		pollInterval = flag.Duration("poll-interval", 1*time.Second, "queue poll interval")
		capsFlag     = flag.String("capabilities", "", "comma-separated capability list")
		adminToken   = flag.String("admin-token", "",
			"admin bearer token (required by v2.3-3a auth); falls back to AGENT_CENTER_ADMIN_TOKEN env")
		adminTarget = flag.String("admin-target", "",
			"admin endpoint, e.g. unix:/run/admin.sock or tcp://host:7300 (default: cfg.server.admin_socket_path)")
		serverFingerprint = flag.String("server-fingerprint", "",
			"sha256:HH:HH:... pinned server cert fingerprint (required with --admin-target=tcp://...); falls back to AGENT_CENTER_SERVER_FINGERPRINT env")
		skillsDir = flag.String("skills-dir", "",
			"directory containing worker-agent.md + extra skills (real-agent dispatch)")
		useControlLoop = flag.Bool("use-control-loop", false,
			"v2.7 D2-f: run the new control-stream execution path (disables the legacy dispatch loop)")
	)
	flag.Parse()

	if strings.TrimSpace(*workerID) == "" {
		fmt.Fprintln(os.Stderr, "[worker] --worker-id is required")
		os.Exit(2)
	}

	logf := func(msg string) { fmt.Fprintf(os.Stderr, "[worker] %s\n", msg) }
	err := workerdaemon.RunDaemon(context.Background(), workerdaemon.RunOptions{
		ConfigPath:        *cfgPath,
		WorkerID:          *workerID,
		WorkerName:        *workerName,
		FakeAgent:         *fakeAgent,
		PollInterval:      *pollInterval,
		CapabilitiesCSV:   *capsFlag,
		AdminToken:        *adminToken,
		AdminTarget:       *adminTarget,
		ServerFingerprint: *serverFingerprint,
		SkillsDir:         *skillsDir,
		UseControlLoop:    *useControlLoop,
	}, logf)
	if err != nil {
		if workerdaemon.IsShutdownError(err) {
			logf(err.Error())
			os.Exit(0)
		}
		logf("fatal: " + err.Error())
		os.Exit(1)
	}
	logf("shutdown complete")
}
