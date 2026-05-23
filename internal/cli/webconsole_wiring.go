package cli

import (
	"context"
	"errors"
	"net/http"
	"time"

	"github.com/oopslink/agent-center/internal/observability"
	"github.com/oopslink/agent-center/internal/webconsole/api"
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
	}
	srv := api.NewServer(":0", api.Deps{SSE: bus})
	return api.WithDeps(deps)(srv.Handler())
}

// runWebConsole binds + serves the Web Console HTTP API at addr,
// enforcing the loopback bind guard (per ADR-0037 / NF2 — no remote
// listen). Returns http.ErrServerClosed on graceful shutdown.
func runWebConsole(ctx context.Context, a *App, bus *sse.Bus, addr string, logger func(string)) (cleanup func() error, err error) {
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
	}
	srv := api.NewServer(addr, api.Deps{SSE: bus})
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
