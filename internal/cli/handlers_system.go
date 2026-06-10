package cli

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"os/signal"
	"path/filepath"
	"runtime/debug"
	"strconv"
	"syscall"
	"time"

	agentservice "github.com/oopslink/agent-center/internal/agent/service"
	"github.com/oopslink/agent-center/internal/config"
	"github.com/oopslink/agent-center/internal/observability"
	"github.com/oopslink/agent-center/internal/observability/escalator"
	"github.com/oopslink/agent-center/internal/persistence"
	"github.com/oopslink/agent-center/internal/webconsole/sse"
	wfservice "github.com/oopslink/agent-center/internal/workforce/service"
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

				// v2.3-3a (task #28): ensure the admin endpoint has at
				// least one valid bearer token. EnsureBootstrapToken is
				// a no-op when admin_tokens is non-empty; on a fresh
				// deploy it mints a system superuser token + writes
				// plaintext to <sqlite_dir>/bootstrap_token (0600).
				//
				// We pass context.Background() instead of the boot
				// context because bootstrap is an atomic schema-level
				// operation: a canceled ctx (e.g. SIGINT during tests
				// that exercise the cancel path) would mean we shut
				// down without writing the token — but the admin
				// endpoint never starts in that case either, so the
				// missing token is irrelevant. Decoupling avoids
				// spurious admin_bootstrap errors during cancellation.
				if berr := EnsureBootstrapToken(context.Background(), app, "", func(msg string) {
					fmt.Fprintf(out, "[server] %s\n", msg)
				}); berr != nil {
					fmt.Fprintf(errw, "Error: admin_bootstrap: %v\n", berr)
					return ExitBusinessError
				}

				// v2.2-A2 + v2.3-7a (task #27): admin endpoint — AppService
				// transport for in-process tools (unix socket) and
				// optional cross-host tools (TCP+TLS) per conventions
				// § 0.4. Full 93-route surface populated from cli.App
				// via adminDepsFromApp.
				var adminCleanup func() error
				var adminInfo AdminTransportInfo
				if cfg.Server.AdminSocketPath != "" || cfg.Server.AdminTCPListen != "" {
					tc := adminTransportFromCfg(cfg)
					info, cleanup, aerr := runAdminEndpoint(ctx, app, tc, func(msg string) {
						fmt.Fprintf(errw, "[server] %s\n", msg)
					})
					if aerr != nil {
						fmt.Fprintf(errw, "Error: admin: %v\n", aerr)
						return ExitBusinessError
					}
					adminInfo = info
					adminCleanup = cleanup
					defer func() {
						if adminCleanup != nil {
							_ = adminCleanup()
						}
					}()
					// v2.3-7a: emit cert-expiry-warning observability
					// event if the cert is within 30 days of expiry.
					if adminInfo.TLSExpiryWarn && app.Sink != nil {
						_, _ = app.Sink.Emit(ctx, observability.EmitCommand{
							EventType: "admin.tcp_cert_expiring",
							Actor:     app.operatorActor(),
							Payload: map[string]any{
								"cert_path":      cfg.Server.AdminTLSCertPath,
								"expires_at":     adminInfo.TLSCertNotAfter.UTC().Format(time.RFC3339),
								"days_remaining": adminInfo.TLSExpiryDays,
							},
						})
					}
				}

				// P11 § 3.2/3.3: Web Console HTTP + SSE. Default-on per
				// v2.2 (config.DefaultConfig sets enabled=true +
				// listen_addr=127.0.0.1:7100). Operators opt out with
				// explicit `web_console: {enabled: false}`. The
				// listen_addr fallback covers configs that wipe both
				// fields back to zero (unusual but possible).
				var webCleanup func() error
				webAddr := cfg.WebConsole.ListenAddr
				webEnabled := cfg.WebConsole.Enabled
				if webEnabled {
					if webAddr == "" {
						webAddr = "127.0.0.1:7100"
					}
					// v2.4-D-F3 fix: pass admin TCP fingerprint + public
					// bootstrap host through to the Web Console so the
					// AddWorkerModal can render a working install command.
					enrollWiring := WebConsoleEnrollWiring{
						// v2.7 #200: bootstrap_public_url (if set) wins over the bind
						// address so remote workers get a reachable Add Worker command.
						BootstrapHost: resolveEnrollBootstrapHost(cfg.Server.BootstrapPublicURL, cfg.Server.AdminTCPListen),
						Fingerprint:   adminInfo.TLSFingerprint,
					}
					bus := sse.NewBus()
					cleanup, werr := runWebConsole(ctx, app, bus, webAddr, enrollWiring, func(msg string) {
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

				// v2.4-D-X1 fix B8: worker heartbeat reconciler. Scans
				// online workers every 30s; flips to offline when
				// last_heartbeat_at goes stale (default 60s). Without
				// this, a worker that stopped heartbeating stayed
				// pinned at `online` forever — Fleet view lied about
				// the cluster state.
				reconciler := wfservice.NewHeartbeatReconciler(app.WorkerRepo, app.Sink, nil, 0, 0)
				reconcilerCtx, reconcilerCancel := context.WithCancel(ctx)
				go func() {
					_ = reconciler.Run(reconcilerCtx, app.operatorActor())
				}()
				defer reconcilerCancel()

				// v2.8.1 #278 D PR5: work-item reconciler. Releases an agent's
				// active WorkItem once the agent has been inactive for the stale-age
				// (default 30 min; AGENT_CENTER_WORKITEM_STALE_MINUTES overrides for
				// testing), freeing the single-active slot so a hung/dead agent's
				// task can be retried instead of wedging forever.
				wiStaleAge := agentservice.WorkItemReconcileDefaultStaleAge
				if v := os.Getenv("AGENT_CENTER_WORKITEM_STALE_MINUTES"); v != "" {
					if m, perr := strconv.Atoi(v); perr == nil && m > 0 {
						wiStaleAge = time.Duration(m) * time.Minute
					}
				}
				wiReconciler := agentservice.NewWorkItemReconciler(
					app.AgentWorkItemRepo, app.AgentActivityRepo, nil, wiStaleAge, 0,
					func(msg string, a ...any) { fmt.Fprintf(out, "[work-item-reconcile] "+msg+"\n", a...) },
				)
				wiReconcilerCtx, wiReconcilerCancel := context.WithCancel(ctx)
				go func() {
					_ = wiReconciler.Run(wiReconcilerCtx)
				}()
				defer wiReconcilerCancel()

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
				// v2.3-7a banner: if TCP listener is enabled, print its
				// address + cert fingerprint + expiry so operators can
				// hand the fingerprint to clients (worker / CLI) for
				// pinning. Cert generation status surfaces "auto-generated"
				// vs "loaded existing" so the operator knows.
				if cfg.Server.AdminTCPListen != "" {
					gen := "loaded existing"
					if adminInfo.TLSCertGenerated {
						gen = "auto-generated"
					}
					fmt.Fprintf(out, "  admin tcp:  %s (TLS, %s)\n              cert valid until %s (%d days)\n              fingerprint: %s\n",
						cfg.Server.AdminTCPListen,
						gen,
						adminInfo.TLSCertNotAfter.UTC().Format("2006-01-02"),
						adminInfo.TLSExpiryDays,
						adminInfo.TLSFingerprint,
					)
				}
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

// WorkerShimPlaceholder is the `worker shim` entry point. The shim
// runtime is daemon-internal; this is a thin CLI hook. Daemon spawns shim
// via this entry; users do not directly invoke it (audience=Sys per
// 03-cli § 8.3).
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
	// v2.7 #199 follow-up: with no explicit --config (and no global/env), prefer
	// the user-mode install config (~/.agent-center/etc/config.yaml) over the
	// built-in defaults. #199 makes the operator run `agent-center server` in the
	// foreground; without this, a bare run fell back to the system /var/lib paths
	// (which need root) and failed to start in user mode.
	if path == "" {
		path = discoverDefaultConfigPath()
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

// discoverDefaultConfigPath returns the user-mode install config path
// (~/.agent-center/etc/config.yaml) when it exists, else "" (v2.7 #199 follow-up).
// Lets a bare `agent-center server` (the foreground run command #199 prints) pick
// up the install's config instead of the built-in system /var/lib defaults that
// need root. A missing file → "" → fall through to config.Load's defaults.
func discoverDefaultConfigPath() string {
	p := filepath.Join(defaultInstallPrefix(true), "etc", "config.yaml")
	if _, err := os.Stat(p); err == nil {
		return p
	}
	return ""
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
