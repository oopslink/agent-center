package cli

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"strings"
	"time"

	"github.com/oopslink/agent-center/internal/observability"
	"github.com/oopslink/agent-center/internal/secretmgmt"
	secretservice "github.com/oopslink/agent-center/internal/secretmgmt/service"
)

// SecretCommands returns the `secret` subcommand tree per P11 § 3.7b.
// All commands operate on UserSecret; resolution is internal-only via
// SecretRef (per ADR-0027) and is NOT exposed to the user CLI.
//
// Plaintext rules (per ADR-0026 § 5):
//   - value never echoed back by list / show
//   - create input comes from --value-file=<path> | -file=- | interactive
//     prompt; literal --value=<...> on the command line is forbidden to
//     avoid shell-history leakage.
func (a *App) SecretCommands() []*Command {
	return []*Command{
		{Name: "list", Summary: "List user secrets (metadata only)", Flags: a.secretListHandler},
		{Name: "show", Summary: "Show one user secret (metadata only; value never echoed)", Flags: a.secretShowHandler},
		{Name: "create", Summary: "Create a new user secret", Flags: a.secretCreateHandler},
		{Name: "revoke", Summary: "Revoke a user secret (state → revoked, terminal)", Flags: a.secretRevokeHandler},
	}
}

func (a *App) secretListHandler(fs *flag.FlagSet) Handler {
	kindFlag := fs.String("kind", "", "filter by kind (mcp|cloud_credential|repo_deploy_key|other)")
	stateFlag := fs.String("state", "", "filter by state (active|revoked)")
	format := fs.String("format", FormatTable, formatFlagHelp())
	return func(ctx context.Context, args []string, out, errw io.Writer) ExitCode {
		if a.UserSecretRepo == nil {
			return PrintError(errw, *format, "internal_error", "secret repo not wired", ExitNotImplemented)
		}
		filter := secretmgmt.UserSecretFilter{}
		if *kindFlag != "" {
			k := secretmgmt.UserSecretKind(*kindFlag)
			filter.Kind = &k
		}
		if *stateFlag != "" {
			s := secretmgmt.UserSecretState(*stateFlag)
			filter.State = &s
		}
		secrets, err := a.UserSecretRepo.FindAll(ctx, filter)
		if err != nil {
			return HandleDomainError(errw, *format, err)
		}
		switch *format {
		case FormatJSON:
			arr := make([]map[string]any, len(secrets))
			for i, s := range secrets {
				arr[i] = secretToMap(s)
			}
			b, _ := json.Marshal(arr)
			writeOut(out, string(b))
		case FormatText:
			ids := make([]string, len(secrets))
			for i, s := range secrets {
				ids[i] = string(s.ID())
			}
			writeTextLines(out, ids)
		default:
			fmt.Fprintf(out, "%-32s %-20s %-8s %-20s %s\n", "ID", "NAME", "KIND", "STATE", "CREATED_AT")
			for _, s := range secrets {
				fmt.Fprintf(out, "%-32s %-20s %-8s %-20s %s\n",
					s.ID(), s.Name(), s.Kind(), s.State(), s.CreatedAt().Format(time.RFC3339))
			}
		}
		return ExitOK
	}
}

func (a *App) secretShowHandler(fs *flag.FlagSet) Handler {
	byName := fs.Bool("by-name", false, "treat <arg> as name (default: id)")
	format := fs.String("format", FormatTable, formatFlagHelp())
	return func(ctx context.Context, args []string, out, errw io.Writer) ExitCode {
		if len(args) < 1 {
			return PrintError(errw, *format, "usage_error", "secret show <id-or-name> [--by-name]", ExitUsage)
		}
		if a.UserSecretRepo == nil {
			return PrintError(errw, *format, "internal_error", "secret repo not wired", ExitNotImplemented)
		}
		var s *secretmgmt.UserSecret
		var err error
		if *byName {
			s, err = a.UserSecretRepo.FindByName(ctx, args[0])
		} else {
			s, err = a.UserSecretRepo.FindByID(ctx, secretmgmt.UserSecretID(args[0]))
		}
		if err != nil {
			return HandleDomainError(errw, *format, err)
		}
		m := secretToMap(s)
		if *format == "json" {
			b, _ := json.Marshal(m)
			writeOut(out, string(b))
		} else {
			fmt.Fprintf(out, "secret %s\n  name: %s\n  kind: %s\n  state: %s\n  created_at: %s\n  created_by: %s\n",
				s.ID(), s.Name(), s.Kind(), s.State(),
				s.CreatedAt().Format(time.RFC3339), s.CreatedBy())
			if r := s.RevokedAt(); r != nil {
				fmt.Fprintf(out, "  revoked_at: %s\n  revoked_by: %s\n  revoked_reason: %s\n  revoked_message: %s\n",
					r.Format(time.RFC3339), s.RevokedBy(), s.RevokedReason(), s.RevokedMessage())
			}
		}
		return ExitOK
	}
}

func (a *App) secretCreateHandler(fs *flag.FlagSet) Handler {
	name := fs.String("name", "", "secret name (required, unique)")
	kindFlag := fs.String("kind", "other", "kind (mcp|cloud_credential|repo_deploy_key|other)")
	valueFile := fs.String("value-file", "", "read plaintext from file ('-' = stdin); omit to use interactive prompt")
	format := fs.String("format", FormatTable, formatFlagHelp())
	return func(ctx context.Context, args []string, out, errw io.Writer) ExitCode {
		if *name == "" {
			return PrintError(errw, *format, "usage_error", "--name required", ExitUsage)
		}
		kind := secretmgmt.UserSecretKind(*kindFlag)
		if !validSecretKind(kind) {
			return PrintError(errw, *format, "usage_error",
				"--kind must be one of mcp|cloud_credential|repo_deploy_key|other", ExitUsage)
		}
		if a.UserSecretSvc == nil {
			return PrintError(errw, *format, "internal_error",
				"user secret service not wired", ExitNotImplemented)
		}
		plaintext, err := resolveSecretInput(*valueFile)
		if err != nil {
			return PrintError(errw, *format, "usage_error", err.Error(), ExitUsage)
		}
		res, err := a.UserSecretSvc.Create(ctx, secretservice.CreateSecretCommand{
			Name: *name, Kind: kind,
			Plaintext: plaintext, ActorIdentity: a.DefaultActor(),
		})
		// Best-effort wipe of plaintext buffer.
		for i := range plaintext {
			plaintext[i] = 0
		}
		if err != nil {
			return HandleDomainError(errw, *format, err)
		}
		if *format == "json" {
			b, _ := json.Marshal(map[string]any{
				"id":       string(res.ID),
				"name":     res.Name,
				"event_id": string(res.EventID),
			})
			writeOut(out, string(b))
		} else {
			fmt.Fprintf(out, "created secret %s (id=%s) — plaintext stored encrypted; not echoed\n", res.Name, res.ID)
		}
		return ExitOK
	}
}

func (a *App) secretRevokeHandler(fs *flag.FlagSet) Handler {
	reasonStr := fs.String("reason", string(secretmgmt.UserSecretRevokedReasonManual),
		"reason (manual|rotated|compromise)")
	message := fs.String("message", "", "revoke message (required)")
	versionFlag := fs.Int("version", 0, "expected version (CAS); 0 = look up current version")
	format := fs.String("format", FormatTable, formatFlagHelp())
	return func(ctx context.Context, args []string, out, errw io.Writer) ExitCode {
		if len(args) < 1 {
			return PrintError(errw, *format, "usage_error", "secret revoke <id>", ExitUsage)
		}
		if *message == "" {
			return PrintError(errw, *format, "usage_error", "--message required", ExitUsage)
		}
		if a.UserSecretSvc == nil {
			return PrintError(errw, *format, "internal_error",
				"user secret service not wired", ExitNotImplemented)
		}
		ver := *versionFlag
		if ver == 0 {
			sec, err := a.UserSecretRepo.FindByID(ctx, secretmgmt.UserSecretID(args[0]))
			if err != nil {
				return HandleDomainError(errw, *format, err)
			}
			ver = sec.Version()
		}
		if _, err := a.UserSecretSvc.Revoke(ctx, secretservice.RevokeSecretCommand{
			ID:            secretmgmt.UserSecretID(args[0]),
			Reason:        secretmgmt.UserSecretRevokedReason(*reasonStr),
			Message:       *message,
			Version:       ver,
			ActorIdentity: a.DefaultActor(),
		}); err != nil {
			return HandleDomainError(errw, *format, err)
		}
		writeOut(out, fmt.Sprintf("revoked secret %s", args[0]))
		return ExitOK
	}
}

func secretToMap(s *secretmgmt.UserSecret) map[string]any {
	m := map[string]any{
		"id":         string(s.ID()),
		"name":       s.Name(),
		"kind":       string(s.Kind()),
		"state":      string(s.State()),
		"created_at": s.CreatedAt().Format(time.RFC3339Nano),
		"created_by": s.CreatedBy(),
		"version":    s.Version(),
	}
	if r := s.RevokedAt(); r != nil {
		m["revoked_at"] = r.Format(time.RFC3339Nano)
		m["revoked_by"] = s.RevokedBy()
		m["revoked_reason"] = string(s.RevokedReason())
		m["revoked_message"] = s.RevokedMessage()
	}
	if ru := s.RotatedAt(); ru != nil {
		m["rotated_at"] = ru.Format(time.RFC3339Nano)
	}
	if lu := s.LastUsedAt(); lu != nil {
		m["last_used_at"] = lu.Format(time.RFC3339Nano)
	}
	return m
}

func validSecretKind(k secretmgmt.UserSecretKind) bool {
	switch k {
	case secretmgmt.UserSecretKindMCP, secretmgmt.UserSecretKindCloudCredential,
		secretmgmt.UserSecretKindRepoDeployKey, secretmgmt.UserSecretKindOther:
		return true
	}
	return false
}

// resolveSecretInput reads the plaintext value from one of:
//
//  1. --value-file=<path> (regular file)
//  2. --value-file=- (stdin)
//  3. interactive prompt (omit flag and stdin is a TTY)
//  4. piped stdin (omit flag, stdin is not a TTY)
//
// Returns an error on empty input. Plaintext is NEVER accepted from a
// `--value=` command-line flag to avoid shell history leakage (per
// ADR-0026 § 5).
func resolveSecretInput(valueFile string) ([]byte, error) {
	if valueFile != "" {
		if valueFile == "-" {
			data, err := io.ReadAll(os.Stdin)
			if err != nil {
				return nil, fmt.Errorf("read stdin: %w", err)
			}
			body := trimTrailingNewline(data)
			if len(body) == 0 {
				return nil, errors.New("empty value from stdin")
			}
			return body, nil
		}
		data, err := os.ReadFile(valueFile)
		if err != nil {
			return nil, fmt.Errorf("read --value-file: %w", err)
		}
		body := trimTrailingNewline(data)
		if len(body) == 0 {
			return nil, fmt.Errorf("empty value in %s", valueFile)
		}
		return body, nil
	}
	info, err := os.Stdin.Stat()
	if err == nil && (info.Mode()&os.ModeCharDevice) == 0 {
		data, _ := io.ReadAll(os.Stdin)
		body := trimTrailingNewline(data)
		if len(body) > 0 {
			return body, nil
		}
	}
	// Interactive prompt — read until newline. We do NOT echo prompt for
	// the value to stdout; user types invisibly is preferred but we don't
	// have terminal control here. A passthrough prompt to stderr is the
	// pragmatic v2 behaviour; user can `secret create ... < file` to skip.
	fmt.Fprint(os.Stderr, "value> ")
	reader := bufio.NewReader(os.Stdin)
	line, err := reader.ReadString('\n')
	if err != nil && !errors.Is(err, io.EOF) {
		return nil, fmt.Errorf("read interactive prompt: %w", err)
	}
	body := trimTrailingNewline([]byte(line))
	if len(body) == 0 {
		return nil, errors.New("value required")
	}
	return body, nil
}

func trimTrailingNewline(b []byte) []byte {
	return []byte(strings.TrimRight(string(b), "\n"))
}

// _ keeps observability import alive for actor types.
var _ = observability.Actor("")
