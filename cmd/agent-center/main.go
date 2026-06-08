// Command agent-center is the unified CLI binary covering server,
// supervisor, worker daemon modes plus all admin commands (conventions §
// 10).
//
// Phase 1: server / migrate / version are real; supervisor / worker run /
// admin blob-migrate are stubs that exit 64 with reason
// `not_implemented_in_phase_1`.
package main

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"syscall"

	"github.com/oopslink/agent-center/internal/cli"
)

// build-time variables (overridden via -ldflags in release builds).
var (
	buildVersion = "dev"
	buildCommit  = "unknown"
	buildBranch  = "unknown"
	buildBuiltAt = "unknown"
)

func main() {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// Forward SIGINT/SIGTERM to ctx so long-running modes shut down.
	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)
	go func() {
		<-sigCh
		cancel()
	}()

	args := os.Args[1:]
	// v2.4-D-X1 fix: pass the linker-injected version into the install
	// command so it prints "AgentCenter v2.4.0 installed" instead of
	// the "dev" sentinel for tagged builds.
	cli.SetInstallBuildVersion(buildVersion)
	// v2.7.1 #234: thread the linker-injected commit so `install/upgrade`
	// can swap on a same-version-different-commit rebuild.
	cli.SetInstallBuildCommit(buildCommit)
	// v2.8.1: thread branch + build timestamp for GET /api/system/version
	// (Settings version panel; version convention ${branch}-${commit}).
	cli.SetInstallBuildBranch(buildBranch)
	cli.SetInstallBuildBuiltAt(buildBuiltAt)
	router, configPath, err := cli.BuildRouter(buildVersion, buildCommit, args)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error: build_router: %v\n", err)
		os.Exit(int(cli.ExitBusinessError))
	}
	code := router.Run(ctx, cli.StripGlobalFlags(args, configPath))
	os.Exit(int(code))
}
