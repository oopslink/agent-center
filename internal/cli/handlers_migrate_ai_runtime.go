package cli

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"strings"

	"github.com/oopslink/agent-center/internal/airuntime"
	airuntimesql "github.com/oopslink/agent-center/internal/airuntime/sqlite"
	"github.com/oopslink/agent-center/internal/persistence"
)

func MigrateAIRuntimeCommand() *Command {
	return &Command{
		Name:    "ai-runtime",
		Summary: "Dry-run AI Runtime legacy data mapping and cutover evidence",
		LongHelp: "Reads the configured SQLite database and emits the Stage 5 AI Runtime dry-run report:\n" +
			"exact Profile mappings, content-hash Profile dedupe candidates, object overrides,\n" +
			"unmapped legacy objects, shadow-compare evidence, and feature-flag rollback steps.\n" +
			"The command is read-only and requires --dry-run.",
		Examples: []string{
			"agent-center migrate ai-runtime --config=/etc/agent-center/config.yaml --org=organization-prod --dry-run --format=json",
			"agent-center migrate ai-runtime --config=/etc/agent-center/config.yaml --org=organization-prod --dry-run --stage=shadow_compare",
		},
		Flags: func(fs *flag.FlagSet) Handler {
			cfgPath := fs.String("config", "", "config file path")
			org := fs.String("org", "", "organization id to inspect")
			dryRun := fs.Bool("dry-run", false, "required; report planned mappings without mutating the DB")
			stage := fs.String("stage", string(airuntime.ResolverStageShadowCompare), "cutover stage: legacy_read|shadow_compare|new_resolver_canary|organization_default")
			format := fs.String("format", FormatTable, formatFlagHelp())
			return func(ctx context.Context, args []string, out, errw io.Writer) ExitCode {
				if !*dryRun {
					return PrintError(errw, *format, "usage_error", "migrate ai-runtime requires --dry-run", ExitUsage)
				}
				if strings.TrimSpace(*org) == "" {
					return PrintError(errw, *format, "usage_error", "--org required", ExitUsage)
				}
				cutoverStage := airuntime.ResolverCutoverStage(strings.TrimSpace(*stage))
				if !validAIRuntimeStage(cutoverStage) {
					return PrintError(errw, *format, "usage_error", "invalid --stage", ExitUsage)
				}
				cfg, err := loadConfigForCLI(*cfgPath, nil)
				if err != nil {
					emitConfigErrors(errw, err)
					return ExitUsage
				}
				db, err := persistence.Open(cfg.Server.SqlitePath)
				if err != nil {
					return PrintError(errw, *format, "db_open", err.Error(), ExitBusinessError)
				}
				defer db.Close()
				catalog, err := airuntimesql.ReadCatalog(ctx, db, strings.TrimSpace(*org))
				if err != nil {
					return PrintError(errw, *format, "catalog_read", err.Error(), ExitBusinessError)
				}
				objects, err := airuntimesql.ReadLegacyMigrationObjects(ctx, db, strings.TrimSpace(*org))
				if err != nil {
					return PrintError(errw, *format, "legacy_read", err.Error(), ExitBusinessError)
				}
				report, err := airuntime.NewMigrationPlanner().DryRun(catalog, objects, cutoverStage)
				if err != nil {
					return PrintError(errw, *format, "dry_run_failed", err.Error(), ExitBusinessError)
				}
				writeAIRuntimeMigrationReport(out, *format, report)
				return ExitOK
			}
		},
	}
}

func validAIRuntimeStage(stage airuntime.ResolverCutoverStage) bool {
	switch stage {
	case airuntime.ResolverStageLegacyRead,
		airuntime.ResolverStageShadowCompare,
		airuntime.ResolverStageNewResolverCanary,
		airuntime.ResolverStageOrganizationDefault:
		return true
	default:
		return false
	}
}

func writeAIRuntimeMigrationReport(out io.Writer, format string, report airuntime.MigrationDryRunReport) {
	if format == FormatJSON {
		b, _ := json.MarshalIndent(report, "", "  ")
		fmt.Fprintln(out, string(b))
		return
	}
	fmt.Fprintf(out, "AI Runtime migration dry-run for org %s\n", report.OrgID)
	fmt.Fprintf(out, "catalog revision: %d\n", report.CatalogRevision)
	fmt.Fprintf(out, "objects scanned: %d\n", report.TotalObjects)
	fmt.Fprintf(out, "counts: exact_profile=%d deduplicated_profile=%d object_override=%d unmapped=%d\n",
		report.Counts[airuntime.MigrationCategoryExactProfile],
		report.Counts[airuntime.MigrationCategoryDeduplicated],
		report.Counts[airuntime.MigrationCategoryObjectOverride],
		report.Counts[airuntime.MigrationCategoryUnmapped])
	for _, x := range report.ExactProfiles {
		fmt.Fprintf(out, "exact profile: %s (%s) objects=%d hash=%s\n", x.ProfileKey, x.ProfileID, len(x.Objects), x.ContentHash)
	}
	for _, x := range report.DeduplicatedProfiles {
		fmt.Fprintf(out, "dedupe profile: %s cli=%s model=%s objects=%d hash=%s\n", x.ProposedKey, x.CLIKey, x.ModelKey, len(x.Objects), x.ContentHash)
	}
	for _, x := range report.ObjectOverrides {
		fmt.Fprintf(out, "object override: %s/%s cli=%s model=%s hash=%s\n", x.Object.ObjectType, x.Object.ObjectID, x.CLIKey, x.ModelKey, x.ContentHash)
	}
	for _, x := range report.Unmapped {
		fmt.Fprintf(out, "unmapped: %s/%s reason=%s legacy_cli=%q legacy_model=%q\n", x.Object.ObjectType, x.Object.ObjectID, x.Reason, x.Original.CLI, x.Original.Model)
	}
	for _, x := range report.CutoverEvidence {
		fmt.Fprintf(out, "cutover: stage=%s flag=%s rollback=%s\n", x.Stage, x.FeatureFlag, x.Rollback)
	}
	fmt.Fprintf(out, "idempotency digest: %s\n", report.IdempotencyDigestSHA256)
	fmt.Fprintln(out, "dry-run: no changes applied")
}
