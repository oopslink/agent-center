package cli

import (
	"errors"
	"net"
	"os"
	"strings"
	"syscall"
	"testing"
)

// v2.4-D-A6 tests for install error-path mapping + preflight checks.

func TestInstallErrorHint_DiskFull(t *testing.T) {
	friendly, hint := installErrorHint(syscall.ENOSPC, installErrorContext{Prefix: "/var/lib/agent-center"})
	if !strings.Contains(friendly, "Disk full") {
		t.Errorf("friendly = %q", friendly)
	}
	if !strings.Contains(hint, "Free up space") {
		t.Errorf("hint = %q", hint)
	}
}

func TestInstallErrorHint_PermissionDenied_WriteUnit(t *testing.T) {
	friendly, hint := installErrorHint(syscall.EACCES, installErrorContext{
		Operation: "write_unit",
		Path:      "/etc/systemd/system/agent-center.service",
	})
	if !strings.Contains(friendly, "Permission denied") {
		t.Errorf("friendly = %q", friendly)
	}
	if !strings.Contains(hint, "sudo") || !strings.Contains(hint, "--user-mode") {
		t.Errorf("hint missing sudo/user-mode: %q", hint)
	}
}

func TestInstallErrorHint_PermissionDenied_WriteBinary(t *testing.T) {
	friendly, hint := installErrorHint(os.ErrPermission, installErrorContext{
		Operation: "write_binary",
		Path:      "/opt/agent-center/versions/v2.4.0/bin",
	})
	if !strings.Contains(friendly, "Permission denied") {
		t.Errorf("friendly = %q", friendly)
	}
	if !strings.Contains(hint, "--prefix") {
		t.Errorf("hint missing --prefix: %q", hint)
	}
}

func TestInstallErrorHint_PortInUse(t *testing.T) {
	err := errors.New(`listen tcp 127.0.0.1:7100: bind: address already in use`)
	friendly, hint := installErrorHint(err, installErrorContext{
		Operation: "bind_port",
		Port:      "127.0.0.1:7100",
	})
	if !strings.Contains(friendly, "Port 127.0.0.1:7100") {
		t.Errorf("friendly = %q", friendly)
	}
	if !strings.Contains(hint, "--port") {
		t.Errorf("hint missing --port: %q", hint)
	}
}

func TestInstallErrorHint_UnknownErr_Empty(t *testing.T) {
	friendly, hint := installErrorHint(errors.New("some random error"), installErrorContext{})
	if friendly != "" || hint != "" {
		t.Errorf("expected empty friendly/hint for unknown err: %q / %q", friendly, hint)
	}
}

func TestRenderInstallError_WithFriendly(t *testing.T) {
	err := syscall.ENOSPC
	got := renderInstallError(err, installErrorContext{Prefix: "/x"})
	if !strings.Contains(got, "Disk full") {
		t.Errorf("missing friendly: %s", got)
	}
	if !strings.Contains(got, "What to try:") {
		t.Errorf("missing hint label: %s", got)
	}
	if !strings.Contains(got, "Underlying:") {
		t.Errorf("missing underlying: %s", got)
	}
}

func TestRenderInstallError_NoFriendly_Verbatim(t *testing.T) {
	err := errors.New("random unmapped error")
	got := renderInstallError(err, installErrorContext{})
	if got != err.Error() {
		t.Errorf("expected verbatim, got %q", got)
	}
}

func TestPreflightPortAvailable_FreePort(t *testing.T) {
	// Find a free port by binding briefly.
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	addr := ln.Addr().String()
	_ = ln.Close()
	// Now the port should be free.
	if err := preflightPortAvailable(addr); err != nil {
		t.Errorf("free port should pass preflight, got %v", err)
	}
}

func TestPreflightPortAvailable_InUse(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer ln.Close()
	addr := ln.Addr().String()
	// Port is held by ln; preflight should fail.
	err = preflightPortAvailable(addr)
	if err == nil {
		t.Fatal("expected port-in-use error")
	}
	if !strings.Contains(err.Error(), "address already in use") &&
		!strings.Contains(err.Error(), "bind") {
		t.Errorf("err missing bind/in-use marker: %v", err)
	}
}

func TestPreflightPortAvailable_EmptyAddr_NoOp(t *testing.T) {
	if err := preflightPortAvailable(""); err != nil {
		t.Errorf("empty addr should pass, got %v", err)
	}
}

func TestInstallCenter_PortInUse_FriendlyError(t *testing.T) {
	t.Setenv("AGENT_CENTER_INSTALL_SKIP_ACTIVATE", "1")
	// Hold a port to trigger the preflight.
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer ln.Close()
	_, portStr, _ := net.SplitHostPort(ln.Addr().String())

	cmd := InstallCenterCommand()
	prefix := t.TempDir()
	_, stderr, code := runHandler(t, cmd, []string{
		"--prefix=" + prefix,
		"--port=" + portStr,
	})
	if code != ExitBusinessError {
		t.Fatalf("port-in-use should ExitBusinessError, got %d", code)
	}
	if !strings.Contains(stderr, "Port") || !strings.Contains(stderr, "already in use") {
		t.Errorf("stderr missing port-in-use msg: %q", stderr)
	}
	if !strings.Contains(stderr, "What to try:") {
		t.Errorf("stderr missing hint: %q", stderr)
	}
}
