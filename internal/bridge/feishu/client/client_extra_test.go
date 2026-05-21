package client_test

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/oopslink/agent-center/internal/bridge/feishu/client"
	"github.com/oopslink/agent-center/internal/clock"
)

// Connect when fetchTenantToken returns non-JSON / malformed body.
func TestConnectMalformedJSON(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("not json"))
	}))
	defer srv.Close()
	a := client.NewOAPIAdapter(client.AdapterConfig{
		BaseURL: srv.URL, AppID: "x", AppSecret: "y",
		MaxRetries: 1, BackoffStep: time.Microsecond,
	})
	if err := a.Connect(context.Background()); !errors.Is(err, client.ErrTransientFailure) {
		t.Fatalf("malformed json: want transient, got %v", err)
	}
}

// Connect when body parses to {"code": 0, "tenant_access_token": ""}.
func TestConnectEmptyToken(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"code":0,"msg":"ok","tenant_access_token":""}`))
	}))
	defer srv.Close()
	a := client.NewOAPIAdapter(client.AdapterConfig{
		BaseURL: srv.URL, AppID: "x", AppSecret: "y",
		MaxRetries: 1, BackoffStep: time.Microsecond,
	})
	if err := a.Connect(context.Background()); !errors.Is(err, client.ErrAuthFailed) {
		t.Fatalf("want auth failed, got %v", err)
	}
}

// Connect when the HTTP transport fails (transient).
func TestConnectTransportError(t *testing.T) {
	a := client.NewOAPIAdapter(client.AdapterConfig{
		BaseURL: "http://127.0.0.1:1", // closed port → connect refused
		AppID:   "x", AppSecret: "y",
		MaxRetries:  1,
		BackoffStep: time.Microsecond,
		HTTPClient:  &http.Client{Timeout: 500 * time.Millisecond},
	})
	if err := a.Connect(context.Background()); !errors.Is(err, client.ErrTransientFailure) {
		t.Fatalf("want transient, got %v", err)
	}
}

// sendMessage with malformed 2xx body → transient.
func TestSendMessageMalformed(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/open-apis/auth/v3/tenant_access_token/internal", func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"code":0,"tenant_access_token":"t","expire":3600}`))
	})
	mux.HandleFunc("/open-apis/im/v1/messages", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("not-json"))
	})
	srv := httptest.NewServer(mux)
	defer srv.Close()
	a := client.NewOAPIAdapter(client.AdapterConfig{
		BaseURL: srv.URL, AppID: "x", AppSecret: "y",
		MaxRetries: 0, BackoffStep: time.Microsecond,
		Clock: clock.NewFakeClock(time.Date(2026, 5, 22, 10, 0, 0, 0, time.UTC)),
	})
	if err := a.Connect(context.Background()); err != nil {
		t.Fatal(err)
	}
	if _, err := a.SendTextMessage(context.Background(), client.Target{ThreadKey: "x"}, "y"); !errors.Is(err, client.ErrTransientFailure) {
		t.Fatalf("want transient, got %v", err)
	}
}

// SendMessage with code != 0 in 2xx body → permanent.
func TestSendMessageBusinessFailure(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/open-apis/auth/v3/tenant_access_token/internal", func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"code":0,"tenant_access_token":"t","expire":3600}`))
	})
	mux.HandleFunc("/open-apis/im/v1/messages", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"code":230002,"msg":"invalid"}`))
	})
	srv := httptest.NewServer(mux)
	defer srv.Close()
	a := client.NewOAPIAdapter(client.AdapterConfig{
		BaseURL: srv.URL, AppID: "x", AppSecret: "y", MaxRetries: 0, BackoffStep: time.Microsecond,
	})
	if err := a.Connect(context.Background()); err != nil {
		t.Fatal(err)
	}
	if _, err := a.SendTextMessage(context.Background(), client.Target{ThreadKey: "x"}, "y"); !errors.Is(err, client.ErrPermanentFailure) {
		t.Fatalf("want permanent, got %v", err)
	}
}

// fake server unknown endpoint path → 404.
func TestFakeServerUnknownEndpoint(t *testing.T) {
	fs := client.NewFakeServer()
	defer fs.Close()
	resp, err := http.Post(fs.URL()+"/unknown", "application/json", nil)
	if err != nil {
		t.Fatal(err)
	}
	if resp.StatusCode != http.StatusNotFound {
		t.Fatalf("status: %d", resp.StatusCode)
	}
}
