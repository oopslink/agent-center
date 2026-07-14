package cli

import (
	"context"
	"crypto/tls"
	"errors"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/oopslink/agent-center/internal/admin/api"
	"github.com/oopslink/agent-center/internal/admintoken"
	"github.com/oopslink/agent-center/internal/cognition/memory/centergit"
	"github.com/oopslink/agent-center/internal/config"
	"github.com/oopslink/agent-center/internal/observability"
	pmsql "github.com/oopslink/agent-center/internal/projectmanager/sqlite"
	teamservice "github.com/oopslink/agent-center/internal/team/service"
	teamsql "github.com/oopslink/agent-center/internal/team/sqlite"
)

// AdminTransportConfig captures the v2.3-7a (task #27) admin listener
// configuration: optional unix socket + optional TCP+TLS address with
// auto-managed cert + fingerprint files. At least one of SocketPath or
// TCPListenAddr must be non-empty.
type AdminTransportConfig struct {
	SocketPath      string
	TCPListenAddr   string
	TLSCertPath     string
	TLSKeyPath      string
	FingerprintPath string
	Hostname        string
}

// AdminTransportInfo is what runAdminEndpoint returns to the caller
// (boot banner code in handlers_system.go uses this to print the cert
// fingerprint, expiry, etc.).
type AdminTransportInfo struct {
	TLSFingerprint   string
	TLSCertNotAfter  time.Time
	TLSCertGenerated bool
	TLSExpiryWarn    bool
	TLSExpiryDays    int
}

// runAdminEndpoint starts the v2.2 admin unix-socket server (and, since
// v2.3-7a, an optional concurrent TCP+TLS listener). v2.2-A1 scaffolded
// the unix listener + health endpoint; v2.2-A2 mounts the full CLI
// AppService surface via api.HandlerDeps populated from the App; v2.3-7a
// (task #27) adds the TCP+TLS leg for cross-host worker / CLI access.
//
// Returns a cleanup function that shuts the server down + removes the
// socket file. cleanup is non-nil even on error so the caller can
// always defer it safely.
func runAdminEndpoint(ctx context.Context, app *App, tc AdminTransportConfig, logger func(string)) (info AdminTransportInfo, cleanup func() error, err error) {
	noopCleanup := func() error { return nil }
	if tc.SocketPath == "" && tc.TCPListenAddr == "" {
		return AdminTransportInfo{}, noopCleanup, errors.New("admin: at least one of socket_path or tcp_listen required")
	}
	if app == nil {
		return AdminTransportInfo{}, noopCleanup, errors.New("admin: app nil")
	}

	var (
		tlsCert        *tls.Certificate
		tlsFingerprint string
		tlsGenerated   bool
	)
	if tc.TCPListenAddr != "" {
		var lerr error
		tlsCert, tlsFingerprint, tlsGenerated, lerr = api.LoadOrGenerateCert(tc.TLSCertPath, tc.TLSKeyPath, tc.Hostname)
		if lerr != nil {
			return AdminTransportInfo{}, noopCleanup, lerr
		}
		// Best-effort fingerprint file write — never block boot.
		if tc.FingerprintPath != "" {
			if werr := api.WriteFingerprintFile(tc.FingerprintPath, tlsFingerprint); werr != nil {
				logger("admin: write fingerprint file: " + werr.Error())
			}
		}
		info.TLSCertGenerated = tlsGenerated
		info.TLSFingerprint = tlsFingerprint
		info.TLSCertNotAfter = api.CertNotAfter(tlsCert)
		info.TLSExpiryWarn, info.TLSExpiryDays = api.CertExpiryWarning(tlsCert)
	}

	deps := adminDepsFromApp(app)
	// Center-hosted git smart-HTTP (design §4.2/§4.3): mount the centergit handler
	// at /admin/git/ behind the same bearer auth. Git authz is backed by the LIVE
	// team tables (teamRepoMembership). Best-effort: if git-http-backend can't be
	// located the route degrades to 501 rather than failing boot. The global repo
	// is provisioned up front so it is immediately readable by every agent.
	var gitHandler http.Handler
	if deps.TeamGitHost != nil {
		membership := api.NewTeamMembership(teamsql.NewRepo(app.DB), app.AgentRepo)
		gh, gerr := api.NewGitHandler(deps.TeamGitHost, membership)
		if gerr != nil {
			logger("admin: center git disabled: " + gerr.Error())
		} else {
			gitHandler = gh
			if perr := deps.TeamGitHost.EnsureRepo(ctx, centergit.GlobalRepo()); perr != nil {
				logger("admin: provision global git repo: " + perr.Error())
			}
		}
	}
	srv := api.NewServerWithTransports(tc.SocketPath, tc.TCPListenAddr, tlsCert, tlsFingerprint, api.ServerDeps{GitHandler: gitHandler})
	// Wrap the inner mux with deps middleware (parallel to
	// webconsole_wiring.go pattern), then rate-limit (v2.3-7c task #27),
	// then auth on top so every non-public request must carry a valid
	// bearer (v2.3-3a task #28). SetHandler applies to BOTH unix + tcp
	// legs (server.go fans it out).
	rateLimitSink := newAdminRateLimitSink(app)
	srv.SetHandler(api.AuthMiddleware(app.AdminTokenSvc)(
		api.RateLimitMiddleware(api.RateLimitDefaults, rateLimitSink)(
			api.WithDeps(deps)(srv.Handler()))))
	go func() {
		if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			logger("admin: " + err.Error())
		}
	}()
	cleanup = func() error {
		shutCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		return srv.Shutdown(shutCtx)
	}
	return info, cleanup, nil
}

// hostnameForCertSAN returns os.Hostname() with the trailing domain
// stripped (we only want the short name for SAN). Empty on error.
func hostnameForCertSAN() string {
	h, err := os.Hostname()
	if err != nil {
		return ""
	}
	return h
}

// adminTransportFromCfg derives the AdminTransportConfig from the
// server config. Empty TLS paths fall back to defaults under the
// SqlitePath directory.
func adminTransportFromCfg(cfg config.Config) AdminTransportConfig {
	certPath := cfg.Server.AdminTLSCertPath
	keyPath := cfg.Server.AdminTLSKeyPath
	fingerprintPath := ""
	if cfg.Server.AdminTCPListen != "" {
		base := defaultTLSDir(cfg.Server.SqlitePath)
		if certPath == "" {
			certPath = filepath.Join(base, "admin-tls.crt")
		}
		if keyPath == "" {
			keyPath = filepath.Join(base, "admin-tls.key")
		}
		fingerprintPath = filepath.Join(base, "admin-tls.fingerprint")
	}
	return AdminTransportConfig{
		SocketPath:      cfg.Server.AdminSocketPath,
		TCPListenAddr:   cfg.Server.AdminTCPListen,
		TLSCertPath:     certPath,
		TLSKeyPath:      keyPath,
		FingerprintPath: fingerprintPath,
		Hostname:        hostnameForCertSAN(),
	}
}

// defaultTLSDir picks the directory to hold TLS cert + key + fingerprint
// files when the operator hasn't set explicit paths. We mirror the
// SQLite DB's parent dir on the assumption that DB + TLS state share
// a single backup boundary.
func defaultTLSDir(sqlitePath string) string {
	if sqlitePath == "" {
		return "/var/lib/agent-center"
	}
	return filepath.Dir(sqlitePath)
}

// enrollBootstrapHost converts an admin_tcp_listen address (e.g.
// "0.0.0.0:7300" or "127.0.0.1:7300") into the host:port string the
// AddWorkerModal will paste into the worker install command. When
// the listener bound to 0.0.0.0 we substitute the OS hostname so the
// worker can dial in from another machine; loopback / explicit hosts
// are passed through unchanged.
func enrollBootstrapHost(adminTCPListen string) string {
	if adminTCPListen == "" {
		return ""
	}
	host, port, err := splitHostPortFlexible(adminTCPListen)
	if err != nil {
		return adminTCPListen
	}
	if host == "" || host == "0.0.0.0" || host == "::" {
		// Substitute hostname so cross-host workers can dial. Falls
		// back to 127.0.0.1 when hostname is unavailable.
		if h := hostnameForCertSAN(); h != "" {
			return h + ":" + port
		}
		return "127.0.0.1:" + port
	}
	return host + ":" + port
}

// resolveEnrollBootstrapHost picks the host:port the Web Console "Add Worker"
// command advertises (v2.7 #200). An explicit bootstrap_public_url wins — it is
// independent of the bind address, so a center that binds 0.0.0.0/loopback can
// still advertise a public DNS / LB / NAT address remote workers can dial. A
// leading "tcp://" scheme (if pasted) is stripped. Empty → derive from the bind
// address (admin_tcp_listen), the prior behavior.
func resolveEnrollBootstrapHost(bootstrapPublicURL, adminTCPListen string) string {
	if p := strings.TrimSpace(bootstrapPublicURL); p != "" {
		return strings.TrimPrefix(p, "tcp://")
	}
	return enrollBootstrapHost(adminTCPListen)
}

// splitHostPortFlexible accepts "host:port" or ":port" (bare port).
func splitHostPortFlexible(addr string) (host, port string, err error) {
	if addr != "" && addr[0] == ':' {
		return "", addr[1:], nil
	}
	host, port, err = net.SplitHostPort(addr)
	return host, port, err
}

// adminRateLimitSink bridges api.RateLimitMiddleware events to the
// observability EventSink. Emits `admin.rate_limit_hit` with token_id +
// client_ip + method + path for audit trail. v2.3-7c (task #27).
type adminRateLimitSink struct {
	sink  *observability.EventSink
	actor observability.Actor
}

func newAdminRateLimitSink(app *App) api.RateLimitSink {
	if app == nil || app.Sink == nil {
		return nil // Middleware uses noopRateLimitSink as fallback.
	}
	return &adminRateLimitSink{sink: app.Sink, actor: app.operatorActor()}
}

func (s *adminRateLimitSink) EmitRateLimitHit(id admintoken.TokenID, ip, method, path string) {
	if s == nil || s.sink == nil {
		return
	}
	_, _ = s.sink.Emit(context.Background(), observability.EmitCommand{
		EventType: "admin.rate_limit_hit",
		Actor:     s.actor,
		Payload: map[string]any{
			"token_id":  string(id),
			"client_ip": ip,
			"method":    method,
			"path":      path,
		},
	})
}

// adminDepsFromApp adapts cli.App into the admin/api HandlerDeps bag.
// All BCs whose Services are reachable from any CLI command must be
// wired here — per conventions § 0.4 ("AppService is the only entry"),
// every CLI handler will eventually call into admin via an
// admin Client (Phase B), so a missing dep here = a dead CLI path.
func adminDepsFromApp(a *App) api.HandlerDeps {
	return api.HandlerDeps{
		Actor: a.operatorActor(),

		// Raw DB for composite endpoints (v2.3-2 ADR-0014 § 2).
		DB: a.DB,

		// Conversation BC
		ConvRepo:           a.ConvRepo,
		MsgRepo:            a.MsgRepo,
		ConvRefRepo:        a.ConvRefRepo,
		ReadStateRepo:      a.ReadStateRepo,
		MessageWriter:      a.MessageWriter,
		ChannelMgmtSvc:     a.ChannelMgmtSvc,
		ParticipantMgmtSvc: a.ParticipantMgmtSvc,
		CarryOverSvc:       a.CarryOverSvc,
		ReadStateSvc:       a.ReadStateSvc,
		InboxSvc:           a.InboxSvc,
		// T341 reply-guardrail: turn-end + TrueIdle reply-nudge derivation.
		ReplyNudgeSvc: a.ReplyNudgeSvc,

		// Workforce BC
		WorkerRepo:      a.WorkerRepo,
		EnrollSvc:       a.EnrollSvc,
		WorkerConfigSvc: a.WorkerConfigSvc,

		// Environment BC (v2.7 D1, ADR-0050, task #102)
		EnvControlSvc: a.EnvControlSvc,
		// v2.7 D5 slice-1: SSE down-push bus for /admin/environment/worker/commands/stream.
		ControlStreamBus: a.ControlStreamBus,
		// I5 (issue-921db054): shared runtime-fs correlator (worker reply → waiting req).
		RuntimeFsDispatcher: a.RuntimeFsDispatcher,

		// Agent BC (v2.7 C3 / D2-b1) — per-agent MCP tool surface.
		AgentSvc: a.AgentService,
		// v2.7 D2-f s4 — worker boot-resume endpoint reads the worker's agents.
		AgentRepo:         a.AgentRepo,
		AgentActivityRepo: a.AgentActivityRepo,
		// v2.7 D2-e-ii (OQ5): outbox emitter for conversation.message_added wakes.
		OutboxRepo: a.OutboxRepo,

		// ProjectManager BC (v2.7 D2-b2) — block_task / complete_task.
		PMService: a.PMService,
		// T768: the orchestration engine service backing the 18 agent MCP graph/node/
		// edge tools (create_graph/add_node/get_ready_nodes/resolve_condition/…). nil
		// here made every one return orchestration_not_wired (501). This is the only
		// admin-api HandlerDeps builder (live server + admin_client_testhelper share
		// it), so wiring it here covers both the wiring test and the live path.
		OrchService: a.OrchService,
		// T764 hotfix: back the agent list_templates / get_template tools.
		// This is the ONLY admin-api HandlerDeps builder (used by both the live
		// server at NewServerWithTransports and the admin_client_testhelper), so
		// wiring it here keeps the wiring test and the live path in lock-step —
		// no "test green but prod 501" gap. Parallels webconsole_wiring.go's
		// TemplateRepo line; builtin rows are seeded at boot in NewApp.
		TemplateRepo: pmsql.NewTemplateRepo(a.DB),
		// issue-93dd8daa ①: org model catalog repo backing the *_model_catalog_entry tools.
		ModelCatalogRepo: pmsql.NewModelCatalogRepo(a.DB),
		// v2.18.4 BE-2 (issue-f980c8de) — workspace CodeRepo svc backing the agent
		// repo-info MCP tools (list_project_repos / get_repo_info live).
		CodeRepoSvc: a.CodeRepoService,
		// v2.19.0: the heartbeat handler writes per-agent live executor snapshots here.
		LiveState: a.LiveState,
		// Cognition reminders (T206) — create/list/get/update_reminder tools + API.
		ReminderSvc: buildReminderService(a),
		// pm (new-model) project repo for the operator/admin-token project
		// find-* read endpoints. v2.7 #131 PR-3.
		PMProjectRepo: a.PMProjectRepo,

		// identity org repo — org-name resolution for get_my_profile (v2.7.1 #239).
		IdentityOrgRepo: a.IdentityOrgRepo,

		// T460 ③: resolve a participant identity ref → display_name for the
		// post_message unresolved-mention report, so a valid HUMAN @mention is not
		// falsely flagged. Same closure shape the WakeProjector uses; nil-safe when the
		// identity repo is unwired.
		DisplayNameResolver: func(ctx context.Context, identityRef string) (string, bool) {
			if a.IdentityRepo == nil {
				return "", false
			}
			id := identityRef
			if i := strings.IndexByte(id, ':'); i >= 0 {
				id = id[i+1:] // strip the agent:/user: scheme → bare identity id
			}
			idn, err := a.IdentityRepo.GetByID(ctx, id)
			if err != nil || idn == nil {
				return "", false
			}
			return idn.DisplayName(), true
		},

		// Files module (v2.7 post-D3, task #104) — agent file MCP tools. Reuses
		// the shared buildFilesService helper (same as the webconsole FilesSvc +
		// GC loop); nil when the blobstore root is unset → file endpoints 501.
		FilesSvc: buildFilesService(a),

		// SecretManagement BC
		UserSecretRepo:       a.UserSecretRepo,
		UserSecretSvc:        a.UserSecretSvc,
		UserSecretResolveSvc: a.UserSecretResolveSvc,

		// AdminToken BC (v2.3-3a task #28)
		AdminTokenSvc: a.AdminTokenSvc,

		// Observability BC
		EventRepo: a.EventRepo,
		QuerySvc:  a.QuerySvc,
		FleetSvc:  a.FleetSvc,
		StatsSvc:  a.StatsSvc,
		LogsSvc:   a.LogsSvc,
		BlobStore: a.BlobStore,

		// Usage BC (v2.15.0 I28/F2)
		UsageEventRepo: a.UsageEventRepo,
		ModelPriceRepo: a.ModelPriceRepo,

		// Team BC (Team Phase-1 wiring, design §4/§6/§7/§9). This is the ONLY
		// admin-api HandlerDeps builder (live server + admin_client_testhelper
		// share it), so wiring here lands both the wiring test and the live path —
		// no "test green but prod 501" gap. The team service is built on the SAME
		// *sql.DB the migrations (0107_v229_teams) run against; git provisioning is
		// wired only in server mode (a.DB != nil, sqlite path set).
		// The MemberResolver hardens add-member: AddMember rejects a ref that does
		// not resolve to a real, matching-kind, same-org identity (dangling/cross-
		// org/kind-mismatch), stopping team_members pollution from ANY client (web
		// facade OR MCP add_member — both share AddMember). Nil-safe: an unwired
		// identity repo degrades to the pre-hardening pass-through.
		TeamSvc: teamservice.New(teamsql.NewRepo(a.DB), a.DB, a.IDGen, a.Clock).
			WithMemberResolver(newIdentityMemberResolver(a.IdentityRepo, a.IdentityMemberRepo)),
		TeamIDGen:   a.IDGen,
		TeamGitHost: buildTeamGitHost(a),
		// instantiate_team builds REAL agent identities (design §6/§8): reuse the
		// identity-provision path so the identities table gets real rows (not a
		// dangling ref). TeamMemberRepo resolves the owner/admin provisioner.
		TeamIdentityProvisionSvc: a.IdentityAgentProvisionSvc,
		TeamMemberRepo:           a.IdentityMemberRepo,
	}
}

// buildTeamGitHost constructs the center-hosted git provisioning surface (design
// §4.2/§4.3). The bare-repo tree lives alongside the SQLite DB (shared backup
// boundary). Returns nil when there is no on-disk DB (test / client mode) so the
// team tools degrade to "team created, memory_seeded=false" rather than erroring.
func buildTeamGitHost(a *App) *centergit.Host {
	sqlitePath := a.Config.Server.SqlitePath
	if a.DB == nil || sqlitePath == "" {
		return nil
	}
	root := filepath.Join(filepath.Dir(sqlitePath), "team-git")
	return centergit.NewHost(root, nil)
}

// The centergit TeamMembership adapter (runtime agent-id → team, bridging the
// identity-member ref namespace) lives in the admin/api package alongside the git
// resolver it must agree with — see api.NewTeamMembership.
