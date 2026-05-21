package cli

import (
	"context"
	"flag"
	"fmt"
	"io"
	"time"

	"github.com/oopslink/agent-center/internal/admin/backup"
	"github.com/oopslink/agent-center/internal/observability"
)

// AdminCommands returns the admin command subtree per
// 03-cli-subcommands § 8.x. Phase 7 lands `backup` as the first real
// admin command (alongside the existing `blob-migrate` placeholder).
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
	}
}

func (a *App) adminBackupHandler(fs *flag.FlagSet) Handler {
	dest := fs.String("dest", "", "destination root directory")
	retentionDays := fs.Int("retention-days", backup.DefaultRetentionDays,
		"retention window in days for dated subdirectories")
	format := fs.String("format", "human", "output format (human|json)")
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
