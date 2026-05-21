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
	SetGlobalConfigPath(cfgPath)

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
	// Phase 6: `supervisor` is now a real subcommand group with a default
	// Run handler (the per-invocation subprocess loop) plus `retrigger`.
	if err := router.Add(nil, SupervisorRunCommand()); err != nil {
		return nil, "", err
	}
	if err := router.Add([]string{"admin"}, AdminBlobMigratePlaceholder()); err != nil {
		return nil, "", err
	}
	// Phase 7: `bootstrap check-systemd` (no DB; runs at install time).
	if err := router.Add(nil, BootstrapCommand()); err != nil {
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

	// task group
	for _, c := range provider.taskCommands() {
		if err := router.Add([]string{"task"}, c); err != nil {
			return nil, "", err
		}
	}
	// dispatch / kill-execution (top-level)
	if err := router.Add(nil, provider.dispatchCommand()); err != nil {
		return nil, "", err
	}
	if err := router.Add(nil, provider.killExecutionCommand()); err != nil {
		return nil, "", err
	}
	// agent CLI commands (request-input / report-* / read-task-context)
	for _, c := range provider.agentRuntimeCommands() {
		if err := router.Add(nil, c); err != nil {
			return nil, "", err
		}
	}
	// issue group + top-level open-issue (agent audience)
	for _, c := range provider.issueCommands() {
		if err := router.Add([]string{"issue"}, c); err != nil {
			return nil, "", err
		}
	}
	if err := router.Add(nil, provider.openIssueCommand()); err != nil {
		return nil, "", err
	}
	// worker shim placeholder (system audience)
	if err := router.Add([]string{"worker"}, WorkerShimPlaceholder()); err != nil {
		return nil, "", err
	}
	// Observability verbs (top-level): inspect / query / ps / stats / logs / peek-trace
	for _, c := range provider.observabilityCommands() {
		if err := router.Add(nil, c); err != nil {
			return nil, "", err
		}
	}

	// Phase 5: identity + bridge command trees.
	for _, c := range provider.identityCommands() {
		if err := router.Add([]string{"identity"}, c); err != nil {
			return nil, "", err
		}
	}
	for _, c := range provider.bridgeCommands() {
		if err := router.Add([]string{"bridge"}, c); err != nil {
			return nil, "", err
		}
	}

	// Phase 7: `admin backup`.
	for _, c := range provider.adminCommands() {
		if err := router.Add([]string{"admin"}, c); err != nil {
			return nil, "", err
		}
	}

	// Phase 6: supervisor retrigger / record-decision / escalate-input-request.
	if err := router.Add([]string{"supervisor"}, provider.supervisorRetriggerCommand()); err != nil {
		return nil, "", err
	}
	if err := router.Add(nil, provider.recordDecisionCommand()); err != nil {
		return nil, "", err
	}
	if err := router.Add(nil, provider.escalateInputRequestCommand()); err != nil {
		return nil, "", err
	}
	return router, cfgPath, nil
}

func (l *lazyApp) supervisorRetriggerCommand() *Command {
	return l.withApp(func(a *App) *Command { return a.SupervisorRetriggerCommand() })
}

func (l *lazyApp) recordDecisionCommand() *Command {
	return l.withApp(func(a *App) *Command { return a.RecordDecisionCommand() })
}

func (l *lazyApp) escalateInputRequestCommand() *Command {
	return l.withApp(func(a *App) *Command { return a.EscalateInputRequestCommand() })
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

func (l *lazyApp) taskCommands() []*Command {
	names := []string{"create", "bind-conversation", "unbind-conversation"}
	out := make([]*Command, 0, len(names))
	for _, n := range names {
		n := n
		out = append(out, l.withApp(func(a *App) *Command {
			return findCmd(a.TaskCommands(), n)
		}))
	}
	return out
}

func (l *lazyApp) dispatchCommand() *Command {
	return l.withApp(func(a *App) *Command { return a.DispatchCommand() })
}

func (l *lazyApp) killExecutionCommand() *Command {
	return l.withApp(func(a *App) *Command { return a.KillExecutionCommand() })
}

func (l *lazyApp) agentRuntimeCommands() []*Command {
	names := []string{"request-input", "report-progress", "report-artifact", "report-failure", "read-task-context"}
	out := make([]*Command, 0, len(names))
	for _, n := range names {
		n := n
		out = append(out, l.withApp(func(a *App) *Command {
			return findCmd(a.AgentRuntimeCommands(), n)
		}))
	}
	return out
}

func (l *lazyApp) issueCommands() []*Command {
	names := []string{"open", "comment", "conclude", "withdraw", "bind-conversation", "link-conversation"}
	out := make([]*Command, 0, len(names))
	for _, n := range names {
		n := n
		out = append(out, l.withApp(func(a *App) *Command {
			return findCmd(a.IssueCommands(), n)
		}))
	}
	return out
}

func (l *lazyApp) openIssueCommand() *Command {
	return l.withApp(func(a *App) *Command { return a.OpenIssueCommand() })
}

func (l *lazyApp) observabilityCommands() []*Command {
	names := []string{"inspect", "query", "ps", "stats", "logs", "peek-trace"}
	out := make([]*Command, 0, len(names))
	for _, n := range names {
		n := n
		out = append(out, l.withApp(func(a *App) *Command {
			return findCmd(a.ObservabilityCommands(), n)
		}))
	}
	return out
}

func (l *lazyApp) identityCommands() []*Command {
	names := []string{"add", "list", "bind", "unbind"}
	out := make([]*Command, 0, len(names))
	for _, n := range names {
		n := n
		out = append(out, l.withApp(func(a *App) *Command {
			return findCmd(a.IdentityCommands(), n)
		}))
	}
	return out
}

func (l *lazyApp) adminCommands() []*Command {
	names := []string{"backup"}
	out := make([]*Command, 0, len(names))
	for _, n := range names {
		n := n
		out = append(out, l.withApp(func(a *App) *Command {
			return findCmd(a.AdminCommands(), n)
		}))
	}
	return out
}

func (l *lazyApp) bridgeCommands() []*Command {
	// `bridge feishu setup` is a deep tree (bridge → feishu → setup);
	// we register the `feishu` group node so its subcommands attach
	// under the bridge group provided by BuildRouter.
	feishuGroup := &Command{Name: "feishu", Summary: "FeishuBridge management"}
	feishuGroup.Subcommands = append(feishuGroup.Subcommands,
		l.withApp(func(a *App) *Command {
			cmds := a.BridgeCommands()
			feishu := findCmd(cmds, "feishu")
			if feishu == nil {
				return nil
			}
			return findCmd(feishu.Subcommands, "setup")
		}),
	)
	return []*Command{feishuGroup}
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
