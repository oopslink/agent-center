package cli

import (
	"context"
	"net"
	"net/http"
	"os"
	"testing"
	"time"

	"github.com/oopslink/agent-center/internal/webconsole/sse"
)

// ============================================================================
// runWebConsole — starts the web console, hits /healthz, then shuts down.
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

// ============================================================================
// resolveSecretInput — stdin "-" + piped stdin branches
// ============================================================================

// withStdin temporarily replaces os.Stdin with a pipe that yields data.
func withStdin(t *testing.T, data string, fn func()) {
	t.Helper()
	orig := os.Stdin
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	if _, err := w.WriteString(data); err != nil {
		t.Fatal(err)
	}
	_ = w.Close()
	os.Stdin = r
	defer func() {
		os.Stdin = orig
		_ = r.Close()
	}()
	fn()
}

func TestResolveSecretInput_StdinDash(t *testing.T) {
	withStdin(t, "secret-data\n", func() {
		got, err := resolveSecretInput("-")
		if err != nil {
			t.Fatal(err)
		}
		if string(got) != "secret-data" {
			t.Fatalf("got %q", got)
		}
	})
}

func TestResolveSecretInput_StdinDashEmpty(t *testing.T) {
	withStdin(t, "", func() {
		_, err := resolveSecretInput("-")
		if err == nil {
			t.Fatal()
		}
	})
}

func TestResolveSecretInput_PipedStdin(t *testing.T) {
	withStdin(t, "piped-value\n", func() {
		got, err := resolveSecretInput("")
		if err != nil {
			t.Fatal(err)
		}
		if string(got) != "piped-value" {
			t.Fatalf("got %q", got)
		}
	})
}

// ============================================================================
// convTail follow mode — exercises ticker loop + context cancellation.
// ============================================================================

func TestCLI_ConvTail_FollowCancellation(t *testing.T) {
	app := newTestApp(t)
	_, _, _ = runOn(t, app, "channel", "create", []string{"--name=tfollow"})
	out, _, _ := runOn(t, app, "channel", "show", []string{"tfollow", "--format=json"})
	// Trivially grab the conv id without re-parsing — read directly.
	convs, _ := app.ConvRepo.FindByName(context.Background(), "tfollow")
	if convs == nil {
		t.Fatalf("no conv: %s", out)
	}
	cid := string(convs.ID())
	cmd := findCmd(app.ConversationCommands(), "tail")
	if cmd == nil {
		t.Fatal()
	}
	ctx, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
	defer cancel()
	_, _, _ = runHandlerCtx(t, ctx, cmd, []string{cid, "-f", "--interval=1"})
}
