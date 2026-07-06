package agentruntime

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// stubResolver returns a fixed value or error per secret name.
type stubResolver struct {
	values map[string]string
	err    error
}

func (s *stubResolver) Resolve(_ context.Context, name string) ([]byte, error) {
	if s.err != nil {
		return nil, s.err
	}
	if v, ok := s.values[name]; ok {
		return []byte(v), nil
	}
	return nil, errors.New("stub: secret not found: " + name)
}

func writeTemplate(t *testing.T, homeDir string, body string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(homeDir, "mcp_config.json"), []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
}

func TestMCPInject_NoTemplate_Noop(t *testing.T) {
	dir := t.TempDir() // empty home_dir
	inj := NewMCPInjector(nil)
	path, cleanup, err := inj.Inject(context.Background(), dir)
	if err != nil {
		t.Fatal(err)
	}
	if path != "" {
		t.Fatalf("expected empty path, got %s", path)
	}
	if cleanup == nil {
		t.Fatal("cleanup should never be nil")
	}
	cleanup() // should not panic
}

func TestMCPInject_HappyPath_ReplacesSecretRefs(t *testing.T) {
	dir := t.TempDir()
	writeTemplate(t, dir, `{
		"mcpServers": {
			"github": {
				"command": "npx",
				"env": {
					"GITHUB_TOKEN": "secret:github-pat"
				}
			},
			"db": {
				"command": "db-mcp",
				"env": {
					"DB_PASSWORD": "secret:db-pwd"
				}
			}
		}
	}`)
	inj := NewMCPInjector(&stubResolver{values: map[string]string{
		"github-pat": "ghp_real_token_secret",
		"db-pwd":     "supersecret123",
	}})
	path, cleanup, err := inj.Inject(context.Background(), dir)
	if err != nil {
		t.Fatalf("Inject: %v", err)
	}
	if path != filepath.Join(dir, "mcp_config.runtime.json") {
		t.Fatalf("path: %s", path)
	}
	t.Cleanup(cleanup)
	// Permission check: file should be 0600.
	info, _ := os.Stat(path)
	if info.Mode().Perm() != 0o600 {
		t.Fatalf("expected 0600, got %o", info.Mode().Perm())
	}
	// Content check: SecretRefs replaced with plaintext.
	raw, _ := os.ReadFile(path)
	if !strings.Contains(string(raw), "ghp_real_token_secret") {
		t.Fatalf("github-pat plaintext missing: %s", raw)
	}
	if !strings.Contains(string(raw), "supersecret123") {
		t.Fatalf("db-pwd plaintext missing: %s", raw)
	}
	if strings.Contains(string(raw), "secret:") {
		t.Fatal("SecretRef token leaked into runtime.json")
	}
}

func TestMCPInject_Cleanup_RemovesFile(t *testing.T) {
	dir := t.TempDir()
	writeTemplate(t, dir, `{"env":{"K":"secret:x"}}`)
	inj := NewMCPInjector(&stubResolver{values: map[string]string{"x": "v"}})
	path, cleanup, err := inj.Inject(context.Background(), dir)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(path); err != nil {
		t.Fatalf("file should exist: %v", err)
	}
	cleanup()
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Fatalf("expected file removed, got %v", err)
	}
	// Calling cleanup again must not panic.
	cleanup()
}

func TestMCPInject_ResolveError_Propagates(t *testing.T) {
	dir := t.TempDir()
	writeTemplate(t, dir, `{"env":{"K":"secret:missing"}}`)
	inj := NewMCPInjector(&stubResolver{values: map[string]string{}})
	_, _, err := inj.Inject(context.Background(), dir)
	if err == nil {
		t.Fatal("expected resolve error")
	}
	var miErr *MCPInjectError
	if !errors.As(err, &miErr) {
		t.Fatalf("expected *MCPInjectError, got %T", err)
	}
	if miErr.SecretName != "missing" {
		t.Fatalf("secret name: %s", miErr.SecretName)
	}
}

func TestMCPInject_NoResolverWired_Errors(t *testing.T) {
	dir := t.TempDir()
	writeTemplate(t, dir, `{"env":{"K":"secret:x"}}`)
	inj := NewMCPInjector(nil)
	_, _, err := inj.Inject(context.Background(), dir)
	if err == nil {
		t.Fatal("expected no-resolver error")
	}
}

func TestMCPInject_BadJSON_Errors(t *testing.T) {
	dir := t.TempDir()
	writeTemplate(t, dir, `not json`)
	inj := NewMCPInjector(&stubResolver{})
	_, _, err := inj.Inject(context.Background(), dir)
	if err == nil {
		t.Fatal()
	}
}

func TestMCPInject_NoSecrets_OnlyCopy(t *testing.T) {
	dir := t.TempDir()
	writeTemplate(t, dir, `{"mcpServers":{"a":{"command":"x"}}}`)
	inj := NewMCPInjector(nil) // no secrets needed
	path, cleanup, err := inj.Inject(context.Background(), dir)
	if err != nil {
		t.Fatalf("Inject: %v", err)
	}
	t.Cleanup(cleanup)
	var doc map[string]any
	raw, _ := os.ReadFile(path)
	if err := json.Unmarshal(raw, &doc); err != nil {
		t.Fatal(err)
	}
	if _, ok := doc["mcpServers"]; !ok {
		t.Fatal("mcpServers missing")
	}
}

func TestMCPInject_EmptyHomeDir(t *testing.T) {
	_, _, err := NewMCPInjector(nil).Inject(context.Background(), "")
	if err == nil {
		t.Fatal()
	}
}

func TestMCPInject_SecretRefInNestedArray(t *testing.T) {
	dir := t.TempDir()
	writeTemplate(t, dir, `{"servers":[{"env":{"K":"secret:s"}}]}`)
	inj := NewMCPInjector(&stubResolver{values: map[string]string{"s": "val"}})
	path, cleanup, err := inj.Inject(context.Background(), dir)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(cleanup)
	raw, _ := os.ReadFile(path)
	if !strings.Contains(string(raw), "val") {
		t.Fatalf("array nested secret missing: %s", raw)
	}
}

// =============================================================================
// CrashRecoveryScan
// =============================================================================

func TestCrashRecoveryScan_RemovesInactiveRuntimeJSON(t *testing.T) {
	root := t.TempDir()
	// Create three agent home_dirs; first has active execution, others don't.
	for _, id := range []string{"01HACTIVE", "01HSTALE", "01HALSO_STALE"} {
		d := filepath.Join(root, id)
		_ = os.MkdirAll(d, 0o700)
		_ = os.WriteFile(filepath.Join(d, "mcp_config.runtime.json"), []byte("{}"), 0o600)
	}
	active := map[string]bool{"01HACTIVE": true}
	removed, err := CrashRecoveryScan(root, active)
	if err != nil {
		t.Fatal(err)
	}
	if removed != 2 {
		t.Fatalf("expected 2 removed, got %d", removed)
	}
	// Active one's file still present.
	if _, err := os.Stat(filepath.Join(root, "01HACTIVE", "mcp_config.runtime.json")); err != nil {
		t.Fatalf("active agent's file should remain: %v", err)
	}
	if _, err := os.Stat(filepath.Join(root, "01HSTALE", "mcp_config.runtime.json")); !os.IsNotExist(err) {
		t.Fatalf("stale file should be gone: %v", err)
	}
}

func TestCrashRecoveryScan_NoRuntimeJSON_Skips(t *testing.T) {
	root := t.TempDir()
	_ = os.MkdirAll(filepath.Join(root, "01HNORTM"), 0o700) // no runtime.json
	removed, err := CrashRecoveryScan(root, map[string]bool{})
	if err != nil {
		t.Fatal(err)
	}
	if removed != 0 {
		t.Fatalf("removed: %d", removed)
	}
}

func TestCrashRecoveryScan_NonexistentRoot_Quietly(t *testing.T) {
	removed, err := CrashRecoveryScan(filepath.Join(t.TempDir(), "nonexistent"), nil)
	if err != nil {
		t.Fatal(err)
	}
	if removed != 0 {
		t.Fatal()
	}
}

func TestCrashRecoveryScan_EmptyRoot(t *testing.T) {
	_, err := CrashRecoveryScan("", nil)
	if err == nil {
		t.Fatal()
	}
}

// MCPInjectError implements error + Unwrap.
func TestMCPInjectError_ErrorAndUnwrap(t *testing.T) {
	base := errors.New("boom")
	e := &MCPInjectError{SecretName: "x", Cause: base}
	if !strings.Contains(e.Error(), "secret \"x\"") {
		t.Fatalf("Error: %s", e.Error())
	}
	if !errors.Is(e, base) {
		t.Fatal("Unwrap should expose Cause for errors.Is")
	}
}

// Template file read error (permission denied) propagates as non-IsNotExist.
func TestMCPInject_TemplateReadError(t *testing.T) {
	if os.Getuid() == 0 {
		t.Skip("running as root")
	}
	dir := t.TempDir()
	path := filepath.Join(dir, "mcp_config.json")
	if err := os.WriteFile(path, []byte("{}"), 0o600); err != nil {
		t.Fatal(err)
	}
	_ = os.Chmod(path, 0o000)
	t.Cleanup(func() { _ = os.Chmod(path, 0o600) })
	_, _, err := NewMCPInjector(nil).Inject(context.Background(), dir)
	if err == nil {
		t.Fatal("expected read error")
	}
}

func TestCrashRecoveryScan_IgnoresFiles(t *testing.T) {
	root := t.TempDir()
	_ = os.WriteFile(filepath.Join(root, "not-a-dir.txt"), []byte("x"), 0o600)
	removed, err := CrashRecoveryScan(root, nil)
	if err != nil {
		t.Fatal(err)
	}
	if removed != 0 {
		t.Fatal()
	}
}
