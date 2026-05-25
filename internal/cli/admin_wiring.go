package cli

import (
	"context"
	"errors"
	"net/http"
	"time"

	"github.com/oopslink/agent-center/internal/admin/api"
)

// runAdminEndpoint starts the v2.2 admin unix-socket server. v2.2-A1
// scaffolded the listener + health endpoint; v2.2-A2 mounts the full
// CLI AppService surface via api.HandlerDeps populated from the App.
//
// Returns a cleanup function that shuts the server down + removes the
// socket file. cleanup is non-nil even on error so the caller can
// always defer it safely.
func runAdminEndpoint(ctx context.Context, app *App, socketPath string, logger func(string)) (cleanup func() error, err error) {
	if socketPath == "" {
		return func() error { return nil }, errors.New("admin: socket_path required")
	}
	if app == nil {
		return func() error { return nil }, errors.New("admin: app nil")
	}
	deps := adminDepsFromApp(app)
	srv := api.NewServerWithDeps(socketPath, api.ServerDeps{
		Queue: app.DispatchQueue,
	})
	// Wrap the inner mux with deps middleware (parallel to
	// webconsole_wiring.go pattern), then layer auth on top so every
	// non-public request must carry a valid bearer (v2.3-3a task #28).
	srv.SetHandler(api.AuthMiddleware(app.AdminTokenSvc)(
		api.WithDeps(deps)(srv.Handler())))
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
	return cleanup, nil
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
		UserSecretRepo: a.UserSecretRepo,
		UserSecretSvc:  a.UserSecretSvc,

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
