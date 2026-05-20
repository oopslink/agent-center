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

	"github.com/oopslink/agent-center/internal/config"
	"github.com/oopslink/agent-center/internal/persistence"
)

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
				fmt.Fprintf(out, "agent-center server: db=%s listen=%s (Phase 1: idle until SIGTERM)\n",
					cfg.Server.SqlitePath, cfg.Server.ListenAddr)
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

// MigrateCommand returns the `migrate` admin command.
func MigrateCommand() *Command {
	return &Command{
		Name:    "migrate",
		Summary: "Run migrations against the configured SQLite file",
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
