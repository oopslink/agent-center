package cli

import (
	"bytes"
	"context"
	"flag"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func runBootstrapCheckSystemd(t *testing.T, unit string) (ExitCode, string, string) {
	t.Helper()
	cmd := BootstrapCommand()
	check := findCmd(cmd.Subcommands, "check-systemd")
	if check == nil {
		t.Fatal("check-systemd command missing")
	}
	fs := flag.NewFlagSet("check-systemd", flag.ContinueOnError)
	h := check.Flags(fs)
	if err := fs.Parse([]string{"--unit", unit}); err != nil {
		t.Fatal(err)
	}
	var out, errw bytes.Buffer
	code := h(context.Background(), fs.Args(), &out, &errw)
	return code, out.String(), errw.String()
}

func writeUnit(t *testing.T, body string) string {
	t.Helper()
	dir := t.TempDir()
	p := filepath.Join(dir, "agent-center-worker.service")
	if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	return p
}

func TestBootstrap_KillModeProcessPresent(t *testing.T) {
	body := `[Unit]
Description=worker
[Service]
Type=simple
ExecStart=/bin/true
KillMode=process

[Install]
WantedBy=default.target
`
	p := writeUnit(t, body)
	code, out, _ := runBootstrapCheckSystemd(t, p)
	if code != ExitOK {
		t.Errorf("code: %d", code)
	}
	if !strings.Contains(out, "ok") {
		t.Errorf("out: %q", out)
	}
}

func TestBootstrap_KillModeMissing(t *testing.T) {
	body := `[Unit]
Description=worker
[Service]
Type=simple
ExecStart=/bin/true

[Install]
WantedBy=default.target
`
	p := writeUnit(t, body)
	code, _, errw := runBootstrapCheckSystemd(t, p)
	if code != ExitInvariantViolation {
		t.Errorf("code: %d", code)
	}
	if !strings.Contains(errw, "kill_mode_missing") {
		t.Errorf("err: %q", errw)
	}
}

func TestBootstrap_KillModeWrongValue(t *testing.T) {
	body := `[Service]
KillMode=control-group
`
	p := writeUnit(t, body)
	code, _, errw := runBootstrapCheckSystemd(t, p)
	if code != ExitInvariantViolation {
		t.Errorf("code: %d", code)
	}
	if !strings.Contains(errw, "kill_mode_missing") {
		t.Errorf("err: %q", errw)
	}
}

func TestBootstrap_KillModeInWrongSection(t *testing.T) {
	body := `[Unit]
KillMode=process
[Service]
Type=simple
`
	p := writeUnit(t, body)
	code, _, _ := runBootstrapCheckSystemd(t, p)
	if code != ExitInvariantViolation {
		t.Errorf("KillMode in [Unit] should not satisfy: %d", code)
	}
}

func TestBootstrap_KillModeWithSpaces(t *testing.T) {
	body := `[Service]
 KillMode = process
`
	p := writeUnit(t, body)
	code, _, _ := runBootstrapCheckSystemd(t, p)
	if code != ExitOK {
		t.Errorf("liberal whitespace should still pass: %d", code)
	}
}

func TestBootstrap_FileMissing(t *testing.T) {
	code, _, errw := runBootstrapCheckSystemd(t, "/does/not/exist/agent-center-worker.service")
	if code != ExitUsage {
		t.Errorf("code: %d", code)
	}
	if !strings.Contains(errw, "unit_not_found") {
		t.Errorf("err: %q", errw)
	}
}

func TestBootstrap_DefaultUnitPathFromHome(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("HOME", dir)
	// File missing → unit_not_found at default path.
	cmd := BootstrapCommand()
	check := findCmd(cmd.Subcommands, "check-systemd")
	fs := flag.NewFlagSet("check-systemd", flag.ContinueOnError)
	h := check.Flags(fs)
	if err := fs.Parse([]string{}); err != nil {
		t.Fatal(err)
	}
	var out, errw bytes.Buffer
	code := h(context.Background(), nil, &out, &errw)
	if code != ExitUsage {
		t.Errorf("code: %d", code)
	}
}

func TestBootstrap_NoHomeNoUnit(t *testing.T) {
	t.Setenv("HOME", "")
	cmd := BootstrapCommand()
	check := findCmd(cmd.Subcommands, "check-systemd")
	fs := flag.NewFlagSet("check-systemd", flag.ContinueOnError)
	h := check.Flags(fs)
	_ = fs.Parse([]string{})
	var out, errw bytes.Buffer
	code := h(context.Background(), nil, &out, &errw)
	if code != ExitUsage {
		t.Errorf("code: %d", code)
	}
	if !strings.Contains(errw.String(), "no_home") {
		t.Errorf("err: %q", errw.String())
	}
}

func TestBootstrap_JSONFormat(t *testing.T) {
	body := `[Service]
KillMode=process
`
	p := writeUnit(t, body)
	cmd := BootstrapCommand()
	check := findCmd(cmd.Subcommands, "check-systemd")
	fs := flag.NewFlagSet("check-systemd", flag.ContinueOnError)
	h := check.Flags(fs)
	if err := fs.Parse([]string{"--unit", p, "--format", "json"}); err != nil {
		t.Fatal(err)
	}
	var out, errw bytes.Buffer
	code := h(context.Background(), nil, &out, &errw)
	if code != ExitOK {
		t.Fatalf("code: %d errw=%q", code, errw.String())
	}
	if !strings.Contains(out.String(), `"kill_mode_process":true`) {
		t.Errorf("out: %q", out.String())
	}
}
