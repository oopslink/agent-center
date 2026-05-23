package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"flag"
	"io"
	"strings"
	"testing"
)

func runIdentity(t *testing.T, app *App, name string, args []string) (string, string, ExitCode) {
	t.Helper()
	cmd := findCmd(app.IdentityCommands(), name)
	if cmd == nil {
		t.Fatalf("unknown identity subcmd: %s", name)
	}
	var outBuf, errBuf bytes.Buffer
	fs := flag.NewFlagSet(cmd.Name, flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	handler := cmd.Flags(fs)
	positionals, err := permissiveParse(fs, args)
	if err != nil {
		errBuf.WriteString("usage: " + err.Error())
		return outBuf.String(), errBuf.String(), ExitUsage
	}
	code := handler(context.Background(), positionals, &outBuf, &errBuf)
	return outBuf.String(), errBuf.String(), code
}

func TestIdentityAddHappyAndDuplicate(t *testing.T) {
	app := newTestApp(t)
	stdout, _, code := runIdentity(t, app, "add", []string{"user:hayang", "--kind=user", "--display-name=Hayang"})
	if code != ExitOK {
		t.Fatalf("exit %d: %s", code, stdout)
	}
	if !strings.Contains(stdout, "user:hayang") {
		t.Fatalf("stdout: %s", stdout)
	}
	// Duplicate
	_, stderr, code := runIdentity(t, app, "add", []string{"user:hayang", "--kind=user", "--display-name=Hayang"})
	if code != ExitBusinessError {
		t.Fatalf("dup exit %d", code)
	}
	if !strings.Contains(stderr, "identity_already_exists") {
		t.Fatalf("stderr: %s", stderr)
	}
}

func TestIdentityAddDerivesKindFromID(t *testing.T) {
	app := newTestApp(t)
	_, _, code := runIdentity(t, app, "add", []string{"agent:s-1", "--display-name=S1"})
	if code != ExitOK {
		t.Fatalf("exit %d", code)
	}
}

func TestIdentityAddRejectsMissingArgs(t *testing.T) {
	app := newTestApp(t)
	_, stderr, code := runIdentity(t, app, "add", nil)
	if code != ExitUsage {
		t.Fatalf("exit %d stderr=%s", code, stderr)
	}
	_, stderr, code = runIdentity(t, app, "add", []string{"user:x"})
	if code != ExitUsage || !strings.Contains(stderr, "display-name") {
		t.Fatalf("exit %d stderr=%s", code, stderr)
	}
	_, _, code = runIdentity(t, app, "add", []string{"user:x", "--kind=bogus", "--display-name=x"})
	if code != ExitUsage {
		t.Fatalf("exit %d", code)
	}
}

func TestIdentityListFilter(t *testing.T) {
	app := newTestApp(t)
	for _, x := range [][]string{
		{"user:hayang", "--kind=user", "--display-name=Hayang"},
		{"agent:s-1", "--kind=agent", "--display-name=S1"},
	} {
		if _, _, code := runIdentity(t, app, "add", x); code != ExitOK {
			t.Fatalf("seed %v code=%d", x, code)
		}
	}
	stdout, _, code := runIdentity(t, app, "list", []string{"--kind=user", "--format=json"})
	if code != ExitOK {
		t.Fatalf("exit %d", code)
	}
	var arr []map[string]any
	if err := json.Unmarshal([]byte(strings.TrimSpace(stdout)), &arr); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if len(arr) != 1 || arr[0]["kind"] != "user" {
		t.Fatalf("unexpected list: %v", arr)
	}
	stdout, _, _ = runIdentity(t, app, "list", nil)
	if !strings.Contains(stdout, "user:hayang") || !strings.Contains(stdout, "agent:s-1") {
		t.Fatalf("human list missing entries: %s", stdout)
	}
	_, _, code = runIdentity(t, app, "list", []string{"--kind=weird"})
	if code != ExitUsage {
		t.Fatalf("bad kind code=%d", code)
	}
}
