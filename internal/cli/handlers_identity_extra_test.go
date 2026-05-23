package cli

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/oopslink/agent-center/internal/conversation/identity"
)

// JSON output paths for bind / unbind / list happy.
func TestIdentityJSONOutputPaths(t *testing.T) {
	app := newTestApp(t)
	if _, _, code := runIdentity(t, app, "add", []string{
		"user:hayang", "--kind=user", "--display-name=H", "--format=json",
	}); code != ExitOK {
		t.Fatal("add json")
	}
	stdout, _, code := runIdentity(t, app, "bind", []string{
		"user:hayang", "--channel=feishu", "--vendor-user-id=ou_j", "--format=json",
	})
	if code != ExitOK {
		t.Fatalf("bind exit=%d", code)
	}
	var m map[string]any
	if err := json.Unmarshal([]byte(strings.TrimSpace(stdout)), &m); err != nil {
		t.Fatalf("bind json: %v stdout=%s", err, stdout)
	}
	if m["channel"] != "feishu" {
		t.Fatalf("payload: %v", m)
	}
	stdout, _, code = runIdentity(t, app, "unbind", []string{
		"user:hayang", "--channel=feishu", "--format=json",
	})
	if code != ExitOK {
		t.Fatalf("unbind json exit=%d", code)
	}
	if !strings.Contains(stdout, "feishu") {
		t.Fatalf("unbind json: %s", stdout)
	}
}

// Cover the "without IdentityRegistration service wired" error branches.
func TestIdentityHandlersWithoutService(t *testing.T) {
	app := newTestApp(t)
	app.IdentityRegistration = nil
	if _, _, code := runIdentity(t, app, "add", []string{"user:x", "--kind=user", "--display-name=x"}); code != ExitNotImplemented {
		t.Fatalf("add no-svc: %d", code)
	}
	if _, _, code := runIdentity(t, app, "bind", []string{"user:x", "--channel=feishu", "--vendor-user-id=ou"}); code != ExitNotImplemented {
		t.Fatalf("bind no-svc: %d", code)
	}
	if _, _, code := runIdentity(t, app, "unbind", []string{"user:x", "--channel=feishu"}); code != ExitNotImplemented {
		t.Fatalf("unbind no-svc: %d", code)
	}
}

// handleIdentityError matrix.
func TestHandleIdentityErrorMatrix(t *testing.T) {
	t.Parallel()
	for err, want := range map[error]string{
		identity.ErrIdentityNotFound:              "identity_not_found",
		identity.ErrIdentityAlreadyExists:         "identity_already_exists",
		identity.ErrIdentityVersionConflict:       "identity_version_conflict",
		identity.ErrIdentityInvalidKind:           "identity_invalid_kind",
		identity.ErrIdentityKindImmutable:         "identity_kind_immutable",
		identity.ErrChannelBindingNotFound:        "channel_binding_not_found",
		identity.ErrChannelBindingAlreadyExists:   "channel_binding_already_exists",
		identity.ErrChannelBindingPreferredConflict: "channel_binding_preferred_conflict",
	} {
		var buf strings.Builder
		handleIdentityError(&buf, "json", err)
		if !strings.Contains(buf.String(), want) {
			t.Errorf("err %v: missing reason %s in %s", err, want, buf.String())
		}
	}
}

// Bind preferred-conflict surfaces the right reason.
func TestIdentityBindPreferredConflict(t *testing.T) {
	app := newTestApp(t)
	_, _, _ = runIdentity(t, app, "add", []string{"user:x", "--kind=user", "--display-name=x"})
	_, _, _ = runIdentity(t, app, "bind", []string{"user:x", "--channel=feishu", "--vendor-user-id=ou_a", "--preferred"})
	_, stderr, code := runIdentity(t, app, "bind", []string{"user:x", "--channel=feishu", "--vendor-user-id=ou_b", "--preferred"})
	if code != ExitInvariantViolation {
		t.Fatalf("exit %d stderr=%s", code, stderr)
	}
	if !strings.Contains(stderr, "channel_binding_preferred_conflict") {
		t.Fatalf("stderr: %s", stderr)
	}
}

