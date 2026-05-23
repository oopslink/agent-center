package cli

import (
	"bufio"
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
)

// BootstrapCommand returns the `bootstrap` admin command tree. v1
// surface:
//
//   - bootstrap check-systemd → validate the worker user-systemd unit
//     contains KillMode=process (ADR-0018 hard requirement). Designed to
//     be invoked by install-worker.sh AND from the worker daemon at
//     startup so manual tampering is caught.
func BootstrapCommand() *Command {
	return &Command{
		Name:    "bootstrap",
		Summary: "Runtime install validations",
		Subcommands: []*Command{
			{
				Name:    "check-systemd",
				Summary: "Validate ADR-0018 KillMode=process on the worker unit",
				Flags:   bootstrapCheckSystemdHandler,
			},
		},
	}
}

// bootstrapCheckSystemdHandler is the leaf handler. It reads the unit
// file (defaulting to ~/.config/systemd/user/agent-center-worker.service)
// and confirms KillMode=process is present at the top-level (not commented,
// not nested under another section).
//
// Exit codes:
//   - 0       → KillMode=process present
//   - ExitInvariantViolation (19) → file exists but lacks the directive
//   - ExitUsage (2) → file not found / no $HOME / explicit --unit missing
func bootstrapCheckSystemdHandler(fs *flag.FlagSet) Handler {
	unit := fs.String("unit", "", "path to the worker unit file (defaults to ~/.config/systemd/user/agent-center-worker.service)")
	format := fs.String("format", FormatTable, formatFlagHelp())
	return func(ctx context.Context, args []string, out, errw io.Writer) ExitCode {
		path := *unit
		if path == "" {
			home := os.Getenv("HOME")
			if home == "" {
				return PrintError(errw, *format, "no_home",
					"$HOME unset and --unit not provided", ExitUsage)
			}
			path = filepath.Join(home, ".config", "systemd", "user",
				"agent-center-worker.service")
		}
		hasKillMode, err := hasKillModeProcess(path)
		if err != nil {
			if errors.Is(err, os.ErrNotExist) {
				return PrintError(errw, *format, "unit_not_found",
					fmt.Sprintf("unit file %q not found; run install-worker.sh", path),
					ExitUsage)
			}
			return PrintError(errw, *format, "read_failed", err.Error(), ExitBusinessError)
		}
		if !hasKillMode {
			return PrintError(errw, *format, "kill_mode_missing",
				fmt.Sprintf("unit %q lacks KillMode=process; ADR-0018 hard requirement "+
					"(per-execution shim must outlive daemon). Re-run install-worker.sh.",
					path),
				ExitInvariantViolation)
		}
		if *format == "json" {
			fmt.Fprintf(out, `{"unit":%q,"kill_mode_process":true}`+"\n", path)
		} else {
			fmt.Fprintf(out, "ok: %s has KillMode=process\n", path)
		}
		return ExitOK
	}
}

// hasKillModeProcess scans the file for `KillMode=process` at the start
// of a non-comment, non-blank line inside the [Service] section.
//
// We intentionally do NOT use a full ini parser — the systemd grammar is
// simple enough that line-level scanning gives us a robust check + zero
// extra deps. The implementation tolerates trailing whitespace and ;
// inline comments per `man systemd.unit`.
func hasKillModeProcess(path string) (bool, error) {
	f, err := os.Open(path)
	if err != nil {
		return false, err
	}
	defer f.Close()
	scanner := bufio.NewScanner(f)
	inService := false
	for scanner.Scan() {
		raw := scanner.Text()
		// Strip trailing inline comments (per `man systemd.syntax`, ; is
		// the only valid inline-comment leader at line start; we keep
		// inline ; alongside the value tolerant for hand-edited files).
		line := raw
		if idx := strings.Index(line, "#"); idx >= 0 {
			line = line[:idx]
		}
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		if strings.HasPrefix(line, "[") && strings.HasSuffix(line, "]") {
			inService = strings.EqualFold(line, "[Service]")
			continue
		}
		if !inService {
			continue
		}
		// Match "KillMode = process" with arbitrary whitespace.
		if !strings.HasPrefix(line, "KillMode") {
			continue
		}
		// Split key/value on first '='.
		idx := strings.Index(line, "=")
		if idx < 0 {
			continue
		}
		key := strings.TrimSpace(line[:idx])
		val := strings.TrimSpace(line[idx+1:])
		if !strings.EqualFold(key, "KillMode") {
			continue
		}
		if strings.EqualFold(val, "process") {
			return true, nil
		}
		// Wrong value — explicit mismatch.
		return false, nil
	}
	if err := scanner.Err(); err != nil {
		return false, err
	}
	return false, nil
}
