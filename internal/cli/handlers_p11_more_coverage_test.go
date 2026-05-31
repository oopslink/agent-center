package cli

import (
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/oopslink/agent-center/internal/secretmgmt"
)

// ============================================================================
// projection helpers — secretToMap / valid kind / trim
// ============================================================================

func TestSecretToMap_AllFields(t *testing.T) {
	now := time.Now().UTC()
	sec, err := secretmgmt.NewUserSecret(secretmgmt.NewUserSecretInput{
		ID: "S-1", Name: "x", Kind: secretmgmt.UserSecretKindOther,
		Ciphertext: []byte{1, 2, 3}, Nonce: []byte{4, 5, 6},
		CreatedAt: now, CreatedBy: "user:hayang",
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := sec.Revoke(now, "user:hayang", secretmgmt.UserSecretRevokedReasonManual, "done"); err != nil {
		t.Fatal(err)
	}
	m := secretToMap(sec)
	if m["state"] != "revoked" || m["revoked_by"] != "user:hayang" {
		t.Fatalf("got %v", m)
	}
}

func TestSecretToMap_Active(t *testing.T) {
	now := time.Now().UTC()
	sec, _ := secretmgmt.NewUserSecret(secretmgmt.NewUserSecretInput{
		ID: "S-2", Name: "y", Kind: secretmgmt.UserSecretKindMCP,
		Ciphertext: []byte{1}, Nonce: []byte{2},
		CreatedAt: now, CreatedBy: "user:hayang",
	})
	m := secretToMap(sec)
	if _, ok := m["revoked_at"]; ok {
		t.Fatalf("active secret should not show revoked_at: %v", m)
	}
}

func TestValidSecretKind(t *testing.T) {
	for _, k := range []secretmgmt.UserSecretKind{
		secretmgmt.UserSecretKindMCP, secretmgmt.UserSecretKindCloudCredential,
		secretmgmt.UserSecretKindRepoDeployKey, secretmgmt.UserSecretKindOther,
	} {
		if !validSecretKind(k) {
			t.Fatalf("%s should be valid", k)
		}
	}
	if validSecretKind("weird") {
		t.Fatal()
	}
}

func TestTrimTrailingNewline(t *testing.T) {
	if string(trimTrailingNewline([]byte("hello\n"))) != "hello" {
		t.Fatal()
	}
	if string(trimTrailingNewline([]byte("hello\n\n\n"))) != "hello" {
		t.Fatal()
	}
	if string(trimTrailingNewline([]byte("no-newline"))) != "no-newline" {
		t.Fatal()
	}
}

// ============================================================================
// resolveSecretInput — file paths
// ============================================================================

func TestResolveSecretInput_FromFile(t *testing.T) {
	tmp := writeTempFile(t, "deadbeef\n")
	got, err := resolveSecretInput(tmp)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != "deadbeef" {
		t.Fatalf("got %q", got)
	}
}

func TestResolveSecretInput_EmptyFile(t *testing.T) {
	tmp := writeTempFile(t, "")
	_, err := resolveSecretInput(tmp)
	if err == nil {
		t.Fatal()
	}
}

func TestResolveSecretInput_FileNotFound(t *testing.T) {
	_, err := resolveSecretInput("/no/such/file/asdf")
	if err == nil {
		t.Fatal()
	}
}

// ============================================================================
// convShowHandler — happy path
// ============================================================================

func TestCLI_ConvShow_Happy(t *testing.T) {
	app := newTestApp(t)
	_, _, _ = runOn(t, app, "channel", "create", []string{"--name=showme"})
	out, _, _ := runOn(t, app, "channel", "show", []string{"showme", "--format=json"})
	var m map[string]any
	_ = json.Unmarshal([]byte(strings.TrimSpace(out)), &m)
	cid := m["conversation_id"].(string)
	out2, _, code := runOn(t, app, "conversation", "show", []string{cid})
	if code != ExitOK {
		t.Fatalf("code %d", code)
	}
	if !strings.Contains(out2, "showme") {
		t.Fatalf("got %s", out2)
	}
}

func TestCLI_ConvShow_JSON(t *testing.T) {
	app := newTestApp(t)
	_, _, _ = runOn(t, app, "channel", "create", []string{"--name=showmejson"})
	out, _, _ := runOn(t, app, "channel", "show", []string{"showmejson", "--format=json"})
	var m map[string]any
	_ = json.Unmarshal([]byte(strings.TrimSpace(out)), &m)
	cid := m["conversation_id"].(string)
	out2, _, _ := runOn(t, app, "conversation", "show", []string{cid, "--format=json"})
	var m2 map[string]any
	_ = json.Unmarshal([]byte(strings.TrimSpace(out2)), &m2)
	if m2["name"] != "showmejson" {
		t.Fatalf("got %v", m2)
	}
}

// ============================================================================
// webconsole_wiring buildWebConsoleHandler
// ============================================================================

func TestBuildWebConsoleHandler_Happy(t *testing.T) {
	app := newTestApp(t)
	h := buildWebConsoleHandler(app, nil)
	if h == nil {
		t.Fatal()
	}
}

func TestBuildWebConsoleHandler_NilApp(t *testing.T) {
	if buildWebConsoleHandler(nil, nil) != nil {
		t.Fatal()
	}
}

// ============================================================================
// GlobalConfigPath roundtrip
// ============================================================================

func TestGlobalConfigPath_RoundTrip(t *testing.T) {
	SetGlobalConfigPath("/tmp/x")
	defer SetGlobalConfigPath("")
	if GlobalConfigPath() != "/tmp/x" {
		t.Fatal()
	}
}
