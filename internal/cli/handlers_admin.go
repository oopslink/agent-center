package cli

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"strings"
	"time"

	"github.com/oopslink/agent-center/internal/admin/backup"
	"github.com/oopslink/agent-center/internal/observability"
)

// AdminCommands returns the admin command subtree per
// 03-cli-subcommands § 8.x. Phase 7 lands `backup`; v2.3-3a (task #28)
// adds the `token` group (create / list / revoke).
func (a *App) AdminCommands() []*Command {
	return []*Command{
		{
			Name:    "backup",
			Summary: "Snapshot SQLite + prune old dated backups",
			LongHelp: "Runs a WAL checkpoint, copies the SQLite file into\n" +
				"<dest>/<YYYYMMDD-HHMMSS>/agent-center.db, then prunes any\n" +
				"dated subdirectories older than --retention-days.",
			Flags: a.adminBackupHandler,
		},
		{
			Name:    "token",
			Summary: "Admin bearer tokens for the unix-socket admin endpoint",
			LongHelp: "Manage the bearer tokens that authenticate clients of the\n" +
				"admin endpoint. The first token is auto-minted at server boot\n" +
				"(see <sqlite_dir>/bootstrap_token); subsequent tokens are issued\n" +
				"via `agent-center admin token create` and rotated via revoke +\n" +
				"create. Caller must hold a token with `admin:token` scope.",
			Subcommands: []*Command{
				{
					Name:    "create",
					Summary: "Mint a new admin bearer token (plaintext echoed once)",
					Flags:   a.adminTokenCreateHandler,
					Examples: []string{
						`agent-center admin token create --owner=cli:hayang --scope=*`,
						`agent-center admin token create --owner=worker:w-01 --scope=dispatch:pull,task:* --format=json`,
					},
				},
				{
					Name:     "list",
					Summary:  "List admin tokens (metadata only; value never echoed)",
					Flags:    a.adminTokenListHandler,
					Examples: []string{`agent-center admin token list --format=json`},
				},
				{
					Name:    "revoke",
					Summary: "Revoke an admin token (terminal)",
					Flags:   a.adminTokenRevokeHandler,
					Examples: []string{
						`agent-center admin token revoke <id> --reason="rotated"`,
					},
				},
			},
		},
	}
}

// =============================================================================
// admin token handlers
// =============================================================================

func (a *App) adminTokenCreateHandler(fs *flag.FlagSet) Handler {
	owner := fs.String("owner", "", "principal (kind:id) — required")
	scope := fs.String("scope", "", "comma-separated scopes — required (e.g. `*` or `task:*,dispatch:pull`)")
	createdBy := fs.String("created-by", "", "audit attribution (defaults to caller's bearer owner)")
	format := fs.String("format", FormatTable, formatFlagHelp())
	return func(ctx context.Context, args []string, out, errw io.Writer) ExitCode {
		if *owner == "" {
			return PrintError(errw, *format, "usage_error", "--owner required", ExitUsage)
		}
		if strings.TrimSpace(*scope) == "" {
			return PrintError(errw, *format, "usage_error", "--scope required (comma-separated)", ExitUsage)
		}
		scopes := splitScopes(*scope)
		if len(scopes) == 0 {
			return PrintError(errw, *format, "usage_error", "--scope must contain at least one entry", ExitUsage)
		}
		if a.Client == nil {
			return PrintError(errw, *format, "internal_error",
				"admin client not configured — start the server first", ExitNotImplemented)
		}
		res, err := a.Client.AdminTokenCreate(ctx, AdminTokenCreateRequest{
			Owner:     *owner,
			Scopes:    scopes,
			CreatedBy: *createdBy,
		})
		if err != nil {
			return HandleClientError(errw, *format, err)
		}
		if *format == FormatJSON {
			b, _ := json.Marshal(map[string]any{
				"id":        res.ID,
				"plaintext": res.Plaintext,
			})
			writeOut(out, string(b))
		} else {
			fmt.Fprintf(out, "created admin token %s\n  plaintext: %s\n  WARNING: plaintext is not stored; capture it now.\n",
				res.ID, res.Plaintext)
		}
		return ExitOK
	}
}

func (a *App) adminTokenListHandler(fs *flag.FlagSet) Handler {
	format := fs.String("format", FormatTable, formatFlagHelp())
	return func(ctx context.Context, args []string, out, errw io.Writer) ExitCode {
		if a.Client == nil {
			return PrintError(errw, *format, "internal_error",
				"admin client not configured — start the server first", ExitNotImplemented)
		}
		dtos, err := a.Client.AdminTokenList(ctx)
		if err != nil {
			return HandleClientError(errw, *format, err)
		}
		switch *format {
		case FormatJSON:
			b, _ := json.Marshal(dtos)
			writeOut(out, string(b))
		case FormatText:
			ids := make([]string, len(dtos))
			for i, t := range dtos {
				ids[i] = t.ID
			}
			writeTextLines(out, ids)
		default:
			fmt.Fprintf(out, "%-32s %-24s %-12s %-8s %s\n", "ID", "OWNER", "STATE", "VER", "SCOPES")
			for _, t := range dtos {
				state := "active"
				if t.RevokedAt != "" {
					state = "revoked"
				}
				fmt.Fprintf(out, "%-32s %-24s %-12s %-8d %s\n",
					t.ID, t.Owner, state, t.Version, strings.Join(t.Scopes, ","))
			}
		}
		return ExitOK
	}
}

func (a *App) adminTokenRevokeHandler(fs *flag.FlagSet) Handler {
	reason := fs.String("reason", "", "audit message (recommended)")
	format := fs.String("format", FormatTable, formatFlagHelp())
	return func(ctx context.Context, args []string, out, errw io.Writer) ExitCode {
		if len(args) < 1 {
			return PrintError(errw, *format, "usage_error", "admin token revoke <id> [--reason=...]", ExitUsage)
		}
		if a.Client == nil {
			return PrintError(errw, *format, "internal_error",
				"admin client not configured — start the server first", ExitNotImplemented)
		}
		if err := a.Client.AdminTokenRevoke(ctx, AdminTokenRevokeRequest{
			ID:     args[0],
			Reason: *reason,
		}); err != nil {
			return HandleClientError(errw, *format, err)
		}
		writeOut(out, fmt.Sprintf("revoked admin token %s", args[0]))
		return ExitOK
	}
}

// splitScopes turns a comma-separated `--scope=` value into a deduped
// scope list. Empty / whitespace-only entries are dropped.
func splitScopes(raw string) []string {
	out := make([]string, 0, 4)
	seen := map[string]struct{}{}
	for _, s := range strings.Split(raw, ",") {
		s = strings.TrimSpace(s)
		if s == "" {
			continue
		}
		if _, dup := seen[s]; dup {
			continue
		}
		seen[s] = struct{}{}
		out = append(out, s)
	}
	return out
}

func (a *App) adminBackupHandler(fs *flag.FlagSet) Handler {
	dest := fs.String("dest", "", "destination root directory")
	retentionDays := fs.Int("retention-days", backup.DefaultRetentionDays,
		"retention window in days for dated subdirectories")
	format := fs.String("format", FormatTable, formatFlagHelp())
	return func(ctx context.Context, args []string, out, errw io.Writer) ExitCode {
		if *dest == "" {
			return PrintError(errw, *format, "usage_error",
				"--dest required", ExitUsage)
		}
		runner, err := backup.NewRunner(backup.Config{
			DB:        a.DB,
			DBPath:    a.Config.Server.SqlitePath,
			DestRoot:  *dest,
			Retention: time.Duration(*retentionDays) * 24 * time.Hour,
			Sink:      a.Sink,
			Clock:     a.Clock,
			Actor:     observability.Actor("system"),
		})
		if err != nil {
			return PrintError(errw, *format, "internal_error", err.Error(),
				ExitBusinessError)
		}
		res, err := runner.Run(ctx)
		if err != nil {
			return PrintError(errw, *format, "backup_failed", err.Error(),
				ExitBusinessError)
		}
		if *format == "json" {
			fmt.Fprintf(out, `{"dest_dir":%q,"dest_file":%q,"bytes":%d,"pruned":%d}`+"\n",
				res.DestDir, res.DestFile, res.BytesCopied, len(res.Pruned))
		} else {
			fmt.Fprintf(out, "backup ok: %s (%d bytes); pruned %d old dirs\n",
				res.DestFile, res.BytesCopied, len(res.Pruned))
		}
		return ExitOK
	}
}
