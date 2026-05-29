package cli

import (
	"context"
	"errors"
	"net/http"
	"time"

	agentsql "github.com/oopslink/agent-center/internal/agent/sqlite"
	"github.com/oopslink/agent-center/internal/blobstore"
	"github.com/oopslink/agent-center/internal/environment"
	envservice "github.com/oopslink/agent-center/internal/environment/service"
	envsql "github.com/oopslink/agent-center/internal/environment/sqlite"
	"github.com/oopslink/agent-center/internal/files"
	filesservice "github.com/oopslink/agent-center/internal/files/service"
	filessql "github.com/oopslink/agent-center/internal/files/sqlite"
	"github.com/oopslink/agent-center/internal/observability"
	"github.com/oopslink/agent-center/internal/outbox"
	outboxsql "github.com/oopslink/agent-center/internal/outbox/sqlite"
	pmservice "github.com/oopslink/agent-center/internal/projectmanager/service"
	"github.com/oopslink/agent-center/internal/webconsole/api"
	"github.com/oopslink/agent-center/internal/webconsole/spa"
	"github.com/oopslink/agent-center/internal/webconsole/sse"
)

// buildFilesService constructs the files transfer Service from the App's DB +
// the configured blobstore root (mirrors the GC-loop construction in
// runWebConsole). Returns nil when the blobstore root is unset or the local
// blobstore fails to initialize — callers leave FilesSvc nil in that case so
// the /api/files surface degrades to 501 rather than panicking.
func buildFilesService(a *App) *filesservice.Service {
	if a == nil {
		return nil
	}
	blobRoot := a.Config.BlobStore.Root
	if blobRoot == "" {
		return nil
	}
	store, err := blobstore.NewLocalDir(blobRoot)
	if err != nil {
		return nil
	}
	return filesservice.New(filesservice.Deps{
		DB:         a.DB,
		Sessions:   filessql.NewFileTransferSessionRepo(a.DB),
		References: filessql.NewFileReferenceRepo(a.DB),
		Resolver:   files.NewLocalResolver(""),
		BlobStore:  store,
		IDGen:      a.IDGen,
		Clock:      a.Clock,
	}).SetGCRepo(filessql.NewBlobMetadataRepo(a.DB))
}

// buildWebConsoleHandler stitches the App's wired services into the
// HandlerDeps the api package expects + installs WithDeps middleware.
// Returns nil http.Handler when Web Console is disabled.
func buildWebConsoleHandler(a *App, bus *sse.Bus) http.Handler {
	if a == nil {
		return nil
	}
	deps := api.HandlerDeps{
		Actor:               a.DefaultActor(),
		ConvRepo:            a.ConvRepo,
		MsgRepo:             a.MsgRepo,
		MessageWriter:       a.MessageWriter,
		ChannelMgmtSvc:      a.ChannelMgmtSvc,
		ParticipantMgmtSvc:  a.ParticipantMgmtSvc,
		CarryOverSvc:        a.CarryOverSvc,
		IRRepo:              a.IRRepo,
		ExecRepo:            a.ExecRepo,
		IRSvc:               a.IRSvc,
		AgentInstanceRepo:   a.AgentInstanceRepo,
		UserSecretRepo:      a.UserSecretRepo,
		UserSecretSvc:       a.UserSecretSvc,
		ProjectRepo:         a.ProjectRepo,
		PM:                  a.PMService,
		AgentSvc:            a.AgentService,
		FilesSvc:            buildFilesService(a),
		ReadStateRepo:       a.ReadStateRepo,
		ReadStateSvc:        a.ReadStateSvc,
		IssueRepo:           a.IssueRepo,
		TaskRepo:            a.TaskRepo,
		AdminTokenSvc:       a.AdminTokenSvc,
		SignupSvc:           a.IdentitySignupSvc,
		SigninSvc:           a.IdentitySigninSvc,
		SignoutSvc:          a.IdentitySignoutSvc,
		AuthSvc:             a.IdentityAuthSvc,
		PasscodeChangeSvc:   a.IdentityPasscodeChangeSvc,
		OrgRepo:             a.IdentityOrgRepo,
		OrgCreateSvc:        a.IdentityOrgCreateSvc,
		OrgLifecycleSvc:     a.IdentityOrgLifecycleSvc,
		MemberRepo:          a.IdentityMemberRepo,
		MemberAddSvc:        a.IdentityMemberAddSvc,
		MemberCreateUserSvc: a.IdentityMemberCreateUserSvc,
		MemberRoleChangeSvc: a.IdentityMemberRoleChangeSvc,
		MemberDisableSvc:    a.IdentityMemberDisableSvc,
		AgentProvisionSvc:   a.IdentityAgentProvisionSvc,
		OrgUpdateSvc:        a.IdentityOrgUpdateSvc,
	}
	srv := api.NewServer(":0", api.Deps{SSE: bus, SPA: spa.Handler()})
	return api.WithDeps(deps)(srv.Handler())
}

// WebConsoleEnrollWiring carries the values the AddWorkerModal needs
// to render a working install command for the worker box. Both are
// known by ServerCommand after the admin TCP listener boots: the
// fingerprint comes from AdminTransportInfo, the bootstrap host is
// derived from the admin_tcp_listen config + the operator-facing
// hostname (or 127.0.0.1 when the listener is loopback-only).
type WebConsoleEnrollWiring struct {
	BootstrapHost string // e.g. "192.168.1.10:7300" or "127.0.0.1:7300"
	Fingerprint   string // SSH-style sha256:HH:HH:...
}

// runWebConsole binds + serves the Web Console HTTP API at addr,
// enforcing the loopback bind guard (per ADR-0037 / NF2 — no remote
// listen). Returns http.ErrServerClosed on graceful shutdown.
func runWebConsole(ctx context.Context, a *App, bus *sse.Bus, addr string, enroll WebConsoleEnrollWiring, logger func(string)) (cleanup func() error, err error) {
	if addr == "" {
		addr = "127.0.0.1:7100"
	}
	if a == nil {
		return func() error { return nil }, errors.New("webconsole: app nil")
	}
	// v2.6 production guarantee: webconsole requires Identity BC auth.
	// AuthSvc is wired only when secret_management.master_key_file is set
	// (master key doubles as the JWT HS256 signing key per ADR-0043 §6).
	// Refuse to start the webconsole when auth is unconfigured rather than
	// allowing the per-request middleware to fail-open.
	if a.IdentityAuthSvc == nil {
		return func() error { return nil }, errors.New(
			"webconsole: auth not configured — set secret_management.master_key_file " +
				"in the server config (the master key doubles as the JWT signing key)")
	}
	// v2.7 D3-d/D3-c: a single files transfer Service instance backs BOTH the
	// /api/files HTTP surface (FilesSvc) and the refcount GC loop below — built
	// once from the configured blobstore root (nil when unconfigured).
	filesSvc := buildFilesService(a)
	deps := api.HandlerDeps{
		Actor:               a.DefaultActor(),
		ConvRepo:            a.ConvRepo,
		MsgRepo:             a.MsgRepo,
		MessageWriter:       a.MessageWriter,
		ChannelMgmtSvc:      a.ChannelMgmtSvc,
		ParticipantMgmtSvc:  a.ParticipantMgmtSvc,
		CarryOverSvc:        a.CarryOverSvc,
		IRRepo:              a.IRRepo,
		ExecRepo:            a.ExecRepo,
		IRSvc:               a.IRSvc,
		AgentInstanceRepo:   a.AgentInstanceRepo,
		UserSecretRepo:      a.UserSecretRepo,
		UserSecretSvc:       a.UserSecretSvc,
		ProjectRepo:         a.ProjectRepo,
		PM:                  a.PMService,
		AgentSvc:            a.AgentService,
		FilesSvc:            filesSvc,
		QuerySvc:            a.QuerySvc,
		FleetSvc:            a.FleetSvc,
		ReadStateRepo:       a.ReadStateRepo,
		ReadStateSvc:        a.ReadStateSvc,
		IssueRepo:           a.IssueRepo,
		TaskRepo:            a.TaskRepo,
		AdminTokenSvc:       a.AdminTokenSvc,
		EnrollBootstrapHost: enroll.BootstrapHost,
		EnrollFingerprint:   enroll.Fingerprint,
		WorkerRenameSvc:     a.EnrollSvc,
		WorkerAddSvc:        a.EnrollSvc,
		WorkerRemoveSvc:     a.EnrollSvc,
		WorkerRepo:          a.WorkerRepo,
		SignupSvc:           a.IdentitySignupSvc,
		SigninSvc:           a.IdentitySigninSvc,
		SignoutSvc:          a.IdentitySignoutSvc,
		AuthSvc:             a.IdentityAuthSvc,
		PasscodeChangeSvc:   a.IdentityPasscodeChangeSvc,
		OrgRepo:             a.IdentityOrgRepo,
		OrgCreateSvc:        a.IdentityOrgCreateSvc,
		OrgLifecycleSvc:     a.IdentityOrgLifecycleSvc,
		MemberRepo:          a.IdentityMemberRepo,
		MemberAddSvc:        a.IdentityMemberAddSvc,
		MemberCreateUserSvc: a.IdentityMemberCreateUserSvc,
		MemberRoleChangeSvc: a.IdentityMemberRoleChangeSvc,
		MemberDisableSvc:    a.IdentityMemberDisableSvc,
		AgentProvisionSvc:   a.IdentityAgentProvisionSvc,
		OrgUpdateSvc:        a.IdentityOrgUpdateSvc,
	}
	srv := api.NewServer(addr, api.Deps{SSE: bus, SPA: spa.Handler(), Version: ResolvedBuildVersion()})
	// Wrap the inner mux with deps middleware; install it as the
	// server's handler so the loopback guard in api.Server.ListenAndServe
	// still applies.
	wrapped := api.WithDeps(deps)(srv.Handler())
	srv.SetHandler(wrapped)
	go func() {
		if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			logger("webconsole: " + err.Error())
		}
	}()
	// Start the EventSink → SSE Bus fan-out tailer. It polls the
	// events table on a 250ms ticker and publishes each new event
	// onto the bus, where subscribed users receive it.
	fanoutCtx, fanoutCancel := context.WithCancel(ctx)
	fanout := sse.NewEventFanout(a.EventRepo, bus, 0).WithErrorHandler(func(err error) {
		logger("webconsole fanout: " + err.Error())
	})
	go fanout.Run(fanoutCtx)

	// v2.7 B3: bring the cross-BC outbox online. A single-goroutine Pump
	// drains the outbox (backlog on boot, then ~1s ticker) and applies the
	// ProjectManager→Conversation participant projection + the AssignTask→
	// AgentWorkItem projection. Without this loop the projectors are static
	// and no cross-BC effect ever happens (plan §10 OQ1). Mirrors the fanout
	// lifecycle (ctx-cancel + graceful shutdown).
	outboxRepo := outboxsql.NewOutboxRepo(a.DB)
	appliedRepo := outboxsql.NewAppliedRepo(a.DB)
	participantProj := pmservice.NewParticipantProjector(a.DB, a.ConvRepo, appliedRepo, a.IDGen, a.Clock)
	workItemProj := pmservice.NewWorkItemProjector(a.DB, agentsql.NewWorkItemRepo(a.DB), appliedRepo, a.IDGen, a.Clock)
	// v2.7 D2-a: ADDITIVE reconcile projector. Agent lifecycle intent changes
	// (C3 agent.lifecycle_changed) become declarative agent.reconcile commands on
	// the agent's Worker control stream (D1). D1's NoopHandler no-op-acks them →
	// zero real effect yet (no execution cutover; old taskruntime path untouched).
	controlLog := environment.NewControlLog(envsql.NewControlEventRepo(a.DB), a.IDGen, a.Clock)
	agentControlProj := envservice.NewAgentControlProjector(a.DB, controlLog, appliedRepo, a.Clock)
	relay := outbox.NewRelay(outboxRepo, appliedRepo, a.Clock, participantProj, workItemProj, agentControlProj)
	pump := outbox.NewPump(relay, time.Second, 0).WithErrorHandler(func(err error) {
		logger("webconsole outbox pump: " + err.Error())
	})
	pumpCtx, pumpCancel := context.WithCancel(ctx)
	go pump.Run(pumpCtx)

	// v2.7 D3-c: bring the file-blob refcount GC online. A single-goroutine
	// loop (initial pass on boot, then ~1h ticker) expires stale upload sessions
	// and reaps blobs whose live-reference count has been zero past the grace
	// period (default 7d, ADR-0048 §5). Mirrors the Pump/fanout lifecycle
	// (ctx-cancel + graceful shutdown); ADDITIVE — it does not touch the
	// existing pump/fanout. The files transfer Service was constructed above
	// (shared with the /api/files HTTP surface); the resolver yields
	// blobstore-relative paths so the blobstore owns the physical root.
	gcCtx, gcCancel := context.WithCancel(ctx)
	if filesSvc != nil {
		gcLoop := filesservice.NewGCLoop(filesSvc, filesservice.DefaultGCGrace, time.Hour).
			WithErrorHandler(func(err error) {
				logger("webconsole files gc: " + err.Error())
			})
		go gcLoop.Run(gcCtx)
	}

	cleanup = func() error {
		fanoutCancel()
		pumpCancel()
		gcCancel()
		shutCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = bus.Shutdown(shutCtx)
		return srv.Shutdown(shutCtx)
	}
	return cleanup, nil
}

// _ keeps observability import alive (handler deps include Actor).
var _ = observability.Actor("")
