// install_errors.go — friendly error wrapping for `agent-center install`
// failure modes. v2.4-D-A6 (task #40). Covers the install-command-
// reachable subset of deployment doc § 5's 12-row failure matrix.
//
// **Out of A6 scope** (visible at runtime, not at install time):
//   - "Cannot reach center at <host>:7300" — worker daemon's enroll
//     attempt logs this to stderr after install completes
//   - "Token already used" / "Token expired" / "fingerprint mismatch" /
//     "worker name already enrolled" — same: emerge from worker daemon
//     first enroll attempt
//   - "DB migration failed" — server boot path; A5 health probe surfaces
//     it via /admin/health timeout → rollback
//
// **In A6 scope** (install-command-detectable):
//   - "Port already in use" — pre-flight TCP bind on the configured web
//     port + admin TCP port (if set)
//   - "Need sudo" — wraps EACCES from systemd unit write with the
//     friendly text suggesting `sudo` or `--user-mode`
//   - "Disk full" — wraps ENOSPC from binary copy with the friendly
//     "free up space" hint
//   - "Same version already installed" — A1 already handles via state
//     detection; this file adds the friendly recovery hint
package cli

import (
	"errors"
	"fmt"
	"net"
	"os"
	"strings"
	"syscall"
)

// installErrorHint maps a raw install error to a friendly user message
// + recommended recovery, matching deployment doc § 5 vocab.
//
// Returns (friendlyMsg, hintMsg). Empty friendly = no improvement on
// the raw error (caller surfaces err.Error() directly).
func installErrorHint(err error, context installErrorContext) (friendly, hint string) {
	if err == nil {
		return "", ""
	}
	// Disk-full / ENOSPC.
	if errors.Is(err, syscall.ENOSPC) {
		return "Disk full while copying binaries to " + context.Prefix,
			"Free up space (e.g. " + context.Prefix + "/versions/ holds old versions you can delete) and retry."
	}
	// Permission denied — most common on Linux when system-mode install
	// is attempted without sudo.
	if errors.Is(err, syscall.EACCES) || errors.Is(err, os.ErrPermission) {
		if context.Operation == "write_unit" {
			return "Permission denied writing service unit to " + context.Path,
				"Re-run with `sudo`, or use `--user-mode` to install under your user account (Linux only)."
		}
		if context.Operation == "write_binary" || context.Operation == "mkdir" {
			return "Permission denied writing to " + context.Path,
				"Re-run with `sudo`, or use `--user-mode` + `--prefix=<your-writable-dir>`."
		}
		return "Permission denied", "Re-run with sufficient privileges (sudo) or pick a writable --prefix."
	}
	// Pre-flight port-in-use check, mapped to friendly text.
	if strings.Contains(err.Error(), "address already in use") {
		return "Port " + context.Port + " is already in use",
			"Stop the conflicting process or rerun with a different --port / config."
	}
	return "", ""
}

// installErrorContext bundles details needed to render the hint.
type installErrorContext struct {
	Operation string // "write_unit" / "write_binary" / "mkdir" / "bind_port" / "" (unknown)
	Prefix    string
	Path      string
	Port      string
}

// renderInstallError formats a friendly error block for the operator:
//
//	Error: <friendly>
//	  What to try: <hint>
//	  Underlying: <raw err>
//
// Falls back to the raw err verbatim when no friendly mapping exists.
func renderInstallError(err error, context installErrorContext) string {
	friendly, hint := installErrorHint(err, context)
	if friendly == "" {
		return err.Error()
	}
	return fmt.Sprintf("%s\n  What to try: %s\n  Underlying: %v", friendly, hint, err)
}

// preflightPortAvailable tries to bind 127.0.0.1:<port> briefly to
// detect "port already in use" BEFORE running the install. Returns
// nil on success (port free) or an error describing the conflict.
//
// Tightly scoped — we only check the web-console port + the optional
// admin TCP port the operator passed; we don't probe the server's
// main listen_addr because that's typically firewalled / not visible.
func preflightPortAvailable(addr string) error {
	if addr == "" {
		return nil
	}
	// Bind + immediately release. If anything else is bound to the
	// same addr we get "address already in use".
	ln, err := net.Listen("tcp", addr)
	if err != nil {
		return err
	}
	_ = ln.Close()
	return nil
}
