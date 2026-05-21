package client_test

import (
	"context"
	"errors"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/oopslink/agent-center/internal/bridge/feishu/client"
	"github.com/oopslink/agent-center/internal/clock"
)

type stateRecorder struct {
	mu       sync.Mutex
	statuses []client.ConnectionStatus
	reasons  []string
}

func (s *stateRecorder) record(st client.ConnectionStatus, reasonMsg client.ReasonMessage) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.statuses = append(s.statuses, st)
	s.reasons = append(s.reasons, reasonMsg.Reason+":"+reasonMsg.Message)
}

func TestConnectHappy(t *testing.T) {
	fs := client.NewFakeServer()
	defer fs.Close()
	rec := &stateRecorder{}
	a := client.NewOAPIAdapter(client.AdapterConfig{
		BaseURL: fs.URL(), AppID: "app", AppSecret: "secret",
		Clock: clock.NewFakeClock(time.Date(2026, 5, 22, 10, 0, 0, 0, time.UTC)),
		OnStateChange: rec.record,
		MaxRetries:  1,
		BackoffStep: time.Millisecond,
	})
	if err := a.Connect(context.Background()); err != nil {
		t.Fatalf("connect: %v", err)
	}
	if a.ConnectionStatus() != client.StatusConnected {
		t.Fatalf("status: %s", a.ConnectionStatus())
	}
	if len(rec.statuses) < 2 || rec.statuses[len(rec.statuses)-1] != client.StatusConnected {
		t.Fatalf("state changes %v", rec.statuses)
	}
}

func TestConnectAuthFailedNoRetry(t *testing.T) {
	fs := client.NewFakeServer()
	defer fs.Close()
	fs.SetAuthFails(true)
	a := client.NewOAPIAdapter(client.AdapterConfig{
		BaseURL: fs.URL(), AppID: "app", AppSecret: "bad",
		Clock: clock.NewFakeClock(time.Date(2026, 5, 22, 10, 0, 0, 0, time.UTC)),
		MaxRetries: 3, BackoffStep: time.Millisecond,
	})
	if err := a.Connect(context.Background()); !errors.Is(err, client.ErrAuthFailed) {
		t.Fatalf("want ErrAuthFailed, got %v", err)
	}
	if a.ConnectionStatus() != client.StatusDisconnected {
		t.Fatalf("status: %s", a.ConnectionStatus())
	}
}

func TestConnectTransientRetryThenSuccess(t *testing.T) {
	fs := client.NewFakeServer()
	defer fs.Close()
	fs.SetConnect5xx(2) // next two calls 500, third succeeds
	a := client.NewOAPIAdapter(client.AdapterConfig{
		BaseURL: fs.URL(), AppID: "app", AppSecret: "ok",
		Clock: clock.NewFakeClock(time.Date(2026, 5, 22, 10, 0, 0, 0, time.UTC)),
		MaxRetries: 3, BackoffStep: time.Microsecond,
	})
	if err := a.Connect(context.Background()); err != nil {
		t.Fatalf("connect: %v", err)
	}
	if a.ConnectionStatus() != client.StatusConnected {
		t.Fatalf("status: %s", a.ConnectionStatus())
	}
}

func TestConnectTransientExhausted(t *testing.T) {
	fs := client.NewFakeServer()
	defer fs.Close()
	fs.SetConnect5xx(99)
	a := client.NewOAPIAdapter(client.AdapterConfig{
		BaseURL: fs.URL(), AppID: "app", AppSecret: "ok",
		Clock: clock.NewFakeClock(time.Date(2026, 5, 22, 10, 0, 0, 0, time.UTC)),
		MaxRetries: 2, BackoffStep: time.Microsecond,
	})
	if err := a.Connect(context.Background()); !errors.Is(err, client.ErrTransientFailure) {
		t.Fatalf("want ErrTransientFailure, got %v", err)
	}
}

func TestSendTextNotConnected(t *testing.T) {
	fs := client.NewFakeServer()
	defer fs.Close()
	a := client.NewOAPIAdapter(client.AdapterConfig{BaseURL: fs.URL(), AppID: "x", AppSecret: "y"})
	if _, err := a.SendTextMessage(context.Background(), client.Target{ThreadKey: "t"}, "hi"); !errors.Is(err, client.ErrNotConnected) {
		t.Fatalf("want ErrNotConnected, got %v", err)
	}
}

func TestSendTextHappy(t *testing.T) {
	fs := client.NewFakeServer()
	defer fs.Close()
	a := connected(t, fs)
	res, err := a.SendTextMessage(context.Background(), client.Target{ThreadKey: "oc_123"}, "hello")
	if err != nil {
		t.Fatal(err)
	}
	if res.VendorMsgRef == "" {
		t.Fatal("missing vendor_msg_ref")
	}
	if !strings.HasPrefix(fs.Sends()[0].Content, `{"text":"hello`) {
		t.Fatalf("payload: %s", fs.Sends()[0].Content)
	}
}

func TestSendInteractiveCardHappy(t *testing.T) {
	fs := client.NewFakeServer()
	defer fs.Close()
	a := connected(t, fs)
	res, err := a.SendInteractiveCard(context.Background(), client.Target{ThreadKey: "oc_x"}, `{"elements":[]}`)
	if err != nil {
		t.Fatal(err)
	}
	if res.CardMessageID == "" {
		t.Fatal("card_message_id missing")
	}
	if fs.Sends()[0].MsgType != "interactive" {
		t.Fatalf("msg_type: %s", fs.Sends()[0].MsgType)
	}
}

func TestSendPermanentNoRetry(t *testing.T) {
	fs := client.NewFakeServer()
	defer fs.Close()
	a := connected(t, fs)
	fs.SetSendPermFails(true)
	if _, err := a.SendTextMessage(context.Background(), client.Target{ThreadKey: "x"}, "y"); !errors.Is(err, client.ErrPermanentFailure) {
		t.Fatalf("want ErrPermanentFailure, got %v", err)
	}
	// Only one send recorded (no retry).
	if len(fs.Sends()) != 0 {
		t.Fatalf("expected 0 recorded sends (4xx returns before record), got %d", len(fs.Sends()))
	}
}

func TestSendTransientRetryThenOk(t *testing.T) {
	fs := client.NewFakeServer()
	defer fs.Close()
	a := connected(t, fs)
	fs.SetSend5xx(2)
	if _, err := a.SendTextMessage(context.Background(), client.Target{ThreadKey: "x"}, "y"); err != nil {
		t.Fatalf("retry should recover: %v", err)
	}
	if fs.SendCount() != 1 {
		t.Fatalf("expected 1 success send, got %d", fs.SendCount())
	}
}

func TestSendTransientExhausted(t *testing.T) {
	fs := client.NewFakeServer()
	defer fs.Close()
	a := connected(t, fs)
	fs.SetSend5xx(99)
	if _, err := a.SendTextMessage(context.Background(), client.Target{ThreadKey: "x"}, "y"); !errors.Is(err, client.ErrTransientFailure) {
		t.Fatalf("want ErrTransientFailure, got %v", err)
	}
}

func TestSendEmptyReceiveID(t *testing.T) {
	fs := client.NewFakeServer()
	defer fs.Close()
	a := connected(t, fs)
	if _, err := a.SendTextMessage(context.Background(), client.Target{}, "y"); !errors.Is(err, client.ErrPermanentFailure) {
		t.Fatalf("want ErrPermanentFailure, got %v", err)
	}
}

func TestUpdateCardNotSupported(t *testing.T) {
	fs := client.NewFakeServer()
	defer fs.Close()
	a := connected(t, fs)
	if err := a.UpdateCard(context.Background(), "card_1", `{}`); !errors.Is(err, client.ErrUpdateCardNotSupported) {
		t.Fatalf("want ErrUpdateCardNotSupported, got %v", err)
	}
}

func TestCloseAndOnEvent(t *testing.T) {
	fs := client.NewFakeServer()
	defer fs.Close()
	a := connected(t, fs)
	got := 0
	a.OnEvent(func(client.VendorEvent) { got++ })
	a.OnEvent(nil) // nil handler resets to no-op
	if err := a.Close(); err != nil {
		t.Fatal(err)
	}
	if a.ConnectionStatus() != client.StatusDisconnected {
		t.Fatalf("status: %s", a.ConnectionStatus())
	}
}

func TestConnectBadJSONResponse(t *testing.T) {
	// We can't make the fake return bad JSON via flags; we instead point
	// the adapter at a URL that returns a non-JSON 4xx (use SetAuthFails).
	fs := client.NewFakeServer()
	defer fs.Close()
	fs.SetAuthFails(true)
	a := client.NewOAPIAdapter(client.AdapterConfig{
		BaseURL: fs.URL(), AppID: "x", AppSecret: "y",
		MaxRetries: 1, BackoffStep: time.Microsecond,
	})
	if err := a.Connect(context.Background()); !errors.Is(err, client.ErrAuthFailed) {
		t.Fatalf("want auth failed, got %v", err)
	}
}

// connected returns an adapter that has finished a happy Connect.
func connected(t *testing.T, fs *client.FakeServer) *client.OAPIAdapter {
	t.Helper()
	a := client.NewOAPIAdapter(client.AdapterConfig{
		BaseURL: fs.URL(), AppID: "x", AppSecret: "y",
		Clock: clock.NewFakeClock(time.Date(2026, 5, 22, 10, 0, 0, 0, time.UTC)),
		MaxRetries: 2, BackoffStep: time.Microsecond,
	})
	if err := a.Connect(context.Background()); err != nil {
		t.Fatalf("connect: %v", err)
	}
	return a
}
