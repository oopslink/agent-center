package cli

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"time"

	"github.com/oopslink/agent-center/internal/bridge/feishu/client"
	"github.com/oopslink/agent-center/internal/bridge/feishu/inbound"
	"github.com/oopslink/agent-center/internal/conversation/identity"
	"github.com/oopslink/agent-center/internal/observability"
	"github.com/oopslink/agent-center/internal/observability/escalator"
)

// ServerSubsystems wires up everything the `server` mode runs in
// addition to the bare daemon idle loop. Phase 7 introduces:
//
//   - FeishuBridge inbound (if bridge.feishu.enabled): WS reader →
//     Router → Conversation / Task / IR application services.
//   - UnknownEventEscalator: periodic scan for noisy unknown CLI
//     event-types (was Phase 4 stubbed; this is its first runtime
//     wire-in per plan-7 § 7.3).
//
// All subsystems are cancellable via the supplied ctx. ServerSubsystems
// is the production wiring; tests reuse the same primitives via the
// e2e harness (tests/e2e/harness).
type ServerSubsystems struct {
	app          *App
	inboundRouter *inbound.Router
	dedupe       *inbound.Dedupe
	feishuClient client.Client
	escalator    *escalator.Service
	logger       func(string)
}

// NewServerSubsystems constructs the inbound + escalator subsystems
// for the given app. Logger is the diagnostic sink for non-emit errors
// (typically writes to stderr).
//
// Returns nil ServerSubsystems with nil error when the bridge is
// disabled — caller can still run the escalator etc.
func NewServerSubsystems(a *App, logger func(string)) (*ServerSubsystems, error) {
	if a == nil {
		return nil, errors.New("server subsystems: nil app")
	}
	if logger == nil {
		logger = func(string) {}
	}
	ss := &ServerSubsystems{app: a, logger: logger}

	// UnknownEventEscalator is always-on (it's a Phase 4 deliverable
	// that finally gets its long-running wire-up here).
	ss.escalator = escalator.NewService(a.EventRepo, a.Sink, a.Clock, escalator.Config{
		Interval:  1 * time.Hour,
		Threshold: escalator.DefaultThreshold,
		Window:    escalator.DefaultWindow,
	})

	// Bridge inbound is opt-in (and requires identity infra to be
	// populated; we still wire the Router but skip the WS connect
	// when disabled).
	if !a.Config.Bridge.Feishu.Enabled {
		return ss, nil
	}

	dedupe := inbound.NewDedupe(0, 0, a.Clock)
	resolver, err := inbound.NewIdentityResolver(inbound.IdentityResolverDeps{
		Bindings:     a.ChannelBindingRepo,
		Identities:   a.IdentityRepo,
		Registration: a.IdentityRegistration,
		Sink:         a.Sink,
		Clock:        a.Clock,
		Channel:      identity.Channel("feishu"),
		Actor:        observability.Actor("system"),
	})
	if err != nil {
		return nil, fmt.Errorf("server subsystems: resolver: %w", err)
	}
	parser := inbound.NewSlashCommandParser()
	slash, err := inbound.NewSlashRouter(inbound.SlashRouterDeps{
		DB:        a.DB,
		Clock:     a.Clock,
		IDGen:     a.IDGen,
		Sink:      a.Sink,
		Tasks:     a.TaskRepo,
		Execs:     a.ExecRepo,
		Convs:     a.ConvRepo,
		TaskSvc:   a.TaskSvc,
		IRSvc:     a.IRSvc,
		IRRepo:    a.IRRepo,
		MsgWriter: a.MessageWriter,
		Actor:     observability.Actor("system"),
	})
	if err != nil {
		return nil, fmt.Errorf("server subsystems: slash: %w", err)
	}
	card, err := inbound.NewCardCallback(inbound.CardCallbackDeps{
		Clock:     a.Clock,
		Sink:      a.Sink,
		IRRepo:    a.IRRepo,
		IRSvc:     a.IRSvc,
		Execs:     a.ExecRepo,
		Tasks:     a.TaskRepo,
		MsgWriter: a.MessageWriter,
		Actor:     observability.Actor("system"),
	})
	if err != nil {
		return nil, fmt.Errorf("server subsystems: card: %w", err)
	}
	router, err := inbound.NewRouter(inbound.RouterDeps{
		Clock:     a.Clock,
		IDGen:     a.IDGen,
		Sink:      a.Sink,
		Dedupe:    dedupe,
		Resolver:  resolver,
		Parser:    parser,
		Slash:     slash,
		Card:      card,
		DB:        a.DB,
		Convs:     a.ConvRepo,
		MsgWriter: a.MessageWriter,
		Actor:     observability.Actor("system"),
	})
	if err != nil {
		return nil, fmt.Errorf("server subsystems: router: %w", err)
	}
	ss.inboundRouter = router
	ss.dedupe = dedupe
	return ss, nil
}

// AttachFeishuClient wires the client's OnEvent handler to the inbound
// router. Tests use this with a fake client; production wires the
// OAPIAdapter.
func (s *ServerSubsystems) AttachFeishuClient(c client.Client) {
	if s == nil || s.inboundRouter == nil || c == nil {
		return
	}
	s.feishuClient = c
	c.OnEvent(func(ev client.VendorEvent) {
		// Translate vendor adapter envelope into the inbound VO.
		// In Phase 7 the SDK leaf is the SOLE place that fills
		// VendorEvent (client/oapi_adapter.go). The router accepts
		// the typed VO directly when callers use TranslateClientEvent.
		ie := TranslateClientEvent(ev)
		if _, err := s.inboundRouter.OnVendorEvent(context.Background(), ie); err != nil {
			s.logger(fmt.Sprintf("inbound router: %v", err))
		}
	})
}

// TranslateClientEvent maps the bridge/client.VendorEvent envelope to
// the inbound VO. The two structs are intentionally distinct: the
// client envelope is what crosses the SDK boundary; the inbound VO is
// the domain-side view the Router operates on.
//
// This translation lives outside `client/` to keep that package's
// vendor SDK boundary tight (it only deals with raw JSON / SDK
// structs, not domain concepts).
func TranslateClientEvent(ev client.VendorEvent) inbound.VendorEvent {
	// The v1 path: client.VendorEvent.RawJSON is already pre-parsed
	// into the matching fields by the adapter. Tests + production
	// callers using InjectEvent supply ready-made `inbound.VendorEvent`
	// values; this stub provides a minimal translation surface so the
	// public `Client.OnEvent` shape still compiles + is exercised in
	// integration tests.
	return inbound.VendorEvent{
		Kind:    inbound.VendorEventKind(ev.Kind),
		Text:    ev.RawJSON,
	}
}

// RouteInbound is the test/server-side direct entry that bypasses the
// thin SDK translator. The e2e harness calls this with a fully-shaped
// VendorEvent so it can exercise the Router without going through the
// client.VendorEvent intermediate.
func (s *ServerSubsystems) RouteInbound(ctx context.Context, ev inbound.VendorEvent) (inbound.RouteDecision, error) {
	if s == nil || s.inboundRouter == nil {
		return inbound.RouteDecision{}, errors.New("server subsystems: inbound router not wired")
	}
	return s.inboundRouter.OnVendorEvent(ctx, ev)
}

// Run starts the always-on background goroutines (currently just the
// escalator). It blocks until ctx is cancelled.
func (s *ServerSubsystems) Run(ctx context.Context) {
	if s == nil || s.escalator == nil {
		<-ctx.Done()
		return
	}
	s.escalator.Run(ctx, func(err error) {
		s.logger(fmt.Sprintf("escalator: %v", err))
	})
}

// ConnectBridge optionally connects the adapter (skipped when bridge
// disabled). The caller is expected to wire AttachFeishuClient first.
func (s *ServerSubsystems) ConnectBridge(ctx context.Context) error {
	if s == nil || s.feishuClient == nil {
		return nil
	}
	if err := s.feishuClient.Connect(ctx); err != nil {
		s.logger(fmt.Sprintf("feishu connect failed: %v", err))
		return err
	}
	return nil
}

// CloseBridge tears down the adapter.
func (s *ServerSubsystems) CloseBridge() error {
	if s == nil || s.feishuClient == nil {
		return nil
	}
	return s.feishuClient.Close()
}

// EscalatorScan is exposed for tests that want to drive the scan
// deterministically.
func (s *ServerSubsystems) EscalatorScan(ctx context.Context) (escalator.ScanResult, error) {
	if s == nil || s.escalator == nil {
		return escalator.ScanResult{}, errors.New("server subsystems: escalator not wired")
	}
	return s.escalator.Scan(ctx)
}

// InboundRouter exposes the router for direct test injection. Returns
// nil when bridge inbound is disabled.
func (s *ServerSubsystems) InboundRouter() *inbound.Router {
	if s == nil {
		return nil
	}
	return s.inboundRouter
}

// NewFeishuAdapterFromConfig is a small helper that constructs the
// adapter from the app's loaded config. Returns nil when the bridge is
// disabled. Production callers use this; tests substitute their own
// fake client via AttachFeishuClient.
func NewFeishuAdapterFromConfig(a *App) client.Client {
	if a == nil || !a.Config.Bridge.Feishu.Enabled {
		return nil
	}
	return client.NewOAPIAdapter(client.AdapterConfig{
		BaseURL:    a.Config.Bridge.Feishu.BaseURL,
		AppID:      a.Config.Bridge.Feishu.AppID,
		AppSecret:  a.Config.Bridge.Feishu.AppSecret,
		HTTPClient: &http.Client{Timeout: 30 * time.Second},
		Clock:      a.Clock,
	})
}

// suppress unused-import lint when build hits these via test injection.
var _ = io.EOF
