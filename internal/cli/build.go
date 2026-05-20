package cli

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/oopslink/agent-center/internal/config"
	"github.com/oopslink/agent-center/internal/persistence"
)

// WriterAlias is io.Writer; aliased so cmd/agent-center can refer to it
// without re-importing io.
type WriterAlias = io.Writer

// BuildRouter constructs the full command tree.
//
// The router opens the DB lazily — only when a resource command is run
// (i.e. workforce / conversation handlers). `version` / `--help` /
// `supervisor` / `worker run` / `admin blob-migrate` placeholders all
// skip DB construction.
//
// Returns the router + the resolved config path so main.go can strip
// the --config flag from args before dispatching.
func BuildRouter(buildVersion, buildCommit string, args []string) (*Router, string, error) {
	cfgPath := extractConfigFlag(args)

	router := NewRouter("agent-center")
	router.Root.Summary = "agent-center — single binary, multi-mode CLI (Phase 1)"

	// System / mode commands (no DB needed).
	for _, c := range SystemCommands(buildVersion, buildCommit) {
		if err := router.Add(nil, c); err != nil {
			return nil, "", err
		}
	}
	if err := router.Add(nil, ServerCommand()); err != nil {
		return nil, "", err
	}
	if err := router.Add(nil, MigrateCommand()); err != nil {
		return nil, "", err
	}
	if err := router.Add(nil, SupervisorPlaceholder()); err != nil {
		return nil, "", err
	}
	if err := router.Add([]string{"admin"}, AdminBlobMigratePlaceholder()); err != nil {
		return nil, "", err
	}

	// Resource commands. We use a lazy *App provider so each invocation
	// opens / closes the DB.
	provider := &lazyApp{cfgPath: cfgPath}

	// worker group
	for _, c := range provider.workerCommands() {
		if err := router.Add([]string{"worker"}, c); err != nil {
			return nil, "", err
		}
	}
	if err := router.Add([]string{"worker"}, WorkerRunPlaceholder()); err != nil {
		return nil, "", err
	}

	// project group
	for _, c := range provider.projectCommands() {
		if err := router.Add([]string{"project"}, c); err != nil {
			return nil, "", err
		}
	}

	// conversation group
	for _, c := range provider.conversationCommands() {
		if err := router.Add([]string{"conversation"}, c); err != nil {
			return nil, "", err
		}
	}
	return router, cfgPath, nil
}

// StripGlobalFlags removes the global --config / -c flags from args
// because they're handled out-of-band by BuildRouter.
func StripGlobalFlags(args []string, cfgPath string) []string {
	out := make([]string, 0, len(args))
	for i := 0; i < len(args); i++ {
		a := args[i]
		// Skip --config=<x>, --config <x>, -c=<x>, -c <x>.
		if a == "--config" || a == "-c" {
			i++ // skip value
			continue
		}
		if strings.HasPrefix(a, "--config=") || strings.HasPrefix(a, "-c=") {
			continue
		}
		out = append(out, a)
	}
	return out
}

// extractConfigFlag finds --config=... / -c=... / --config X / -c X in
// args and returns its value (or empty).
func extractConfigFlag(args []string) string {
	for i := 0; i < len(args); i++ {
		a := args[i]
		switch {
		case a == "--config" || a == "-c":
			if i+1 < len(args) {
				return args[i+1]
			}
		case strings.HasPrefix(a, "--config="):
			return strings.TrimPrefix(a, "--config=")
		case strings.HasPrefix(a, "-c="):
			return strings.TrimPrefix(a, "-c=")
		}
	}
	return os.Getenv("AGENT_CENTER_CONFIG")
}

// lazyApp opens the DB once per invocation of a resource command.
type lazyApp struct {
	cfgPath string
}

// build opens the DB + constructs the App. Caller is responsible for
// closing app.DB after use.
func (l *lazyApp) build() (*App, error) {
	cfg, err := config.Load(config.LoadOptions{Path: l.cfgPath})
	if err != nil {
		return nil, fmt.Errorf("config: %w", err)
	}
	db, err := persistence.Open(cfg.Server.SqlitePath)
	if err != nil {
		return nil, fmt.Errorf("open sqlite %q: %w", cfg.Server.SqlitePath, err)
	}
	if err := persistence.NewMigrator(db).Up(context.Background()); err != nil {
		_ = db.Close()
		return nil, fmt.Errorf("migrate: %w", err)
	}
	return NewApp(cfg, db, nil)
}

// withApp wraps a Handler-builder so the App is opened on first run.
func (l *lazyApp) withApp(build func(app *App) *Command) *Command {
	// We need to call build NOW so flags are registered, but with a fake
	// command. Instead, we delegate: each command method on lazyApp
	// returns the underlying *Command with its Flags hook wrapped.
	dummy, err := buildPlaceholderApp()
	if err != nil {
		// shouldn't happen in practice (placeholder uses :memory:)
		panic(fmt.Sprintf("lazyApp: build placeholder app: %v", err))
	}
	cmd := build(dummy)
	// Discard the dummy's handler; replace cmd with a Run-only command
	// that does its own arg parsing once a real App is built. We can't
	// reuse the Flags hook because router parses against the outer
	// flagset before our handler runs.
	return &Command{
		Name:    cmd.Name,
		Summary: cmd.Summary,
		LongHelp: cmd.LongHelp,
		Run: func(ctx context.Context, args []string, out, errw WriterAlias) ExitCode {
			real, err := l.build()
			if err != nil {
				return PrintError(errw, "human", "bootstrap_error", err.Error(), ExitBusinessError)
			}
			defer real.DB.Close()
			realCmd := build(real)
			if realCmd.Run != nil {
				return realCmd.Run(ctx, args, out, errw)
			}
			if realCmd.Flags == nil {
				return PrintError(errw, "human", "internal_error",
					"command has no handler", ExitNotImplemented)
			}
			fs2 := flag.NewFlagSet(realCmd.Name, flag.ContinueOnError)
			fs2.SetOutput(io.Discard)
			realHandler := realCmd.Flags(fs2)
			positionals, err := permissiveParse(fs2, args)
			if err != nil {
				return PrintError(errw, "human", "usage_error", err.Error(), ExitUsage)
			}
			return realHandler(ctx, positionals, out, errw)
		},
	}
}

func (l *lazyApp) workerCommands() []*Command {
	out := []*Command{}
	out = append(out,
		l.withApp(func(a *App) *Command {
			c := a.WorkerCommands()
			return findCmd(c, "enroll")
		}),
		l.withApp(func(a *App) *Command {
			c := a.WorkerCommands()
			return findCmd(c, "list")
		}),
		l.withApp(func(a *App) *Command {
			c := a.WorkerCommands()
			return findCmd(c, "status")
		}),
	)
	// proposal subtree
	proposalGroup := &Command{Name: "proposal", Summary: "Manage proposals"}
	for _, sub := range []string{"list", "show", "propose", "accept", "ignore", "unignore"} {
		s := sub
		proposalGroup.Subcommands = append(proposalGroup.Subcommands,
			l.withApp(func(a *App) *Command {
				c := a.WorkerCommands()
				prop := findCmd(c, "proposal")
				return findCmd(prop.Subcommands, s)
			}),
		)
	}
	out = append(out, proposalGroup)
	return out
}

func (l *lazyApp) projectCommands() []*Command {
	names := []string{"add", "list", "show", "update", "remove"}
	out := make([]*Command, 0, len(names))
	for _, n := range names {
		n := n
		out = append(out, l.withApp(func(a *App) *Command {
			return findCmd(a.ProjectCommands(), n)
		}))
	}
	return out
}

func (l *lazyApp) conversationCommands() []*Command {
	names := []string{"open", "add-message", "list", "read", "close"}
	out := make([]*Command, 0, len(names))
	for _, n := range names {
		n := n
		out = append(out, l.withApp(func(a *App) *Command {
			return findCmd(a.ConversationCommands(), n)
		}))
	}
	return out
}

func findCmd(cs []*Command, name string) *Command {
	for _, c := range cs {
		if c.Name == name {
			return c
		}
	}
	return nil
}

// buildPlaceholderApp constructs a no-op App used during flag scaffolding.
// It opens an in-memory DB + runs migrations so all repos / services
// initialise; we never actually run handler logic against it.
func buildPlaceholderApp() (*App, error) {
	db, err := persistence.Open(persistence.MemoryDSN())
	if err != nil {
		return nil, err
	}
	if err := persistence.NewMigrator(db).Up(context.Background()); err != nil {
		return nil, err
	}
	cfg := config.DefaultConfig()
	return NewApp(cfg, db, nil)
}

// _ keeps errors imported (used by lazyApp callers).
var _ = errors.New
