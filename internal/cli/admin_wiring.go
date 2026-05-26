package cli

import (
	"context"
	"crypto/tls"
	"errors"
	"net/http"
	"os"
	"path/filepath"
	"time"

	"github.com/oopslink/agent-center/internal/admin/api"
	"github.com/oopslink/agent-center/internal/admintoken"
	"github.com/oopslink/agent-center/internal/config"
	"github.com/oopslink/agent-center/internal/observability"
)

// AdminTransportConfig captures the v2.3-7a (task #27) admin listener
// configuration: optional unix socket + optional TCP+TLS address with
// auto-managed cert + fingerprint files. At least one of SocketPath or
// TCPListenAddr must be non-empty.
type AdminTransportConfig struct {
	SocketPath       string
	TCPListenAddr    string
	TLSCertPath      string
	TLSKeyPath       string
	FingerprintPath  string
	Hostname         string
}

// AdminTransportInfo is what runAdminEndpoint returns to the caller
// (boot banner code in handlers_system.go uses this to print the cert
// fingerprint, expiry, etc.).
type AdminTransportInfo struct {
	TLSFingerprint     string
	TLSCertNotAfter    time.Time
	TLSCertGenerated   bool
	TLSExpiryWarn      bool
	TLSExpiryDays      int
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
	srv := api.NewServerWithTransports(tc.SocketPath, tc.TCPListenAddr, tlsCert, tlsFingerprint, api.ServerDeps{
		Queue: app.DispatchQueue,
	})
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
	return &adminRateLimitSink{sink: app.Sink, actor: app.DefaultActor()}
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
		Actor: a.DefaultActor(),

		// Raw DB for composite endpoints (v2.3-2 ADR-0014 § 2).
		DB: a.DB,

		// Conversation BC
		ConvRepo:            a.ConvRepo,
		MsgRepo:             a.MsgRepo,
		ConvRefRepo:         a.ConvRefRepo,
		ReadStateRepo:       a.ReadStateRepo,
		MessageWriter:       a.MessageWriter,
		ChannelMgmtSvc:      a.ChannelMgmtSvc,
		ParticipantMgmtSvc:  a.ParticipantMgmtSvc,
		CarryOverSvc:        a.CarryOverSvc,
		DerivationSvc:       a.DerivationSvc,
		ReadStateSvc:        a.ReadStateSvc,

		// Workforce BC
		WorkerRepo:        a.WorkerRepo,
		MappingRepo:       a.MappingRepo,
		ProposalRepo:      a.ProposalRepo,
		ProjectRepo:       a.ProjectRepo,
		AgentInstanceRepo: a.AgentInstanceRepo,
		EnrollSvc:         a.EnrollSvc,
		DiscoverySvc:      a.DiscoverySvc,
		AcceptanceSvc:     a.AcceptanceSvc,
		ProjectSvc:        a.ProjectSvc,
		AgentMgmtSvc:      a.AgentMgmtSvc,

		// TaskRuntime BC
		TaskRepo:        a.TaskRepo,
		ExecRepo:        a.ExecRepo,
		IRRepo:          a.IRRepo,
		ArtifactRepo:    a.ArtifactRepo,
		TaskSvc:         a.TaskSvc,
		IRSvc:           a.IRSvc,
		ArtifactSvc:     a.ArtifactSvc,
		ExecSvc:         a.ExecSvc,
		DispatchSvc:     a.DispatchSvc,
		KillCoordinator: a.KillCoordinator,

		// SecretManagement BC
		UserSecretRepo:       a.UserSecretRepo,
		UserSecretSvc:        a.UserSecretSvc,
		UserSecretResolveSvc: a.UserSecretResolveSvc,

		// AdminToken BC (v2.3-3a task #28)
		AdminTokenSvc: a.AdminTokenSvc,

		// Identity (Conversation subdomain)
		IdentityRepo:         a.IdentityRepo,
		IdentityRegistration: a.IdentityRegistration,

		// Discussion BC
		IssueRepo:                a.IssueRepo,
		IssueLifecycleSvc:        a.IssueLifecycleSvc,
		IssueCommentSvc:          a.IssueCommentSvc,
		IssueBindConversationSvc: a.IssueBindConversationSvc,
		IssueLinkConversationSvc: a.IssueLinkConversationSvc,

		// Cognition BC
		InvocationRepo:    a.InvocationRepo,
		DecisionRepo:      a.DecisionRepo,
		DecisionRecorder:  a.DecisionRecorder,
		SupervisorSpawner: a.SupervisorSpawner,

		// Observability BC
		EventRepo: a.EventRepo,
		QuerySvc:  a.QuerySvc,
		FleetSvc:  a.FleetSvc,
		StatsSvc:  a.StatsSvc,
		LogsSvc:   a.LogsSvc,
		BlobStore: a.BlobStore,
	}
}
