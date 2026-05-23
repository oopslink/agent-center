package cli

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/oopslink/agent-center/internal/persistence"
)

// helper: write a config that points at a fresh sqlite at dbPath.
func writeMigrateCfg(t *testing.T, cfgPath, dbPath string) {
	t.Helper()
	body := "server:\n" +
		"  listen_addr: ':7000'\n" +
		"  sqlite_path: '" + dbPath + "'\n" +
		"identity:\n" +
		"  default_user: hayang\n"
	if err := os.WriteFile(cfgPath, []byte(body), 0o600); err != nil {
		t.Fatalf("write cfg: %v", err)
	}
}

// helper: seed a v1 sqlite by migrating up to v6 (last pre-Phase-8
// version) and inserting bridge rows so the archive step has real
// content. The v6 schema still has the bridge tables (created by 0005).
func seedV1Bridge(t *testing.T, dbPath string) (feishuCount, cursorCount int) {
	t.Helper()
	db, err := persistence.Open(dbPath)
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	if err := persistence.NewMigrator(db).Up(ctx); err != nil {
		t.Fatal(err)
	}
	if err := persistence.NewMigrator(db).Down(ctx, 6); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`
		INSERT INTO feishu_delivery_ledger
		(id, message_id, conversation_id, channel, status, retry_count, updated_at, created_at, version)
		VALUES
		('led-1','m-1','c-1','feishu','delivered',0,'2026-05-22T00:00:00Z','2026-05-22T00:00:00Z',1),
		('led-2','m-2','c-1','feishu','failed',2,'2026-05-22T00:00:00Z','2026-05-22T00:00:00Z',1);
		INSERT INTO bridge_subscription_cursors (subscriber, last_event_id, updated_at)
		VALUES ('feishu_outbound','01ABC','2026-05-22T00:00:00Z');
	`); err != nil {
		t.Fatalf("seed bridge: %v", err)
	}
	_ = db.Close()
	return 2, 1
}

func TestMigrateV1ToV2_DryRunReportsCounts(t *testing.T) {
	dir := t.TempDir()
	dbPath := filepath.Join(dir, "v1.db")
	cfgPath := filepath.Join(dir, "cfg.yaml")
	writeMigrateCfg(t, cfgPath, dbPath)
	fCount, cCount := seedV1Bridge(t, dbPath)

	cmd := MigrateV1ToV2Command()
	stdout, _, code := runHandler(t, cmd, []string{
		"--config=" + cfgPath, "--dry-run",
	})
	if code != ExitOK {
		t.Fatalf("code=%d stdout=%s", code, stdout)
	}
	for _, want := range []string{
		"current schema version: 6",
		"target  schema version: 25",
		"feishu_delivery_ledger:    2",
		"bridge_subscription_cursors: 1",
		"dry-run: no changes applied",
	} {
		if !strings.Contains(stdout, want) {
			t.Fatalf("missing %q in output:\n%s", want, stdout)
		}
	}
	// DB unchanged: still at v6.
	db, _ := persistence.Open(dbPath)
	defer db.Close()
	v, _ := persistence.NewMigrator(db).Version(context.Background())
	if v != 6 {
		t.Fatalf("dry-run mutated schema: version=%d want 6", v)
	}
	_ = fCount
	_ = cCount
}

func TestMigrateV1ToV2_ApplyArchivesAndUpgrades(t *testing.T) {
	dir := t.TempDir()
	dbPath := filepath.Join(dir, "v1.db")
	cfgPath := filepath.Join(dir, "cfg.yaml")
	archiveDir := filepath.Join(dir, "archive")
	writeMigrateCfg(t, cfgPath, dbPath)
	_, _ = seedV1Bridge(t, dbPath)

	cmd := MigrateV1ToV2Command()
	stdout, _, code := runHandler(t, cmd, []string{
		"--config=" + cfgPath, "--apply", "--archive-dir=" + archiveDir,
	})
	if code != ExitOK {
		t.Fatalf("code=%d stdout=%s", code, stdout)
	}
	if !strings.Contains(stdout, "new schema version: 25") {
		t.Fatalf("expected new version line; got:\n%s", stdout)
	}

	// Archive file exists + contains the seeded rows.
	entries, err := os.ReadDir(archiveDir)
	if err != nil {
		t.Fatalf("read archive dir: %v", err)
	}
	if len(entries) != 1 {
		t.Fatalf("expected exactly 1 archive file, got %d", len(entries))
	}
	body, err := os.ReadFile(filepath.Join(archiveDir, entries[0].Name()))
	if err != nil {
		t.Fatal(err)
	}
	var doc struct {
		SchemaVersionBefore int `json:"schema_version_before"`
		Bridge              struct {
			FeishuDeliveryLedger      []map[string]any `json:"feishu_delivery_ledger"`
			BridgeSubscriptionCursors []map[string]any `json:"bridge_subscription_cursors"`
		} `json:"bridge"`
	}
	if err := json.Unmarshal(body, &doc); err != nil {
		t.Fatalf("parse archive: %v\nbody=%s", err, body)
	}
	if doc.SchemaVersionBefore != 6 {
		t.Fatalf("schema_version_before=%d want 6", doc.SchemaVersionBefore)
	}
	if len(doc.Bridge.FeishuDeliveryLedger) != 2 {
		t.Fatalf("feishu rows=%d want 2", len(doc.Bridge.FeishuDeliveryLedger))
	}
	if len(doc.Bridge.BridgeSubscriptionCursors) != 1 {
		t.Fatalf("cursor rows=%d want 1", len(doc.Bridge.BridgeSubscriptionCursors))
	}
	// Spot-check a row carries the expected message id.
	if doc.Bridge.FeishuDeliveryLedger[0]["message_id"] != "m-1" {
		t.Fatalf("ledger[0].message_id = %v want m-1", doc.Bridge.FeishuDeliveryLedger[0]["message_id"])
	}

	// Post-migration: bridge tables gone, schema at 25.
	db, _ := persistence.Open(dbPath)
	defer db.Close()
	v, _ := persistence.NewMigrator(db).Version(context.Background())
	if v != 25 {
		t.Fatalf("post-apply version=%d want 25", v)
	}
	for _, tbl := range []string{"feishu_delivery_ledger", "bridge_subscription_cursors"} {
		var n int
		_ = db.QueryRow(`SELECT COUNT(*) FROM sqlite_master WHERE type='table' AND name=?`, tbl).Scan(&n)
		if n != 0 {
			t.Fatalf("table %s still present after apply (count=%d)", tbl, n)
		}
	}
}

func TestMigrateV1ToV2_Idempotent(t *testing.T) {
	dir := t.TempDir()
	dbPath := filepath.Join(dir, "v1.db")
	cfgPath := filepath.Join(dir, "cfg.yaml")
	archiveDir := filepath.Join(dir, "archive")
	writeMigrateCfg(t, cfgPath, dbPath)
	_, _ = seedV1Bridge(t, dbPath)

	cmd := MigrateV1ToV2Command()
	// First apply: writes archive + upgrades.
	if _, _, code := runHandler(t, cmd, []string{
		"--config=" + cfgPath, "--apply", "--archive-dir=" + archiveDir,
	}); code != ExitOK {
		t.Fatalf("first apply code=%d", code)
	}
	firstFiles, _ := os.ReadDir(archiveDir)

	// Second apply: should report "already at v2" + NOT write new archive.
	stdout, _, code := runHandler(t, cmd, []string{
		"--config=" + cfgPath, "--apply", "--archive-dir=" + archiveDir,
	})
	if code != ExitOK {
		t.Fatalf("second apply code=%d stdout=%s", code, stdout)
	}
	if !strings.Contains(stdout, "already at v2") {
		t.Fatalf("expected 'already at v2'; got:\n%s", stdout)
	}
	secondFiles, _ := os.ReadDir(archiveDir)
	if len(firstFiles) != len(secondFiles) {
		t.Fatalf("idempotent re-apply wrote new archive: before=%d after=%d",
			len(firstFiles), len(secondFiles))
	}
}

func TestMigrateV1ToV2_RefusesSilently(t *testing.T) {
	dir := t.TempDir()
	dbPath := filepath.Join(dir, "v1.db")
	cfgPath := filepath.Join(dir, "cfg.yaml")
	writeMigrateCfg(t, cfgPath, dbPath)

	cmd := MigrateV1ToV2Command()
	_, errOut, code := runHandler(t, cmd, []string{"--config=" + cfgPath})
	if code != ExitUsage {
		t.Fatalf("code=%d want usage", code)
	}
	if !strings.Contains(errOut, "must pass exactly one of --dry-run or --apply") {
		t.Fatalf("missing usage error; got: %s", errOut)
	}
}

func TestMigrateV1ToV2_DryRunOnFreshV2(t *testing.T) {
	dir := t.TempDir()
	dbPath := filepath.Join(dir, "v2.db")
	cfgPath := filepath.Join(dir, "cfg.yaml")
	writeMigrateCfg(t, cfgPath, dbPath)
	// Bring the DB straight to v2.
	db, _ := persistence.Open(dbPath)
	if err := persistence.NewMigrator(db).Up(context.Background()); err != nil {
		t.Fatal(err)
	}
	_ = db.Close()

	cmd := MigrateV1ToV2Command()
	stdout, _, code := runHandler(t, cmd, []string{
		"--config=" + cfgPath, "--dry-run",
	})
	if code != ExitOK {
		t.Fatalf("code=%d stdout=%s", code, stdout)
	}
	if !strings.Contains(stdout, "already at v2") {
		t.Fatalf("expected 'already at v2' on fresh v2 DB; got:\n%s", stdout)
	}
}
