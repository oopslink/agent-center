// Package client adapter — vendor SDK glue.
//
// THIS IS THE ONLY SOURCE FILE in the entire agent-center repo that imports
// the feishu vendor SDK (github.com/larksuite/oapi-sdk-go/v3). All other
// packages depend on the domain Client port (client.go) and stay vendor-
// clean per conventions § 9.y.
//
// The import-graph e2e test (`tests/e2e/phase5_test.go`) asserts:
//   1. No `internal/conversation/...`, `internal/taskruntime/...`,
//      `internal/discussion/...`, `internal/cognition/...` package imports
//      any `github.com/larksuite/oapi-sdk-go/v3/...` subpackage.
//   2. Within `internal/bridge/feishu/...`, this single file is the SDK
//      import leaf.
//
// v1 design notes:
//   - We use `larkcore` constants (URL paths, token type names) but issue
//     HTTP requests via `net/http` directly — narrow surface (~3 endpoints)
//     doesn't justify pulling 60 service subpackages into the binary
//     (plan-5 § 6 risk 7).
//   - The WebSocket long-poll is stubbed in v1: Connect issues a token
//     exchange and stores the result; inbound events are NOT yet routed to
//     OnEvent handlers — Phase 7 wires the real WS plumbing.
//   - The base URL is overridable so the fake server can swap endpoints
//     for integration / e2e tests (test-only injection point).
package client

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/oopslink/agent-center/internal/clock"

	// Single vendor SDK import on the entire agent-center side.
	// `larkcore` provides the open-API URL path constants + AppType enum;
	// importing it does NOT transitively pull in the 60+ service
	// subpackages (those live behind separate import paths).
	larkcore "github.com/larksuite/oapi-sdk-go/v3/core"
)

// AdapterConfig configures the HTTP adapter.
type AdapterConfig struct {
	// BaseURL defaults to FeishuBaseURL when empty (production).
	BaseURL string
	// AppID / AppSecret from `bridge feishu setup`.
	AppID     string
	AppSecret string
	// HTTPClient defaults to a 30s-timeout client.
	HTTPClient *http.Client
	// Clock for backoff calculations + token expiry tracking.
	Clock clock.Clock
	// MaxRetries on transient failures (5xx / network). Defaults to 3.
	MaxRetries int
	// BackoffStep is the unit of exponential backoff (1s by default). Use
	// a small value (e.g. 1ms) in tests + inject a manual clock.
	BackoffStep time.Duration
	// OnStateChange is called whenever the connection status changes.
	// Used by the dispatcher to emit observability events.
	OnStateChange func(ConnectionStatus, ReasonMessage)
}

// ReasonMessage carries the reason+message duo per conventions § 16. The
// OnStateChange callback receives one for every transition so callers can
// emit observability events that satisfy ADR-0014 + conventions § 16.
type ReasonMessage struct {
	Reason  string
	Message string
}

// FeishuBaseURL is the production endpoint (zh-CN tenant). Lark tenants use
// "https://open.larksuite.com" — overridable via AdapterConfig.BaseURL.
const FeishuBaseURL = "https://open.feishu.cn"

// OAPIAdapter is the HTTP-backed implementation of Client. It is the only
// type in the codebase that touches the feishu SDK; the dispatcher uses
// the Client interface (port).
type OAPIAdapter struct {
	cfg      AdapterConfig
	mu       sync.RWMutex
	status   ConnectionStatus
	token    string
	expires  time.Time
	handler  func(VendorEvent)
}

// NewOAPIAdapter constructs the adapter with defaults applied.
func NewOAPIAdapter(cfg AdapterConfig) *OAPIAdapter {
	if cfg.BaseURL == "" {
		cfg.BaseURL = FeishuBaseURL
	}
	if cfg.HTTPClient == nil {
		cfg.HTTPClient = &http.Client{Timeout: 30 * time.Second}
	}
	if cfg.Clock == nil {
		cfg.Clock = clock.SystemClock{}
	}
	if cfg.MaxRetries <= 0 {
		cfg.MaxRetries = 3
	}
	if cfg.BackoffStep <= 0 {
		cfg.BackoffStep = time.Second
	}
	// Reference the SDK package so it's pinned in the import graph; this
	// also lets static analyzers (`go list -deps`) confirm we DO use the
	// SDK even when only via constants.
	_ = larkcore.AppTypeSelfBuilt
	return &OAPIAdapter{cfg: cfg, status: StatusDisconnected, handler: func(VendorEvent) {}}
}

// Connect obtains a tenant access token via the SDK's documented endpoint
// (larkcore.TenantAccessTokenInternalUrlPath). It treats 4xx as permanent
// (ErrAuthFailed) and 5xx / network as transient (retries up to
// MaxRetries with exponential backoff).
func (a *OAPIAdapter) Connect(ctx context.Context) error {
	a.setStatus(StatusReconnecting, "connecting", "establishing feishu tenant access token")
	body, _ := json.Marshal(map[string]string{
		"app_id":     a.cfg.AppID,
		"app_secret": a.cfg.AppSecret,
	})
	url := strings.TrimRight(a.cfg.BaseURL, "/") + larkcore.TenantAccessTokenInternalUrlPath
	for attempt := 0; attempt <= a.cfg.MaxRetries; attempt++ {
		if attempt > 0 {
			a.sleepBackoff(attempt)
		}
		token, expiresIn, err := a.fetchTenantToken(ctx, url, body)
		if err == nil {
			a.mu.Lock()
			a.token = token
			a.expires = a.cfg.Clock.Now().Add(time.Duration(expiresIn) * time.Second)
			a.mu.Unlock()
			a.setStatus(StatusConnected, "connected", "feishu tenant access token acquired")
			return nil
		}
		if errors.Is(err, ErrAuthFailed) || errors.Is(err, ErrPermanentFailure) {
			a.setStatus(StatusDisconnected, "auth_failed", err.Error())
			return err
		}
		// transient → retry
	}
	a.setStatus(StatusDisconnected, "transient_exhausted",
		fmt.Sprintf("feishu connect retries (%d) exhausted", a.cfg.MaxRetries))
	return ErrTransientFailure
}

// fetchTenantToken issues one POST and classifies the response.
func (a *OAPIAdapter) fetchTenantToken(ctx context.Context, url string, body []byte) (string, int, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(body))
	if err != nil {
		return "", 0, err
	}
	req.Header.Set("Content-Type", "application/json; charset=utf-8")
	resp, err := a.cfg.HTTPClient.Do(req)
	if err != nil {
		return "", 0, ErrTransientFailure
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(resp.Body)
	if resp.StatusCode >= 500 {
		return "", 0, ErrTransientFailure
	}
	var parsed struct {
		Code              int    `json:"code"`
		Msg               string `json:"msg"`
		TenantAccessToken string `json:"tenant_access_token"`
		Expire            int    `json:"expire"`
	}
	if err := json.Unmarshal(raw, &parsed); err != nil {
		return "", 0, ErrTransientFailure
	}
	if resp.StatusCode >= 400 || parsed.Code != 0 {
		// 4xx + non-zero code → permanent auth failure
		return "", 0, fmt.Errorf("%w: code=%d msg=%s", ErrAuthFailed, parsed.Code, parsed.Msg)
	}
	if parsed.TenantAccessToken == "" {
		return "", 0, fmt.Errorf("%w: empty tenant_access_token", ErrAuthFailed)
	}
	if parsed.Expire <= 0 {
		parsed.Expire = 7200
	}
	return parsed.TenantAccessToken, parsed.Expire, nil
}

// SendTextMessage POSTs an im.v1.messages.create with msg_type=text.
func (a *OAPIAdapter) SendTextMessage(ctx context.Context, target Target, markdown string) (SendResult, error) {
	if !a.connected() {
		return SendResult{}, ErrNotConnected
	}
	contentJSON, _ := json.Marshal(map[string]string{"text": markdown})
	return a.sendMessage(ctx, target, "text", string(contentJSON))
}

// SendInteractiveCard POSTs an im.v1.messages.create with msg_type=interactive.
func (a *OAPIAdapter) SendInteractiveCard(ctx context.Context, target Target, cardJSON string) (SendResult, error) {
	if !a.connected() {
		return SendResult{}, ErrNotConnected
	}
	return a.sendMessage(ctx, target, "interactive", cardJSON)
}

// sendMessage is the shared POST + retry logic.
func (a *OAPIAdapter) sendMessage(ctx context.Context, target Target, msgType, contentJSON string) (SendResult, error) {
	// receive_id selection per Feishu API: if thread_key present, use chat;
	// else fall back to vendor_user_id (open_id).
	receiveID := target.ThreadKey
	receiveIDType := "chat_id"
	if receiveID == "" {
		receiveID = target.VendorUserID
		receiveIDType = "open_id"
	}
	if receiveID == "" {
		return SendResult{}, fmt.Errorf("%w: empty receive_id", ErrPermanentFailure)
	}
	body, _ := json.Marshal(map[string]any{
		"receive_id": receiveID,
		"msg_type":   msgType,
		"content":    contentJSON,
	})
	url := strings.TrimRight(a.cfg.BaseURL, "/") +
		"/open-apis/im/v1/messages?receive_id_type=" + receiveIDType
	var lastErr error
	for attempt := 0; attempt <= a.cfg.MaxRetries; attempt++ {
		if attempt > 0 {
			a.sleepBackoff(attempt)
		}
		res, err := a.doSend(ctx, url, body, msgType, target)
		if err == nil {
			return res, nil
		}
		lastErr = err
		if errors.Is(err, ErrPermanentFailure) {
			return SendResult{}, err
		}
		// transient → retry
	}
	if lastErr == nil {
		lastErr = ErrTransientFailure
	}
	return SendResult{}, lastErr
}

// doSend issues one POST and classifies the result.
func (a *OAPIAdapter) doSend(ctx context.Context, url string, body []byte, msgType string, target Target) (SendResult, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(body))
	if err != nil {
		return SendResult{}, err
	}
	a.mu.RLock()
	token := a.token
	a.mu.RUnlock()
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Content-Type", "application/json; charset=utf-8")
	resp, err := a.cfg.HTTPClient.Do(req)
	if err != nil {
		return SendResult{}, ErrTransientFailure
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(resp.Body)
	if resp.StatusCode >= 500 {
		return SendResult{}, fmt.Errorf("%w: %d %s", ErrTransientFailure, resp.StatusCode, string(raw))
	}
	if resp.StatusCode >= 400 {
		return SendResult{}, fmt.Errorf("%w: %d %s", ErrPermanentFailure, resp.StatusCode, string(raw))
	}
	var parsed struct {
		Code int    `json:"code"`
		Msg  string `json:"msg"`
		Data struct {
			MessageID string `json:"message_id"`
			ChatID    string `json:"chat_id"`
		} `json:"data"`
	}
	if err := json.Unmarshal(raw, &parsed); err != nil {
		return SendResult{}, fmt.Errorf("%w: parse: %v body=%s", ErrTransientFailure, err, string(raw))
	}
	if parsed.Code != 0 {
		// non-zero code with 2xx → server-side business failure; treat as permanent
		return SendResult{}, fmt.Errorf("%w: code=%d msg=%s", ErrPermanentFailure, parsed.Code, parsed.Msg)
	}
	res := SendResult{
		VendorMsgRef: parsed.Data.MessageID,
		ThreadKey:    parsed.Data.ChatID,
	}
	if res.ThreadKey == "" {
		res.ThreadKey = target.ThreadKey
	}
	if msgType == "interactive" {
		res.CardMessageID = parsed.Data.MessageID
	}
	return res, nil
}

// UpdateCard is reserved for v2+ per plan-5 § 3.4 + bridge/01 § 11.
func (a *OAPIAdapter) UpdateCard(ctx context.Context, cardMessageID string, cardJSON string) error {
	return ErrUpdateCardNotSupported
}

// Close terminates the connection (in v1 there is no persistent socket;
// this just flips the state).
func (a *OAPIAdapter) Close() error {
	a.setStatus(StatusDisconnected, "shutdown", "feishu client closed")
	return nil
}

// ConnectionStatus returns the current status snapshot.
func (a *OAPIAdapter) ConnectionStatus() ConnectionStatus {
	a.mu.RLock()
	defer a.mu.RUnlock()
	return a.status
}

// OnEvent stores the inbound handler. Phase 5 leaves the handler unused
// (no real WS reader); Phase 7 wires the WS event loop to invoke it.
func (a *OAPIAdapter) OnEvent(handler func(VendorEvent)) {
	a.mu.Lock()
	defer a.mu.Unlock()
	if handler == nil {
		a.handler = func(VendorEvent) {}
		return
	}
	a.handler = handler
}

// InjectEvent drives the registered handler synchronously. This is the
// Phase 7 hook used by:
//   - Production WS reader goroutine in `oapi_adapter.go` once the
//     real long-poll is wired (v1 falls back to the fake WS when the
//     SDK lacks ready-made support).
//   - The test fake server (tests/e2e/fakeserver/feishu) injects
//     events here to exercise the inbound router end-to-end.
//
// Returns immediately when no handler is registered. The function is
// goroutine-safe.
func (a *OAPIAdapter) InjectEvent(ev VendorEvent) {
	a.mu.RLock()
	h := a.handler
	a.mu.RUnlock()
	if h == nil {
		return
	}
	h(ev)
}

func (a *OAPIAdapter) connected() bool {
	a.mu.RLock()
	defer a.mu.RUnlock()
	return a.status == StatusConnected
}

func (a *OAPIAdapter) setStatus(status ConnectionStatus, reason, message string) {
	a.mu.Lock()
	prev := a.status
	a.status = status
	a.mu.Unlock()
	if prev != status && a.cfg.OnStateChange != nil {
		a.cfg.OnStateChange(status, ReasonMessage{Reason: reason, Message: message})
	}
}

func (a *OAPIAdapter) sleepBackoff(attempt int) {
	d := a.cfg.BackoffStep << (attempt - 1)
	clock.SleepWith(a.cfg.Clock, d)
}
