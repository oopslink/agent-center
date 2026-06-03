package cli

import (
	"testing"
)

// v2.7 #162: the secret-helper (secretToMap / validSecretKind / trimTrailingNewline
// / resolveSecretInput) and convShow tests were removed with the retired
// secret/conversation CLI commands. The webconsole-handler + global-config-path
// coverage below is kept.

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
