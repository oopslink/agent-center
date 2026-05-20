package config

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func writeYAML(t *testing.T, content string) string {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, "config.yaml")
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
	return path
}

func TestLoad_DefaultsWhenNoPath(t *testing.T) {
	cfg, err := Load(LoadOptions{})
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if cfg.Server.ListenAddr != ":7000" {
		t.Fatalf("default listen_addr: got %q", cfg.Server.ListenAddr)
	}
	if cfg.Identity.DefaultUser != "hayang" {
		t.Fatalf("default user: got %q", cfg.Identity.DefaultUser)
	}
}

func TestLoad_FromYAML(t *testing.T) {
	path := writeYAML(t, `
server:
  listen_addr: ":8888"
  sqlite_path: "/tmp/x.db"
  admin_socket_path: "/tmp/x.sock"
notification:
  default_channel: "feishu:user:hayang:dm"
identity:
  default_user: "alice"
`)
	cfg, err := Load(LoadOptions{Path: path})
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if cfg.Server.ListenAddr != ":8888" {
		t.Fatalf("listen_addr: got %q", cfg.Server.ListenAddr)
	}
	if cfg.Identity.DefaultUser != "alice" {
		t.Fatalf("default_user: got %q", cfg.Identity.DefaultUser)
	}
}

func TestLoad_UnknownYAMLKeyRejected(t *testing.T) {
	path := writeYAML(t, `
server:
  listen_addr: ":7000"
  sqlite_path: "/tmp/x.db"
unknown_section:
  foo: bar
`)
	_, err := Load(LoadOptions{Path: path})
	if err == nil {
		t.Fatal("expected error for unknown YAML key")
	}
	if !strings.Contains(err.Error(), "unknown_section") {
		t.Fatalf("error missing key name: %v", err)
	}
}

func TestLoad_UnknownYAMLKeyDidYouMean(t *testing.T) {
	path := writeYAML(t, `
server:
  listn_addr: ":7000"
`)
	_, err := Load(LoadOptions{Path: path})
	if err == nil {
		t.Fatal("expected error for typo")
	}
	if !strings.Contains(err.Error(), "did you mean") {
		t.Fatalf("error missing did-you-mean hint: %v", err)
	}
}

func TestLoad_EnvOverridesYAML(t *testing.T) {
	path := writeYAML(t, `
server:
  listen_addr: ":7000"
  sqlite_path: "/tmp/x.db"
`)
	envFn := func(k string) (string, bool) {
		if k == "AGENT_CENTER_SERVER_LISTEN_ADDR" {
			return ":9999", true
		}
		return "", false
	}
	cfg, err := Load(LoadOptions{Path: path, Env: envFn})
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if cfg.Server.ListenAddr != ":9999" {
		t.Fatalf("env override: got %q want :9999", cfg.Server.ListenAddr)
	}
}

func TestLoad_FlagOverridesEnv(t *testing.T) {
	envFn := func(k string) (string, bool) {
		if k == "AGENT_CENTER_SERVER_LISTEN_ADDR" {
			return ":1111", true
		}
		return "", false
	}
	cfg, err := Load(LoadOptions{
		Env:           envFn,
		FlagOverrides: map[string]string{"server.listen_addr": ":2222"},
	})
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if cfg.Server.ListenAddr != ":2222" {
		t.Fatalf("flag override: got %q", cfg.Server.ListenAddr)
	}
}

func TestLoad_RejectsMissingRequired(t *testing.T) {
	path := writeYAML(t, `
server:
  listen_addr: ""
`)
	_, err := Load(LoadOptions{Path: path})
	if err == nil {
		t.Fatal("expected error for empty required field")
	}
	if !strings.Contains(err.Error(), "listen_addr") {
		t.Fatalf("error missing listen_addr: %v", err)
	}
}

func TestLoad_RejectsBadIdentity(t *testing.T) {
	path := writeYAML(t, `
identity:
  default_user: "bad user"
`)
	_, err := Load(LoadOptions{Path: path})
	if err == nil {
		t.Fatal("expected error for invalid user")
	}
}

func TestLoad_RejectsNonexistentPath(t *testing.T) {
	_, err := Load(LoadOptions{Path: "/nonexistent/x.yaml"})
	if err == nil {
		t.Fatal("expected error for nonexistent path")
	}
}

func TestLoad_RejectsMalformedYAML(t *testing.T) {
	path := writeYAML(t, `: : :`)
	_, err := Load(LoadOptions{Path: path})
	if err == nil {
		t.Fatal("expected error for malformed YAML")
	}
}

func TestLoad_RejectsNonMappingTop(t *testing.T) {
	path := writeYAML(t, `["a", "b"]`)
	_, err := Load(LoadOptions{Path: path})
	if err == nil {
		t.Fatal("expected error for non-mapping top")
	}
}

func TestLoad_RejectsUnknownFlag(t *testing.T) {
	_, err := Load(LoadOptions{
		FlagOverrides: map[string]string{"server.bogus": "x"},
	})
	if err == nil {
		t.Fatal("expected error for unknown flag override")
	}
}

func TestConfigError_Format(t *testing.T) {
	e := &ConfigError{Reasons: []string{"r1", "r2"}}
	s := e.Error()
	if !strings.Contains(s, "r1") || !strings.Contains(s, "r2") {
		t.Fatalf("Error() missing reasons: %s", s)
	}
}

func TestConfigError_EmptyReasons(t *testing.T) {
	e := &ConfigError{}
	if e.Error() == "" {
		t.Fatal("empty Error()")
	}
}

func TestAsErrorList(t *testing.T) {
	ce := &ConfigError{Reasons: []string{"a", "b"}}
	got := AsErrorList(ce)
	if len(got) != 2 {
		t.Fatalf("got %d reasons", len(got))
	}
	got = AsErrorList(errors.New("plain"))
	if len(got) != 1 || got[0] != "plain" {
		t.Fatalf("plain error: got %v", got)
	}
	if AsErrorList(nil) != nil {
		t.Fatal("expected nil for nil error")
	}
}

func TestLevenshtein(t *testing.T) {
	cases := []struct {
		a, b string
		d    int
	}{
		{"", "", 0},
		{"a", "", 1},
		{"", "abc", 3},
		{"kitten", "sitting", 3},
		{"abc", "abc", 0},
	}
	for _, c := range cases {
		if got := levenshtein(c.a, c.b); got != c.d {
			t.Fatalf("levenshtein(%q,%q)=%d want %d", c.a, c.b, got, c.d)
		}
	}
}

func TestParsePort(t *testing.T) {
	if ParsePort(":7000") != 7000 {
		t.Fatal("ParsePort :7000")
	}
	if ParsePort("bogus") != -1 {
		t.Fatal("ParsePort bogus")
	}
	if ParsePort(":abc") != -1 {
		t.Fatal("ParsePort :abc")
	}
}

func TestDefaultConfig(t *testing.T) {
	cfg := DefaultConfig()
	if cfg.Server.ListenAddr == "" {
		t.Fatal("DefaultConfig.Server.ListenAddr empty")
	}
}

func TestApplyEnvOverrides_AllKnownKeys(t *testing.T) {
	env := map[string]string{
		"AGENT_CENTER_SERVER_LISTEN_ADDR":           ":1111",
		"AGENT_CENTER_SERVER_SQLITE_PATH":           "/p.db",
		"AGENT_CENTER_SERVER_ADMIN_SOCKET_PATH":     "/a.sock",
		"AGENT_CENTER_NOTIFICATION_DEFAULT_CHANNEL": "x:y:z",
		"AGENT_CENTER_IDENTITY_DEFAULT_USER":        "alice",
	}
	cfg, err := Load(LoadOptions{
		Env: func(k string) (string, bool) {
			v, ok := env[k]
			return v, ok
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Server.ListenAddr != ":1111" {
		t.Fatal()
	}
	if cfg.Server.SqlitePath != "/p.db" {
		t.Fatal()
	}
	if cfg.Server.AdminSocketPath != "/a.sock" {
		t.Fatal()
	}
	if cfg.Notification.DefaultChannel != "x:y:z" {
		t.Fatal()
	}
	if cfg.Identity.DefaultUser != "alice" {
		t.Fatal()
	}
}

func TestApplyFlagOverrides_AllKnownKeys(t *testing.T) {
	cfg, err := Load(LoadOptions{
		FlagOverrides: map[string]string{
			"server.listen_addr":    ":5555",
			"server.sqlite_path":    "/x.db",
			"identity.default_user": "bob",
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Server.ListenAddr != ":5555" {
		t.Fatal()
	}
	if cfg.Server.SqlitePath != "/x.db" {
		t.Fatal()
	}
	if cfg.Identity.DefaultUser != "bob" {
		t.Fatal()
	}
}

func TestValidate_AllMissingFields(t *testing.T) {
	cfg := DefaultConfig()
	cfg.Server.ListenAddr = ""
	cfg.Server.SqlitePath = ""
	cfg.Identity.DefaultUser = ""
	err := validate(&cfg)
	if err == nil {
		t.Fatal()
	}
	reasons := AsErrorList(err)
	if len(reasons) < 3 {
		t.Fatalf("expected 3 reasons: %v", reasons)
	}
}

func TestDidYouMean_NoMatch(t *testing.T) {
	// No suggestion when the candidate is too far from any known key.
	out := didYouMean("z", []string{"absolutely_unrelated_long_path"})
	if out != "" {
		t.Fatalf("expected no suggestion, got %s", out)
	}
}

func TestDidYouMean_Empty(t *testing.T) {
	if didYouMean("foo", nil) != "" {
		t.Fatal()
	}
}
