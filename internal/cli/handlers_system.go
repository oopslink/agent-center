package cli

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"os/signal"
	"runtime/debug"
	"syscall"
	"time"

	"github.com/oopslink/agent-center/internal/config"
	"github.com/oopslink/agent-center/internal/observability/escalator"
	"github.com/oopslink/agent-center/internal/persistence"
	"github.com/oopslink/agent-center/internal/webconsole/sse"
)

// globalConfigPath is set by BuildRouter to the value of the global
// --config flag (extracted from os.Args). System commands fall back to
// this when their own --config flag is empty.
var globalConfigPath string

// SetGlobalConfigPath is called by BuildRouter to publish the global
// --config flag value to the system command handlers.
func SetGlobalConfigPath(p string) { globalConfigPath = p }

// GlobalConfigPath returns the resolved config path published by
// BuildRouter (--config flag or AGENT_CENTER_CONFIG env). Empty when
// neither is set.
func GlobalConfigPath() string { return globalConfigPath }

// SystemCommands returns top-level mode + admin commands.
//
// `server` / `migrate` actually run; `supervisor` / `worker` /
// `admin blob-migrate` are placeholder stubs per plan-1 § 3.1.4 — they
// exist so the CLI surface is stable and exit cleanly with reason
// `not_implemented_in_phase_1`.
func SystemCommands(buildVersion, buildCommit string) []*Command {
	return []*Command{
		{
			Name:    "version",
			Summary: "Print build info",
			Run: func(ctx context.Context, args []string, out, err io.Writer) ExitCode {
				if buildVersion == "" {
					if info, ok := debug.ReadBuildInfo(); ok {
						fmt.Fprintf(out, "agent-center %s\n", info.Main.Version)
					} else {
						fmt.Fprintln(out, "agent-center (dev)")
					}
				} else {
					fmt.Fprintf(out, "agent-center %s (commit %s)\n", buildVersion, buildCommit)
				}
				return ExitOK
			},
		},
	}
}

// ServerCommand returns the `server` mode command. It needs to construct
// its own deps (open DB, run migrations) because it's the entry point
// before any other command runs.
func ServerCommand() *Command {
	return &Command{
		Name:    "server",
		Summary: "Run the center daemon (Phase 1 minimal: migrate + idle)",
		Flags: func(fs *flag.FlagSet) Handler {
			cfgPath := fs.String("config", "", "config file path")
			listen := fs.String("listen", "", "override server.listen_addr")
			migrateOnly := fs.Bool("migrate-only", false, "run migrations and exit")
			return func(ctx context.Context, args []string, out, errw io.Writer) ExitCode {
				cfg, err := loadConfigForCLI(*cfgPath, map[string]string{
					"server.listen_addr": *listen,
				})
				if err != nil {
					emitConfigErrors(errw, err)
					return ExitUsage
				}
				db, err := OpenAndMigrate(cfg)
				if err != nil {
					fmt.Fprintf(errw, "Error: server_startup: %v\n", err)
					return ExitBusinessError
				}
				defer db.Close()
				if *migrateOnly {
					fmt.Fprintln(out, "migrations applied; exiting (--migrate-only)")
					return ExitOK
				}
				// Wire the always-on UnknownEventEscalator. Bridge BC
				// inbound + feishu adapter removed in P10 § 3.9 per
				// ADR-0031.
				app, err := NewApp(cfg, db, nil)
				if err != nil {
					fmt.Fprintf(errw, "Error: app_bootstrap: %v\n", err)
					return ExitBusinessError
				}
				esc := escalator.NewService(app.EventRepo, app.Sink, app.Clock, escalator.Config{
					Interval:  1 * time.Hour,
					Threshold: escalator.DefaultThreshold,
					Window:    escalator.DefaultWindow,
				})
				go esc.Run(ctx, func(err error) {
					fmt.Fprintf(errw, "[server] escalator: %v\n", err)
				})

				// v2.2-A2: admin endpoint (unix socket) — AppService
				// transport for in-process tools per conventions § 0.4.
				// Full 93-route surface populated from cli.App via
				// adminDepsFromApp.
				var adminCleanup func() error
				if sock := cfg.Server.AdminSocketPath; sock != "" {
					cleanup, aerr := runAdminEndpoint(ctx, app, sock, func(msg string) {
						fmt.Fprintf(errw, "[server] %s\n", msg)
					})
					if aerr != nil {
						fmt.Fprintf(errw, "Error: admin: %v\n", aerr)
						return ExitBusinessError
					}
					adminCleanup = cleanup
					defer func() {
						if adminCleanup != nil {
							_ = adminCleanup()
						}
					}()
				}

				// P11 § 3.2/3.3: Web Console HTTP + SSE.
				var webCleanup func() error
				webAddr := cfg.WebConsole.ListenAddr
				webEnabled := cfg.WebConsole.Enabled || webAddr != ""
				if webEnabled {
					if webAddr == "" {
						webAddr = "127.0.0.1:7100"
					}
					bus := sse.NewBus()
					cleanup, werr := runWebConsole(ctx, app, bus, webAddr, func(msg string) {
						fmt.Fprintf(errw, "[server] %s\n", msg)
					})
					if werr != nil {
						fmt.Fprintf(errw, "Error: webconsole: %v\n", werr)
						return ExitBusinessError
					}
					webCleanup = cleanup
					defer func() {
						if webCleanup != nil {
							_ = webCleanup()
						}
					}()
				}

				bannerWeb := "disabled"
				if webEnabled {
					bannerWeb = webAddr
				}
				bannerAdmin := "disabled"
				if cfg.Server.AdminSocketPath != "" {
					bannerAdmin = cfg.Server.AdminSocketPath
				}
				fmt.Fprintf(out, "agent-center server: db=%s listen=%s web=%s admin=%s (escalator running)\n",
					cfg.Server.SqlitePath, cfg.Server.ListenAddr, bannerWeb, bannerAdmin)
				// Wait for SIGINT / SIGTERM.
				sigCh := make(chan os.Signal, 1)
				signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)
				select {
				case s := <-sigCh:
					fmt.Fprintf(out, "signal %s received; shutting down\n", s)
				case <-ctx.Done():
					fmt.Fprintln(out, "context canceled; shutting down")
				}
				return ExitOK
			}
		},
	}
}

// MigrateGroupCommand is the parent of `migrate up` + `migrate
// v1-to-v2`. It carries no Run handler — invoking `migrate` alone
// prints help.
func MigrateGroupCommand() *Command {
	return &Command{
		Name:    "migrate",
		Summary: "Database migration commands (up / v1-to-v2)",
	}
}

// MigrateUpCommand replaces the v1-era top-level `migrate` leaf. It
// runs pending migrations against the configured SQLite file. Behavior
// is preserved verbatim from the v1 form (target=N supported).
func MigrateUpCommand() *Command {
	return &Command{
		Name:    "up",
		Summary: "Run pending migrations against the configured SQLite file",
		Flags: func(fs *flag.FlagSet) Handler {
			cfgPath := fs.String("config", "", "config file path")
			target := fs.Int("target", -1, "target version (negative = up to latest)")
			return func(ctx context.Context, args []string, out, errw io.Writer) ExitCode {
				cfg, err := loadConfigForCLI(*cfgPath, nil)
				if err != nil {
					emitConfigErrors(errw, err)
					return ExitUsage
				}
				db, err := persistence.Open(cfg.Server.SqlitePath)
				if err != nil {
					fmt.Fprintf(errw, "Error: db_open: %v\n", err)
					return ExitBusinessError
				}
				defer db.Close()
				m := persistence.NewMigrator(db)
				if *target < 0 {
					if err := m.Up(ctx); err != nil {
						fmt.Fprintf(errw, "Error: migrate_up: %v\n", err)
						return ExitBusinessError
					}
				} else {
					if err := m.Down(ctx, *target); err != nil {
						fmt.Fprintf(errw, "Error: migrate_down: %v\n", err)
						return ExitBusinessError
					}
				}
				v, _ := m.Version(ctx)
				fmt.Fprintf(out, "migration current version: %d\n", v)
				return ExitOK
			}
		},
	}
}

// SupervisorPlaceholder returns the `supervisor` mode stub.
func SupervisorPlaceholder() *Command {
	return placeholderCommand("supervisor",
		"Run a supervisor invocation (Phase 6)",
		"Cognition BC is implemented in Phase 6; this mode is reserved.",
	)
}

// WorkerRunPlaceholder returns the `worker run` daemon stub.
func WorkerRunPlaceholder() *Command {
	return placeholderCommand("run",
		"Run the worker daemon (Phase 2)",
		"Worker daemon (TaskRuntime) is implemented in Phase 2; this mode is reserved.",
	)
}

// WorkerShimPlaceholder is the `worker shim` entry point. v1 builds the
// shim runtime as a library (internal/shim) and offers a thin CLI hook
// that delegates to it. Daemon spawns shim via this entry; users do not
// directly invoke it (it's audience=Sys per 03-cli § 8.3).
func WorkerShimPlaceholder() *Command {
	return placeholderCommand("shim",
		"Per-execution shim entry (system; ADR-0018)",
		"The shim is spawned by the worker daemon. Direct invocation is "+
			"reserved for daemon-internal use; users should not run this. "+
			"See ADR-0018 for the lifecycle.",
	)
}

// AdminBlobMigratePlaceholder returns the `admin blob-migrate` stub.
func AdminBlobMigratePlaceholder() *Command {
	return placeholderCommand("blob-migrate",
		"Migrate BlobStore backend (Phase 2+)",
		"BlobStore is implemented in Phase 2+; this command is reserved.",
	)
}

func placeholderCommand(name, summary, longHelp string) *Command {
	return &Command{
		Name:     name,
		Summary:  summary,
		LongHelp: longHelp,
		Run: func(ctx context.Context, args []string, out, errw io.Writer) ExitCode {
			return PrintError(errw, "human", "not_implemented_in_phase_1",
				summary+" — see plans/README.md for the phase that adds this.",
				ExitNotImplemented)
		},
	}
}

func loadConfigForCLI(path string, flagOverrides map[string]string) (config.Config, error) {
	// Fall back to globalConfigPath when the local --config wasn't set.
	if path == "" {
		path = globalConfigPath
	}
	// Drop empty overrides so they don't shadow YAML / env.
	clean := map[string]string{}
	for k, v := range flagOverrides {
		if v != "" {
			clean[k] = v
		}
	}
	cfg, err := config.Load(config.LoadOptions{Path: path, FlagOverrides: clean})
	if err != nil {
		return config.Config{}, err
	}
	return cfg, nil
}

func emitConfigErrors(w io.Writer, err error) {
	reasons := config.AsErrorList(err)
	if len(reasons) == 0 {
		fmt.Fprintf(w, "Error: config: %v\n", err)
		return
	}
	for _, r := range reasons {
		fmt.Fprintf(w, "Error: config: %s\n", r)
	}
}

// Used by tests to silence the unused-import warning when SystemCommands
// has no build info wired (default path).
var _ = errors.New
