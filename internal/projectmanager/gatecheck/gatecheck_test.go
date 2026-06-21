package gatecheck

import (
	"context"
	"errors"
	"os/exec"
	"strings"
	"testing"

	pm "github.com/oopslink/agent-center/internal/projectmanager"
)

// realExitError returns a genuine *exec.ExitError (exit code 1) so the verdict
// mapping (non-zero process exit ⇒ GateRed) is exercised exactly as in production.
func realExitError(t *testing.T) error {
	t.Helper()
	err := exec.Command("sh", "-c", "exit 1").Run()
	if _, ok := err.(*exec.ExitError); !ok {
		t.Fatalf("expected *exec.ExitError, got %T (%v)", err, err)
	}
	return err
}

// fakeRunner records calls; git ops succeed, and the gate command returns gateErr.
// checkoutErr (when set) makes the `git checkout` call fail.
type fakeRunner struct {
	calls       []string // "name arg arg ..." per call
	gateErr     error
	checkoutErr error
}

func (f *fakeRunner) Run(_ context.Context, _ string, _ []string, name string, args ...string) (string, error) {
	f.calls = append(f.calls, strings.Join(append([]string{name}, args...), " "))
	if name == "git" {
		if len(args) > 0 && args[0] == "checkout" && f.checkoutErr != nil {
			return "fatal: checkout failed", f.checkoutErr
		}
		return "", nil // clone / fetch / checkout succeed
	}
	return "gate output", f.gateErr // the gate command
}

func TestGateStatus_Green(t *testing.T) {
	f := &fakeRunner{gateErr: nil}
	c := New(t.TempDir(), []string{"make", "gate"}, f)
	v, err := c.GateStatus(context.Background(), "git@example.com:o/r.git", "feat", "dev/v1")
	if err != nil || v != pm.GateGreen {
		t.Fatalf("got (%v, %v); want (green, nil)", v, err)
	}
	// The gate command must actually have run after a git checkout of the branch.
	joined := strings.Join(f.calls, "\n")
	if !strings.Contains(joined, "git checkout --detach origin/feat") {
		t.Errorf("expected checkout of origin/feat, calls:\n%s", joined)
	}
	if !strings.Contains(joined, "make gate") {
		t.Errorf("expected the gate command to run, calls:\n%s", joined)
	}
}

func TestGateStatus_Red(t *testing.T) {
	f := &fakeRunner{gateErr: realExitError(t)}
	c := New(t.TempDir(), []string{"make", "gate"}, f)
	v, err := c.GateStatus(context.Background(), "git@example.com:o/r.git", "feat", "dev/v1")
	if err != nil || v != pm.GateRed {
		t.Fatalf("got (%v, %v); want (red, nil)", v, err)
	}
}

func TestGateStatus_Unknown_GateCantRun(t *testing.T) {
	// A non-ExitError (e.g. command not found) ⇒ couldn't run the gate ⇒ unknown+err.
	f := &fakeRunner{gateErr: errors.New("exec: \"make\": executable file not found in $PATH")}
	c := New(t.TempDir(), []string{"make", "gate"}, f)
	v, err := c.GateStatus(context.Background(), "git@example.com:o/r.git", "feat", "dev/v1")
	if v != pm.GateUnknown || err == nil {
		t.Fatalf("got (%v, %v); want (unknown, err)", v, err)
	}
}

func TestGateStatus_Unknown_CheckoutFails(t *testing.T) {
	f := &fakeRunner{checkoutErr: realExitError(t)}
	c := New(t.TempDir(), []string{"make", "gate"}, f)
	v, err := c.GateStatus(context.Background(), "git@example.com:o/r.git", "feat", "dev/v1")
	if v != pm.GateUnknown || err == nil {
		t.Fatalf("got (%v, %v); want (unknown, err)", v, err)
	}
	// The gate command must NOT run when checkout failed.
	if strings.Contains(strings.Join(f.calls, "\n"), "make gate") {
		t.Errorf("gate must not run after a failed checkout")
	}
}

func TestGateStatus_RejectsEmptyArgs(t *testing.T) {
	c := New(t.TempDir(), nil, &fakeRunner{})
	if _, err := c.GateStatus(context.Background(), "", "feat", "base"); err == nil {
		t.Error("empty repoURL must error")
	}
	if _, err := c.GateStatus(context.Background(), "url", "", "base"); err == nil {
		t.Error("empty branch must error")
	}
}

func TestNew_DefaultsGateCommand(t *testing.T) {
	c := New(t.TempDir(), nil, &fakeRunner{})
	if len(c.gateCmd) != 2 || c.gateCmd[0] != "make" || c.gateCmd[1] != "gate" {
		t.Fatalf("default gate command = %v; want [make gate]", c.gateCmd)
	}
}
