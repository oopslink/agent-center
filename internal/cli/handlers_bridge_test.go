package cli

import (
	"bytes"
	"context"
	"flag"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/oopslink/agent-center/internal/bridge/feishu/client"
	"github.com/oopslink/agent-center/internal/observability"
)

func runBridgeSetup(t *testing.T, app *App, args []string) (string, string, ExitCode) {
	t.Helper()
	bridgeCmds := app.BridgeCommands()
	feishu := findCmd(bridgeCmds, "feishu")
	setup := findCmd(feishu.Subcommands, "setup")
	var outBuf, errBuf bytes.Buffer
	fs := flag.NewFlagSet("setup", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	handler := setup.Flags(fs)
	positionals, err := permissiveParse(fs, args)
	if err != nil {
		errBuf.WriteString("usage: " + err.Error())
		return outBuf.String(), errBuf.String(), ExitUsage
	}
	code := handler(context.Background(), positionals, &outBuf, &errBuf)
	return outBuf.String(), errBuf.String(), code
}

// withFakeFeishuServer spins up a fake feishu server and registers a global
// config path so persistFeishuBridgeConfig writes somewhere predictable.
func withFakeFeishuServer(t *testing.T) (*client.FakeServer, string) {
	t.Helper()
	fs := client.NewFakeServer()
	t.Cleanup(fs.Close)
	cfgPath := filepath.Join(t.TempDir(), "agent-center.yaml")
	if err := os.WriteFile(cfgPath, []byte("server:\n  listen_addr: ':7000'\n  sqlite_path: '/tmp/x'\nidentity:\n  default_user: hayang\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	SetGlobalConfigPath(cfgPath)
	t.Cleanup(func() { SetGlobalConfigPath("") })
	return fs, cfgPath
}

func TestBridgeFeishuSetupHappy(t *testing.T) {
	app := newTestApp(t)
	fs, cfgPath := withFakeFeishuServer(t)
	secretPath := filepath.Join(t.TempDir(), "secret")
	if err := os.WriteFile(secretPath, []byte("super-secret"), 0o600); err != nil {
		t.Fatal(err)
	}
	stdout, _, code := runBridgeSetup(t, app, []string{
		"--app-id=cli_test_123",
		"--app-secret-file=" + secretPath,
		"--base-url=" + fs.URL(),
	})
	if code != ExitOK {
		t.Fatalf("exit %d stdout=%s", code, stdout)
	}
	if !strings.Contains(stdout, "cli_test_123") {
		t.Fatalf("stdout: %s", stdout)
	}
	// config rewrite happened.
	body, _ := os.ReadFile(cfgPath)
	if !strings.Contains(string(body), "app_id: cli_test_123") {
		t.Fatalf("config not rewritten: %s", body)
	}
	if !strings.Contains(string(body), "enabled: true") {
		t.Fatalf("enabled missing: %s", body)
	}
	// bridge.feishu.connection_state_changed connected event emitted.
	events, _ := app.EventRepo.Find(context.Background(), observability.EventQueryFilter{})
	connected := false
	for _, e := range events {
		if e.Type() == "bridge.feishu.connection_state_changed" {
			st, _ := e.Payload()["state"].(string)
			if st == "connected" {
				connected = true
			}
		}
	}
	if !connected {
		t.Fatal("connected event missing")
	}
}

func TestBridgeFeishuSetupMissingFlags(t *testing.T) {
	app := newTestApp(t)
	_, _, code := runBridgeSetup(t, app, nil)
	if code != ExitUsage {
		t.Fatalf("want ExitUsage, got %d", code)
	}
	_, _, code = runBridgeSetup(t, app, []string{"--app-id=x"})
	if code != ExitUsage {
		t.Fatalf("want ExitUsage on missing secret, got %d", code)
	}
}

func TestBridgeFeishuSetupSecretFileMissing(t *testing.T) {
	app := newTestApp(t)
	_, _ = withFakeFeishuServer(t)
	_, stderr, code := runBridgeSetup(t, app, []string{
		"--app-id=x", "--app-secret-file=/proc/no/such/file",
	})
	if code != ExitUsage {
		t.Fatalf("exit %d", code)
	}
	if !strings.Contains(stderr, "app_secret_file_unreadable") {
		t.Fatalf("stderr: %s", stderr)
	}
}

func TestBridgeFeishuSetupEmptySecretFile(t *testing.T) {
	app := newTestApp(t)
	_, _ = withFakeFeishuServer(t)
	_ = app
	dir := t.TempDir()
	secretPath := filepath.Join(dir, "secret")
	_ = os.WriteFile(secretPath, []byte("   \n"), 0o600)
	_, stderr, code := runBridgeSetup(t, app, []string{
		"--app-id=x", "--app-secret-file=" + secretPath,
	})
	if code != ExitUsage {
		t.Fatalf("exit %d", code)
	}
	if !strings.Contains(stderr, "app_secret_file_empty") {
		t.Fatalf("stderr: %s", stderr)
	}
}

func TestBridgeFeishuSetupConfigPathUnknown(t *testing.T) {
	app := newTestApp(t)
	SetGlobalConfigPath("")
	defer SetGlobalConfigPath("")
	secret := filepath.Join(t.TempDir(), "secret")
	_ = os.WriteFile(secret, []byte("s"), 0o600)
	_, stderr, code := runBridgeSetup(t, app, []string{
		"--app-id=x", "--app-secret-file=" + secret,
	})
	if code != ExitUsage {
		t.Fatalf("exit %d", code)
	}
	if !strings.Contains(stderr, "config_path_unknown") {
		t.Fatalf("stderr: %s", stderr)
	}
}

func TestBridgeFeishuSetupConnectFailsEmitsDisconnected(t *testing.T) {
	app := newTestApp(t)
	fs, _ := withFakeFeishuServer(t)
	fs.SetAuthFails(true)
	secretPath := filepath.Join(t.TempDir(), "secret")
	_ = os.WriteFile(secretPath, []byte("bad"), 0o600)
	_, stderr, code := runBridgeSetup(t, app, []string{
		"--app-id=cli_bad", "--app-secret-file=" + secretPath, "--base-url=" + fs.URL(),
	})
	if code != ExitBusinessError {
		t.Fatalf("exit %d stderr=%s", code, stderr)
	}
	if !strings.Contains(stderr, "feishu_auth_failed") {
		t.Fatalf("stderr: %s", stderr)
	}
	events, _ := app.EventRepo.Find(context.Background(), observability.EventQueryFilter{})
	hasDisconnected := false
	for _, e := range events {
		if e.Type() == "bridge.feishu.connection_state_changed" {
			st, _ := e.Payload()["state"].(string)
			if st == "disconnected" {
				hasDisconnected = true
			}
		}
	}
	if !hasDisconnected {
		t.Fatal("disconnected event missing")
	}
}

func TestBridgeFeishuSetupSkipsSmoke(t *testing.T) {
	app := newTestApp(t)
	_, cfgPath := withFakeFeishuServer(t)
	secret := filepath.Join(t.TempDir(), "secret")
	_ = os.WriteFile(secret, []byte("x"), 0o600)
	_, _, code := runBridgeSetup(t, app, []string{
		"--app-id=skip", "--app-secret-file=" + secret, "--skip-smoke-test",
	})
	if code != ExitOK {
		t.Fatalf("exit %d", code)
	}
	body, _ := os.ReadFile(cfgPath)
	if !strings.Contains(string(body), "skip") {
		t.Fatalf("config: %s", body)
	}
}

func TestMergeBridgeFeishuYAMLPreservesOthers(t *testing.T) {
	t.Parallel()
	in := "server:\n  listen_addr: ':7000'\nidentity:\n  default_user: hayang\nbridge:\n  feishu:\n    enabled: false\n    app_id: old\n"
	out := mergeBridgeFeishuYAML(in, "new_app", "/secret", "")
	if !strings.Contains(out, "listen_addr") || strings.Contains(out, "old") {
		t.Fatalf("merge dropped existing or kept stale: %s", out)
	}
	if !strings.Contains(out, "app_id: new_app") {
		t.Fatalf("new app id missing: %s", out)
	}
}

func TestStripBridgeSectionConservative(t *testing.T) {
	t.Parallel()
	// Comment lines and indented keys must not be confused for top-level keys.
	in := "# comment\nserver:\n  listen_addr: ':7000'\nbridge:\n  feishu:\n    enabled: true\nidentity:\n  default_user: x\n"
	out := stripBridgeSection(in)
	if strings.Contains(out, "feishu") {
		t.Fatalf("bridge section not stripped: %s", out)
	}
	if !strings.Contains(out, "listen_addr") || !strings.Contains(out, "default_user") {
		t.Fatalf("other keys lost: %s", out)
	}
}

func TestClassifyConnectError(t *testing.T) {
	t.Parallel()
	for err, want := range map[error]string{
		client.ErrAuthFailed:         "feishu_auth_failed",
		client.ErrTransientFailure:   "feishu_transient_failure",
		client.ErrPermanentFailure:   "feishu_permanent_failure",
		client.ErrNotConnected:       "feishu_connect_failed",
	} {
		got, _ := classifyConnectError(err)
		if got != want {
			t.Errorf("classify(%v)=%s want %s", err, got, want)
		}
	}
}
