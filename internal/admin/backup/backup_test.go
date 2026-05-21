package backup_test

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/oopslink/agent-center/internal/admin/backup"
	"github.com/oopslink/agent-center/internal/clock"
	"github.com/oopslink/agent-center/internal/idgen"
	"github.com/oopslink/agent-center/internal/observability"
	obsqlite "github.com/oopslink/agent-center/internal/observability/sqlite"
	"github.com/oopslink/agent-center/internal/persistence"
)

type kit struct {
	t      *testing.T
	dir    string
	dbPath string
	dest   string
	clock  *clock.FakeClock
	sink   *observability.EventSink
	events *obsqlite.EventRepo
}

func newKit(t *testing.T) *kit {
	t.Helper()
	dir := t.TempDir()
	dbPath := filepath.Join(dir, "agent-center.db")
	db, err := persistence.Open(dbPath)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	if err := persistence.NewMigrator(db).Up(context.Background()); err != nil {
		t.Fatal(err)
	}
	fc := clock.NewFakeClock(time.Date(2026, 5, 22, 10, 0, 0, 0, time.UTC))
	gen := idgen.NewGenerator(fc)
	er, err := obsqlite.NewEventRepo(context.Background(), db)
	if err != nil {
		t.Fatal(err)
	}
	sink := observability.NewEventSink(er, er, gen, fc)
	k := &kit{t: t, dir: dir, dbPath: dbPath, dest: filepath.Join(dir, "backups"), clock: fc, sink: sink, events: er}
	t.Cleanup(func() {})
	return k
}

func (k *kit) newRunner(t *testing.T, retention time.Duration) *backup.Runner {
	t.Helper()
	db, err := persistence.Open(k.dbPath)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	r, err := backup.NewRunner(backup.Config{
		DB:        db,
		DBPath:    k.dbPath,
		DestRoot:  k.dest,
		Retention: retention,
		Sink:      k.sink,
		Clock:     k.clock,
		Actor:     observability.Actor("system"),
	})
	if err != nil {
		t.Fatal(err)
	}
	return r
}

func TestRunner_Happy(t *testing.T) {
	k := newKit(t)
	r := k.newRunner(t, time.Hour)
	res, err := r.Run(context.Background())
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	if res.BytesCopied <= 0 {
		t.Errorf("bytes: %d", res.BytesCopied)
	}
	if !res.WALCheckpoint {
		t.Error("wal checkpoint flag not set")
	}
	if _, err := os.Stat(res.DestFile); err != nil {
		t.Errorf("dest file: %v", err)
	}
	// admin.backup_ok event emitted.
	et := observability.EventType("admin.backup_ok")
	evs, err := k.events.Find(context.Background(), observability.EventQueryFilter{
		EventType: &et, Limit: 10,
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(evs) == 0 {
		t.Error("admin.backup_ok not emitted")
	}
}

func TestRunner_RetentionPrunes(t *testing.T) {
	k := newKit(t)
	// Pre-seed an old directory.
	old := filepath.Join(k.dest, "20250101-100000")
	if err := os.MkdirAll(old, 0o700); err != nil {
		t.Fatal(err)
	}
	r := k.newRunner(t, 7*24*time.Hour) // 7 days
	res, err := r.Run(context.Background())
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	if len(res.Pruned) == 0 {
		t.Error("expected old dir to be pruned")
	}
	if _, err := os.Stat(old); !errors.Is(err, os.ErrNotExist) {
		t.Errorf("old dir should be gone: %v", err)
	}
}

func TestRunner_DBPathMissing(t *testing.T) {
	k := newKit(t)
	db, err := persistence.Open(k.dbPath)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	_, err = backup.NewRunner(backup.Config{
		DB: db, DBPath: "", DestRoot: k.dest, Sink: k.sink, Clock: k.clock, Actor: "system",
	})
	if err == nil {
		t.Fatal("want error on missing db path")
	}
}

func TestRunner_DestRootMissing(t *testing.T) {
	k := newKit(t)
	db, err := persistence.Open(k.dbPath)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	_, err = backup.NewRunner(backup.Config{
		DB: db, DBPath: k.dbPath, DestRoot: "", Sink: k.sink, Clock: k.clock, Actor: "system",
	})
	if err == nil {
		t.Fatal("want error on missing dest")
	}
}

func TestRunner_BadActor(t *testing.T) {
	k := newKit(t)
	db, err := persistence.Open(k.dbPath)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	_, err = backup.NewRunner(backup.Config{
		DB: db, DBPath: k.dbPath, DestRoot: k.dest, Sink: k.sink, Clock: k.clock, Actor: "",
	})
	if err == nil {
		t.Fatal("want error on bad actor")
	}
}

func TestRunner_DefaultRetention(t *testing.T) {
	k := newKit(t)
	db, err := persistence.Open(k.dbPath)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	r, err := backup.NewRunner(backup.Config{
		DB:       db,
		DBPath:   k.dbPath,
		DestRoot: k.dest,
		Sink:     k.sink,
		Clock:    k.clock,
		Actor:    observability.Actor("system"),
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := r.Run(context.Background()); err != nil {
		t.Errorf("run: %v", err)
	}
}

func TestRunner_MkdirFails(t *testing.T) {
	k := newKit(t)
	// destRoot is a file rather than a directory → MkdirAll inside fails.
	bad := filepath.Join(k.dir, "destfile")
	if err := os.WriteFile(bad, []byte{}, 0o600); err != nil {
		t.Fatal(err)
	}
	db, err := persistence.Open(k.dbPath)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	r, err := backup.NewRunner(backup.Config{
		DB: db, DBPath: k.dbPath, DestRoot: bad, Sink: k.sink, Clock: k.clock,
		Actor: observability.Actor("system"),
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := r.Run(context.Background()); err == nil {
		t.Fatal("expected mkdir error")
	}
	// admin.backup_failed event emitted.
	et := observability.EventType("admin.backup_failed")
	evs, err := k.events.Find(context.Background(), observability.EventQueryFilter{
		EventType: &et, Limit: 10,
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(evs) == 0 {
		t.Error("admin.backup_failed not emitted")
	}
}

func TestRunner_NonTimestampDirsIgnored(t *testing.T) {
	k := newKit(t)
	// Pre-seed a directory with a non-timestamp name.
	if err := os.MkdirAll(filepath.Join(k.dest, "manual-snapshot"), 0o700); err != nil {
		t.Fatal(err)
	}
	r := k.newRunner(t, 1*time.Hour)
	res, err := r.Run(context.Background())
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	for _, p := range res.Pruned {
		if filepath.Base(p) == "manual-snapshot" {
			t.Error("non-timestamp dir should not be pruned")
		}
	}
	if _, err := os.Stat(filepath.Join(k.dest, "manual-snapshot")); err != nil {
		t.Errorf("manual dir gone: %v", err)
	}
}
