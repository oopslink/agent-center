package cli

import (
	"context"
	"net"
	"net/http"
	"testing"
	"time"

	"github.com/oopslink/agent-center/internal/webconsole/sse"
)

// ============================================================================
// runWebConsole — starts the web console, hits /healthz, then shuts down.
// (v2.7 #162: the resolveSecretInput + convTail tests were removed with the
// retired secret/conversation CLI commands.)
// ============================================================================

func TestRunWebConsole_StartsAndStops(t *testing.T) {
	app := newTestApp(t)
	bus := sse.NewBus()
	defer bus.Shutdown(context.Background())
	// Grab a free loopback port.
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	addr := ln.Addr().String()
	_ = ln.Close()
	logs := []string{}
	cleanup, err := runWebConsole(context.Background(), app, bus, addr, WebConsoleEnrollWiring{}, func(s string) { logs = append(logs, s) })
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = cleanup() }()
	// Give listener a moment to bind.
	deadline := time.Now().Add(2 * time.Second)
	var resp *http.Response
	for time.Now().Before(deadline) {
		resp, err = http.Get("http://" + addr + "/healthz")
		if err == nil {
			break
		}
		time.Sleep(20 * time.Millisecond)
	}
	if err != nil {
		t.Fatalf("server never came up: %v", err)
	}
	_ = resp.Body.Close()
}

func TestRunWebConsole_NilApp(t *testing.T) {
	bus := sse.NewBus()
	defer bus.Shutdown(context.Background())
	_, err := runWebConsole(context.Background(), nil, bus, "127.0.0.1:0", WebConsoleEnrollWiring{}, func(string) {})
	if err == nil {
		t.Fatal("expected error for nil app")
	}
}
