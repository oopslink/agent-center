package e2e

import (
	"bytes"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/oopslink/agent-center/internal/bridge/feishu/client"
)

// TestE2EP5_IdentityLifecycle drives identity add / list / bind / unbind end
// to end through the compiled binary and verifies the SQLite tables and
// events ledger reflect each step.
func TestE2EP5_IdentityLifecycle(t *testing.T) {
	h := newHarness(t)
	// add
	stdout, _, code := h.run("identity", "add", "user:hayang", "--kind=user", "--display-name=Hayang")
	if code != 0 {
		t.Fatalf("add: code=%d stdout=%s", code, stdout)
	}
	if !strings.Contains(stdout, "user:hayang") {
		t.Fatalf("stdout: %s", stdout)
	}
	// list --format=json
	stdout, _, code = h.run("identity", "list", "--format=json")
	if code != 0 {
		t.Fatalf("list: %d", code)
	}
	var arr []map[string]any
	if err := json.Unmarshal([]byte(strings.TrimSpace(stdout)), &arr); err != nil {
		t.Fatalf("unmarshal: %v stdout=%s", err, stdout)
	}
	if len(arr) != 1 || arr[0]["identity_id"] != "user:hayang" {
		t.Fatalf("list: %v", arr)
	}
	// bind
	_, _, code = h.run("identity", "bind", "user:hayang",
		"--channel=feishu", "--vendor-user-id=ou_e2e", "--preferred")
	if code != 0 {
		t.Fatalf("bind: %d", code)
	}
	rows := queryDB(t, h.dbPath,
		`SELECT identity_id, channel, vendor_user_id, preferred FROM channel_bindings`)
	if len(rows) != 1 || rows[0]["vendor_user_id"] != "ou_e2e" || rows[0]["preferred"] != "1" {
		t.Fatalf("channel_bindings rows: %v", rows)
	}
	// unbind
	_, _, code = h.run("identity", "unbind", "user:hayang", "--channel=feishu")
	if code != 0 {
		t.Fatalf("unbind: %d", code)
	}
	rows = queryDB(t, h.dbPath, `SELECT * FROM channel_bindings`)
	if len(rows) != 0 {
		t.Fatalf("unbind left rows: %v", rows)
	}
	// events table contains identity.registered + identity.channel_bound + identity.channel_unbound
	evRows := queryDB(t, h.dbPath,
		`SELECT event_type FROM events WHERE event_type LIKE 'identity.%' ORDER BY id`)
	want := []string{"identity.registered", "identity.channel_bound", "identity.channel_unbound"}
	if len(evRows) != len(want) {
		t.Fatalf("identity events: %v want %v", evRows, want)
	}
	for i, r := range evRows {
		if r["event_type"] != want[i] {
			t.Fatalf("identity events[%d]: %s want %s", i, r["event_type"], want[i])
		}
	}
}

// TestE2EP5_BridgeFeishuSetup writes a secret file, hits the fake feishu
// server via the OAPIAdapter, and verifies the config file got rewritten +
// a connected event made it into the events table.
func TestE2EP5_BridgeFeishuSetup(t *testing.T) {
	fs := client.NewFakeServer()
	defer fs.Close()
	h := newHarness(t)
	secretPath := filepath.Join(t.TempDir(), "secret")
	if err := os.WriteFile(secretPath, []byte("super-secret"), 0o600); err != nil {
		t.Fatal(err)
	}
	stdout, _, code := h.run("bridge", "feishu", "setup",
		"--app-id=cli_e2e", "--app-secret-file="+secretPath, "--base-url="+fs.URL())
	if code != 0 {
		t.Fatalf("setup: code=%d stdout=%s", code, stdout)
	}
	body, _ := os.ReadFile(h.cfgPath)
	if !strings.Contains(string(body), "app_id: cli_e2e") {
		t.Fatalf("config: %s", body)
	}
	if !strings.Contains(string(body), "enabled: true") {
		t.Fatalf("config: %s", body)
	}
	// events table has bridge.feishu.connection_state_changed with state=connected.
	rows := queryDB(t, h.dbPath,
		`SELECT event_type, payload FROM events WHERE event_type='bridge.feishu.connection_state_changed'`)
	if len(rows) == 0 {
		t.Fatal("no connection_state_changed event")
	}
	if !strings.Contains(rows[0]["payload"], "connected") {
		t.Fatalf("payload: %s", rows[0]["payload"])
	}
}

// TestE2EP5_BridgeFeishuSetupSecretFileMissing verifies the readability
// pre-check fails fast and exit code 2.
func TestE2EP5_BridgeFeishuSetupSecretFileMissing(t *testing.T) {
	h := newHarness(t)
	_, stderr, code := h.run("bridge", "feishu", "setup",
		"--app-id=x", "--app-secret-file=/proc/no/such/file")
	if code != 2 {
		t.Fatalf("exit %d stderr=%s", code, stderr)
	}
	if !strings.Contains(stderr, "app_secret_file_unreadable") {
		t.Fatalf("stderr: %s", stderr)
	}
}

// repoRoot returns the agent-center repo root (parent of tests/e2e). All
// import-graph tests must run from there so `./internal/...` patterns
// resolve.
func repoRoot(t *testing.T) string {
	t.Helper()
	out, err := exec.Command("go", "list", "-m", "-f", "{{.Dir}}").Output()
	if err != nil {
		t.Fatalf("go list -m: %v", err)
	}
	return strings.TrimSpace(string(out))
}

// TestE2EP5_ImportGraph_NoFeishuSDKLeak walks the dependency graph of every
// domain BC package and verifies github.com/larksuite/oapi-sdk-go is NOT
// present. Then walks internal/bridge/feishu/... and verifies the SDK is
// present (sanity check: we ARE using the SDK).
//
// This is the conventions § 9.y enforcement test (plan-5 § 3.8 e2e-5).
func TestE2EP5_ImportGraph_NoFeishuSDKLeak(t *testing.T) {
	root := repoRoot(t)
	domainPkgs := []string{
		"./internal/conversation/...",
		"./internal/taskruntime/...",
		"./internal/discussion/...",
		"./internal/workforce/...",
		"./internal/observability/...",
	}
	for _, pkg := range domainPkgs {
		cmd := exec.Command("go", "list", "-deps", pkg)
		cmd.Dir = root
		var out, errb bytes.Buffer
		cmd.Stdout, cmd.Stderr = &out, &errb
		if err := cmd.Run(); err != nil {
			t.Fatalf("go list %s: %v stderr=%s", pkg, err, errb.String())
		}
		if strings.Contains(out.String(), "github.com/larksuite/oapi-sdk-go") {
			lines := strings.Split(out.String(), "\n")
			var leaks []string
			for _, l := range lines {
				if strings.Contains(l, "larksuite") {
					leaks = append(leaks, l)
				}
			}
			t.Fatalf("domain BC %s leaked vendor SDK: %v", pkg, leaks)
		}
	}
	// Sanity: bridge package DOES depend on the SDK.
	cmd := exec.Command("go", "list", "-deps", "./internal/bridge/feishu/client/...")
	cmd.Dir = root
	var out, errb bytes.Buffer
	cmd.Stdout, cmd.Stderr = &out, &errb
	if err := cmd.Run(); err != nil {
		t.Fatalf("go list bridge: %v stderr=%s", err, errb.String())
	}
	if !strings.Contains(out.String(), "github.com/larksuite/oapi-sdk-go") {
		t.Fatal("bridge package no longer imports the SDK — likely a regression")
	}
}

// TestE2EP5_ImportGraph_FeishuSDKConfinedToOneFile greps the internal tree
// and asserts the SDK is *imported* (as a Go statement, not a doc comment)
// from exactly one file: the oapi_adapter.
func TestE2EP5_ImportGraph_FeishuSDKConfinedToOneFile(t *testing.T) {
	root := repoRoot(t)
	// Pattern matches actual Go import statements only — quoted package
	// path. Doc-comment mentions don't have the surrounding quotes.
	cmd := exec.Command("grep", "-rln", `"github.com/larksuite/oapi-sdk-go`, "internal/")
	cmd.Dir = root
	out, _ := cmd.Output()
	files := strings.Split(strings.TrimSpace(string(out)), "\n")
	expected := "internal/bridge/feishu/client/oapi_adapter.go"
	if len(files) != 1 || files[0] != expected {
		t.Fatalf("expected SDK import only in %q, found in: %v", expected, files)
	}
}
