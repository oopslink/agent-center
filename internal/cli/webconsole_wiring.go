package cli

import (
	"context"
	"errors"
	"net/http"
	"time"

	"github.com/oopslink/agent-center/internal/observability"
	"github.com/oopslink/agent-center/internal/webconsole/api"
	"github.com/oopslink/agent-center/internal/webconsole/spa"
	"github.com/oopslink/agent-center/internal/webconsole/sse"
)

// buildWebConsoleHandler stitches the App's wired services into the
// HandlerDeps the api package expects + installs WithDeps middleware.
// Returns nil http.Handler when Web Console is disabled.
func buildWebConsoleHandler(a *App, bus *sse.Bus) http.Handler {
	if a == nil {
		return nil
	}
	deps := api.HandlerDeps{
		Actor:              a.DefaultActor(),
		ConvRepo:           a.ConvRepo,
		MsgRepo:            a.MsgRepo,
		MessageWriter:      a.MessageWriter,
		ChannelMgmtSvc:     a.ChannelMgmtSvc,
		ParticipantMgmtSvc: a.ParticipantMgmtSvc,
		CarryOverSvc:       a.CarryOverSvc,
		DerivationSvc:      a.DerivationSvc,
		IRRepo:             a.IRRepo,
		IRSvc:              a.IRSvc,
		AgentInstanceRepo:  a.AgentInstanceRepo,
		UserSecretRepo:     a.UserSecretRepo,
		UserSecretSvc:      a.UserSecretSvc,
		ProjectRepo:        a.ProjectRepo,
		ReadStateRepo:      a.ReadStateRepo,
		ReadStateSvc:       a.ReadStateSvc,
		IssueRepo:          a.IssueRepo,
		TaskRepo:           a.TaskRepo,
		IssueLifecycleSvc:  a.IssueLifecycleSvc,
		TaskSvc:            a.TaskSvc,
		AdminTokenSvc:      a.AdminTokenSvc,
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
	deps := api.HandlerDeps{
		Actor:              a.DefaultActor(),
		ConvRepo:           a.ConvRepo,
		MsgRepo:            a.MsgRepo,
		MessageWriter:      a.MessageWriter,
		ChannelMgmtSvc:     a.ChannelMgmtSvc,
		ParticipantMgmtSvc: a.ParticipantMgmtSvc,
		CarryOverSvc:       a.CarryOverSvc,
		DerivationSvc:      a.DerivationSvc,
		IRRepo:             a.IRRepo,
		IRSvc:              a.IRSvc,
		AgentInstanceRepo:  a.AgentInstanceRepo,
		UserSecretRepo:     a.UserSecretRepo,
		UserSecretSvc:      a.UserSecretSvc,
		ProjectRepo:        a.ProjectRepo,
		QuerySvc:           a.QuerySvc,
		FleetSvc:           a.FleetSvc,
		ReadStateRepo:      a.ReadStateRepo,
		ReadStateSvc:       a.ReadStateSvc,
		IssueRepo:          a.IssueRepo,
		TaskRepo:           a.TaskRepo,
		IssueLifecycleSvc:  a.IssueLifecycleSvc,
		TaskSvc:            a.TaskSvc,
		AdminTokenSvc:      a.AdminTokenSvc,
		EnrollBootstrapHost: enroll.BootstrapHost,
		EnrollFingerprint:   enroll.Fingerprint,
		WorkerRenameSvc:    a.EnrollSvc,
		WorkerAddSvc:       a.EnrollSvc,
		WorkerRemoveSvc:    a.EnrollSvc,
		WorkerRepo:         a.WorkerRepo,
		ProjectCRUDSvc:     a.ProjectSvc,
		MappingRepo:        a.MappingRepo,
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
	cleanup = func() error {
		fanoutCancel()
		shutCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = bus.Shutdown(shutCtx)
		return srv.Shutdown(shutCtx)
	}
	return cleanup, nil
}

// _ keeps observability import alive (handler deps include Actor).
var _ = observability.Actor("")
