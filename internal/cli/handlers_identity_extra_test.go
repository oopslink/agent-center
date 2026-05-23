package cli

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/oopslink/agent-center/internal/conversation/identity"
)

// JSON output paths for identity add / list (bind / unbind removed in
// P10 § 3.9 + § 3.2 per ADR-0033).
func TestIdentityJSONOutputPaths(t *testing.T) {
	app := newTestApp(t)
	stdout, _, code := runIdentity(t, app, "add", []string{
		"user:hayang", "--kind=user", "--display-name=H", "--format=json",
	})
	if code != ExitOK {
		t.Fatalf("add json: code=%d stdout=%s", code, stdout)
	}
	var m map[string]any
	if err := json.Unmarshal([]byte(strings.TrimSpace(stdout)), &m); err != nil {
		t.Fatalf("add json: %v stdout=%s", err, stdout)
	}
	if m["identity_id"] != "user:hayang" || m["kind"] != "user" {
		t.Fatalf("payload: %v", m)
	}
	stdout, _, code = runIdentity(t, app, "list", []string{"--format=json"})
	if code != ExitOK {
		t.Fatalf("list json: %d", code)
	}
	if !strings.Contains(stdout, "user:hayang") {
		t.Fatalf("list payload: %s", stdout)
	}
}

// Cover the "without IdentityRegistration service wired" error branch.
func TestIdentityHandlersWithoutService(t *testing.T) {
	app := newTestApp(t)
	app.IdentityRegistration = nil
	if _, _, code := runIdentity(t, app, "add", []string{"user:x", "--kind=user", "--display-name=x"}); code != ExitNotImplemented {
		t.Fatalf("add no-svc: %d", code)
	}
}

// handleIdentityError matrix (v2 — ChannelBinding errors removed).
func TestHandleIdentityErrorMatrix(t *testing.T) {
	t.Parallel()
	for err, want := range map[error]string{
		identity.ErrIdentityNotFound:        "identity_not_found",
		identity.ErrIdentityAlreadyExists:   "identity_already_exists",
		identity.ErrIdentityVersionConflict: "identity_version_conflict",
		identity.ErrIdentityInvalidKind:     "identity_invalid_kind",
		identity.ErrIdentityKindImmutable:   "identity_kind_immutable",
	} {
		var buf strings.Builder
		handleIdentityError(&buf, "json", err)
		if !strings.Contains(buf.String(), want) {
			t.Errorf("err %v: missing reason %s in %s", err, want, buf.String())
		}
	}
}
