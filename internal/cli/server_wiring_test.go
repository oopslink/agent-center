package cli

import (
	"context"
	"errors"
	"testing"

	"github.com/oopslink/agent-center/internal/bridge/feishu/client"
	"github.com/oopslink/agent-center/internal/bridge/feishu/inbound"
)

func TestNewServerSubsystems_NilApp(t *testing.T) {
	_, err := NewServerSubsystems(nil, nil)
	if err == nil {
		t.Fatal("want error on nil app")
	}
}

func TestNewServerSubsystems_FeishuDisabled(t *testing.T) {
	app := newTestAppWithFileDB(t)
	app.Config.Bridge.Feishu.Enabled = false
	ss, err := NewServerSubsystems(app, nil)
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if ss == nil {
		t.Fatal("subsystems nil")
	}
	if ss.InboundRouter() != nil {
		t.Error("inbound router should be nil when bridge disabled")
	}
	// Escalator scan must still work.
	if _, err := ss.EscalatorScan(context.Background()); err != nil {
		t.Errorf("escalator scan: %v", err)
	}
}

func TestNewServerSubsystems_FeishuEnabled(t *testing.T) {
	app := newTestAppWithFileDB(t)
	app.Config.Bridge.Feishu.Enabled = true
	app.Config.Bridge.Feishu.AppID = "x"
	app.Config.Bridge.Feishu.AppSecret = "y"
	ss, err := NewServerSubsystems(app, nil)
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if ss.InboundRouter() == nil {
		t.Fatal("router should be wired")
	}
}

func TestRouteInbound_NotWired(t *testing.T) {
	app := newTestAppWithFileDB(t)
	ss, err := NewServerSubsystems(app, nil)
	if err != nil {
		t.Fatal(err)
	}
	_, err = ss.RouteInbound(context.Background(), inbound.VendorEvent{})
	if err == nil {
		t.Fatal("want error when router not wired")
	}
}

func TestRouteInbound_Wired(t *testing.T) {
	app := newTestAppWithFileDB(t)
	app.Config.Bridge.Feishu.Enabled = true
	ss, err := NewServerSubsystems(app, nil)
	if err != nil {
		t.Fatal(err)
	}
	// Driven with a malformed event so we exercise the dispatch path
	// without requiring a seeded user identity.
	dec, err := ss.RouteInbound(context.Background(), inbound.VendorEvent{
		Kind: inbound.VendorEventMessageReceive,
	})
	if err != nil {
		t.Fatalf("route: %v", err)
	}
	if dec.Kind != inbound.RouteDecisionDropUnknown {
		t.Errorf("decision: %v", dec)
	}
}

func TestEscalatorScan_NotWired(t *testing.T) {
	var ss *ServerSubsystems
	_, err := ss.EscalatorScan(context.Background())
	if err == nil {
		t.Fatal("want error on nil receiver")
	}
}

func TestInboundRouter_NilReceiver(t *testing.T) {
	var ss *ServerSubsystems
	if ss.InboundRouter() != nil {
		t.Fatal("nil receiver should return nil")
	}
}

func TestRun_NoEscalator(t *testing.T) {
	var ss *ServerSubsystems
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	ss.Run(ctx) // should return immediately on cancelled ctx
}

func TestRun_WithEscalator(t *testing.T) {
	app := newTestAppWithFileDB(t)
	ss, err := NewServerSubsystems(app, func(string) {})
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	go func() {
		ss.Run(ctx)
	}()
	cancel()
}

// TestRun_CallsEscalator drives the Run loop with a cancellable ctx
// and confirms the escalator goroutine exits cleanly.
func TestRun_CallsEscalator(t *testing.T) {
	app := newTestAppWithFileDB(t)
	ss, err := NewServerSubsystems(app, func(string) {})
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() { ss.Run(ctx); close(done) }()
	cancel()
	<-done
}

// TestNewServerSubsystems_NilLogger covers the nil-logger default
// branch.
func TestNewServerSubsystems_NilLogger(t *testing.T) {
	app := newTestAppWithFileDB(t)
	ss, err := NewServerSubsystems(app, nil)
	if err != nil {
		t.Fatal(err)
	}
	// The logger should be a no-op; calling it must not panic.
	ss.logger("test")
}

func TestConnectAndCloseBridge_NoClient(t *testing.T) {
	app := newTestAppWithFileDB(t)
	ss, err := NewServerSubsystems(app, nil)
	if err != nil {
		t.Fatal(err)
	}
	if err := ss.ConnectBridge(context.Background()); err != nil {
		t.Errorf("connect: %v", err)
	}
	if err := ss.CloseBridge(); err != nil {
		t.Errorf("close: %v", err)
	}
}

func TestAttachFeishuClient_NoOpWhenNotWired(t *testing.T) {
	app := newTestAppWithFileDB(t)
	ss, err := NewServerSubsystems(app, nil)
	if err != nil {
		t.Fatal(err)
	}
	// Bridge disabled → inbound router nil → AttachFeishuClient is no-op.
	ss.AttachFeishuClient(&fakeClient{})
}

func TestAttachFeishuClient_HandlerInvoked(t *testing.T) {
	app := newTestAppWithFileDB(t)
	app.Config.Bridge.Feishu.Enabled = true
	ss, err := NewServerSubsystems(app, func(string) {})
	if err != nil {
		t.Fatal(err)
	}
	fc := &fakeClient{}
	ss.AttachFeishuClient(fc)
	if fc.handler == nil {
		t.Fatal("handler not registered")
	}
	// Invoking the handler should not panic; identity resolver will
	// fail (no user identity) but the router contains that.
	fc.handler(client.VendorEvent{Kind: "im.message.receive_v1", RawJSON: "{}"})
}

func TestConnectBridge_ClientError(t *testing.T) {
	app := newTestAppWithFileDB(t)
	app.Config.Bridge.Feishu.Enabled = true
	ss, err := NewServerSubsystems(app, func(string) {})
	if err != nil {
		t.Fatal(err)
	}
	ss.AttachFeishuClient(&fakeClient{connectErr: errors.New("boom")})
	if err := ss.ConnectBridge(context.Background()); err == nil {
		t.Fatal("want connect err")
	}
}

func TestCloseBridge_ClientError(t *testing.T) {
	app := newTestAppWithFileDB(t)
	app.Config.Bridge.Feishu.Enabled = true
	ss, err := NewServerSubsystems(app, func(string) {})
	if err != nil {
		t.Fatal(err)
	}
	ss.AttachFeishuClient(&fakeClient{closeErr: errors.New("boom")})
	if err := ss.CloseBridge(); err == nil {
		t.Fatal("want close err")
	}
}

func TestTranslateClientEvent(t *testing.T) {
	ev := TranslateClientEvent(client.VendorEvent{Kind: "x.y", RawJSON: "{}"})
	if ev.Kind != "x.y" {
		t.Errorf("kind: %s", ev.Kind)
	}
}

func TestNewFeishuAdapterFromConfig(t *testing.T) {
	app := newTestAppWithFileDB(t)
	app.Config.Bridge.Feishu.Enabled = false
	if NewFeishuAdapterFromConfig(app) != nil {
		t.Error("disabled bridge should return nil adapter")
	}
	app.Config.Bridge.Feishu.Enabled = true
	app.Config.Bridge.Feishu.AppID = "x"
	app.Config.Bridge.Feishu.AppSecret = "y"
	c := NewFeishuAdapterFromConfig(app)
	if c == nil {
		t.Error("enabled bridge should return adapter")
	}
}

// fakeClient implements bridge/feishu/client.Client for tests.
type fakeClient struct {
	handler    func(client.VendorEvent)
	connectErr error
	closeErr   error
}

func (f *fakeClient) Connect(ctx context.Context) error { return f.connectErr }
func (f *fakeClient) SendTextMessage(ctx context.Context, target client.Target, markdown string) (client.SendResult, error) {
	return client.SendResult{}, nil
}
func (f *fakeClient) SendInteractiveCard(ctx context.Context, target client.Target, cardJSON string) (client.SendResult, error) {
	return client.SendResult{}, nil
}
func (f *fakeClient) UpdateCard(ctx context.Context, cardMessageID string, cardJSON string) error {
	return nil
}
func (f *fakeClient) Close() error { return f.closeErr }
func (f *fakeClient) ConnectionStatus() client.ConnectionStatus {
	return client.StatusConnected
}
func (f *fakeClient) OnEvent(h func(client.VendorEvent)) { f.handler = h }
