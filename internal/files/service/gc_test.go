package service

import (
	"bytes"
	"context"
	"testing"
	"time"

	"github.com/oopslink/agent-center/internal/blobstore"
	"github.com/oopslink/agent-center/internal/clock"
	"github.com/oopslink/agent-center/internal/files"
	filessqlite "github.com/oopslink/agent-center/internal/files/sqlite"
	"github.com/oopslink/agent-center/internal/idgen"
	"github.com/oopslink/agent-center/internal/persistence"
)

const testGrace = 7 * 24 * time.Hour

// gcFixture bundles the wired GC service + the raw repos/store the tests poke.
type gcFixture struct {
	svc      *Service
	store    blobstore.BlobStore
	resolver files.Resolver
	refs     *filessqlite.FileReferenceRepo
	blobMeta *filessqlite.BlobMetadataRepo
	clk      *clock.FakeClock
}

func newGCFixture(t *testing.T) *gcFixture {
	t.Helper()
	db, err := persistence.Open(persistence.MemoryDSN())
	if err != nil {
		t.Fatal(err)
	}
	if err := persistence.NewMigrator(db).Up(context.Background()); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })

	store, err := blobstore.NewLocalDir(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	resolver := files.NewLocalResolver("")
	refs := filessqlite.NewFileReferenceRepo(db)
	blobMeta := filessqlite.NewBlobMetadataRepo(db)
	clk := clock.NewFakeClock(time.Date(2026, 5, 1, 0, 0, 0, 0, time.UTC))

	svc := New(Deps{
		DB:         db,
		Sessions:   filessqlite.NewFileTransferSessionRepo(db),
		References: refs,
		Resolver:   resolver,
		BlobStore:  store,
		IDGen:      idgen.NewGenerator(clk),
		Clock:      clk,
	}).SetGCRepo(blobMeta)

	return &gcFixture{svc: svc, store: store, resolver: resolver, refs: refs, blobMeta: blobMeta, clk: clk}
}

// putBlobWithBytes writes both the metadata row (at createdAt) and the bytes.
func (f *gcFixture) putBlobWithBytes(t *testing.T, createdAt time.Time) (string, files.FileURI) {
	t.Helper()
	ctx := context.Background()
	ulid := idgen.MustNewULID()
	uri, err := files.NewFileURI(ulid)
	if err != nil {
		t.Fatal(err)
	}
	if err := f.blobMeta.PutMetadata(ctx, files.BlobMetadata{ULID: ulid, SizeBytes: 5, CreatedAt: createdAt}); err != nil {
		t.Fatal(err)
	}
	rel, err := f.resolver.ObjectPath(uri)
	if err != nil {
		t.Fatal(err)
	}
	if err := f.store.Put(ctx, rel, bytes.NewReader([]byte("hello")), 5); err != nil {
		t.Fatal(err)
	}
	return ulid, uri
}

func (f *gcFixture) exists(t *testing.T, uri files.FileURI) bool {
	t.Helper()
	rel, err := f.resolver.ObjectPath(uri)
	if err != nil {
		t.Fatal(err)
	}
	ok, err := f.store.Exists(context.Background(), rel)
	if err != nil {
		t.Fatal(err)
	}
	return ok
}

func (f *gcFixture) metaGone(t *testing.T, ulid string) bool {
	t.Helper()
	_, err := f.blobMeta.GetMetadata(context.Background(), ulid)
	return err == files.ErrBlobNotFound
}

// TestRunGCOnce_CollectsUnreferencedPastGrace covers the core matrix: an
// unreferenced old blob is collected; a live-referenced blob and a recent
// unreferenced blob are not.
func TestRunGCOnce_CollectsUnreferencedPastGrace(t *testing.T) {
	f := newGCFixture(t)
	ctx := context.Background()
	now := f.clk.Now()
	old := now.Add(-30 * 24 * time.Hour)

	// (1) unreferenced, old → collected.
	oldULID, oldURI := f.putBlobWithBytes(t, old)
	// (2) live reference → kept.
	liveULID, liveURI := f.putBlobWithBytes(t, old)
	if _, err := f.svc.AddReference(ctx, AddReferenceCmd{FileURI: liveURI, Scope: files.ScopeTask, ScopeID: "task-1", CreatedBy: "u"}); err != nil {
		t.Fatal(err)
	}
	// (3) unreferenced, recent (within grace) → kept.
	recentULID, recentURI := f.putBlobWithBytes(t, now.Add(-time.Hour))

	collected, err := f.svc.RunGCOnce(ctx, testGrace)
	if err != nil {
		t.Fatal(err)
	}
	if collected != 1 {
		t.Fatalf("collected = %d, want 1", collected)
	}

	if f.exists(t, oldURI) || !f.metaGone(t, oldULID) {
		t.Errorf("old unreferenced blob should be reaped (file+metadata gone)")
	}
	if !f.exists(t, liveURI) || f.metaGone(t, liveULID) {
		t.Errorf("live-referenced blob must be kept")
	}
	if !f.exists(t, recentURI) || f.metaGone(t, recentULID) {
		t.Errorf("recent unreferenced blob (within grace) must be kept")
	}
}

// TestRunGCOnce_SoftDeletedReference covers grace measured from the LAST
// reference removal.
func TestRunGCOnce_SoftDeletedReference(t *testing.T) {
	f := newGCFixture(t)
	ctx := context.Background()
	now := f.clk.Now()
	old := now.Add(-30 * 24 * time.Hour)

	// Referenced then soft-deleted long ago → collected.
	delOldULID, delOldURI := f.putBlobWithBytes(t, old)
	refOld, err := f.svc.AddReference(ctx, AddReferenceCmd{FileURI: delOldURI, Scope: files.ScopeTask, ScopeID: "task-a", CreatedBy: "u"})
	if err != nil {
		t.Fatal(err)
	}
	// Soft-delete with an OLD timestamp (drive the clock back temporarily).
	f.clk.Set(old.Add(time.Hour))
	if err := f.svc.SoftDeleteReference(ctx, refOld.ID); err != nil {
		t.Fatal(err)
	}
	f.clk.Set(now)

	// Referenced then soft-deleted just now → kept (within grace).
	delNewULID, delNewURI := f.putBlobWithBytes(t, old)
	refNew, err := f.svc.AddReference(ctx, AddReferenceCmd{FileURI: delNewURI, Scope: files.ScopeTask, ScopeID: "task-b", CreatedBy: "u"})
	if err != nil {
		t.Fatal(err)
	}
	if err := f.svc.SoftDeleteReference(ctx, refNew.ID); err != nil { // at now
		t.Fatal(err)
	}

	if _, err := f.svc.RunGCOnce(ctx, testGrace); err != nil {
		t.Fatal(err)
	}

	if f.exists(t, delOldURI) || !f.metaGone(t, delOldULID) {
		t.Errorf("blob whose last ref was removed past grace should be reaped")
	}
	if !f.exists(t, delNewURI) || f.metaGone(t, delNewULID) {
		t.Errorf("blob whose ref was removed within grace must be kept")
	}
}

// TestCollectOne_RaceRecheck is the PM-mandated safety test: a reference added
// AFTER candidate selection but before the per-candidate delete is seen by the
// in-tx CountLiveByURI re-check, so the blob is NOT deleted.
func TestCollectOne_RaceRecheck(t *testing.T) {
	f := newGCFixture(t)
	ctx := context.Background()
	now := f.clk.Now()
	old := now.Add(-30 * 24 * time.Hour)

	blobULID, blobURI := f.putBlobWithBytes(t, old)

	// Simulate candidate selection (the blob currently has zero live refs).
	cands, err := f.blobMeta.ListCollectable(ctx, now.Add(-testGrace), 100)
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, c := range cands {
		if c == blobULID {
			found = true
		}
	}
	if !found {
		t.Fatalf("blob should be a candidate before the race")
	}

	// RACE: a reference is added between selection and the per-candidate delete.
	if _, err := f.svc.AddReference(ctx, AddReferenceCmd{FileURI: blobURI, Scope: files.ScopeTask, ScopeID: "task-race", CreatedBy: "u"}); err != nil {
		t.Fatal(err)
	}

	// Now drive the per-candidate path. The in-tx re-check sees 1 live ref → skip.
	ok, err := f.svc.collectOne(ctx, blobULID)
	if err != nil {
		t.Fatal(err)
	}
	if ok {
		t.Fatalf("collectOne should SKIP a blob that gained a live reference")
	}
	if !f.exists(t, blobURI) || f.metaGone(t, blobULID) {
		t.Errorf("raced blob must NOT be deleted (file + metadata intact)")
	}
}

// TestRunGCOnce_ExpiresSessionThenCollectsOrphan covers an abandoned upload: an
// expired open session whose blob bytes were written but never referenced
// becomes a collectable never-referenced orphan.
func TestRunGCOnce_ExpiresSessionThenCollectsOrphan(t *testing.T) {
	f := newGCFixture(t)
	ctx := context.Background()

	// Create the upload session in the PAST so it is already expired (1h TTL),
	// and write its blob bytes + metadata, but never add a reference.
	past := f.clk.Now().Add(-30 * 24 * time.Hour)
	f.clk.Set(past)
	sess, err := f.svc.CreateUploadSession(ctx, CreateUploadCmd{ContentType: "text/plain", Size: 5, CreatedBy: "u"})
	if err != nil {
		t.Fatal(err)
	}
	if err := f.svc.WriteBlob(ctx, sess.TransferURI(), bytes.NewReader([]byte("hello")), 5); err != nil {
		t.Fatal(err)
	}
	// Record blob metadata at the (old) creation time so it is past grace.
	if err := f.blobMeta.PutMetadata(ctx, files.BlobMetadata{ULID: sess.FileURI().ULID(), SizeBytes: 5, CreatedAt: past}); err != nil {
		t.Fatal(err)
	}
	f.clk.Set(past.Add(40 * 24 * time.Hour)) // now: session long expired, blob past grace

	if _, err := f.svc.RunGCOnce(ctx, testGrace); err != nil {
		t.Fatal(err)
	}

	// The session was expired by the GC pass.
	reloaded, err := f.svc.sessions.FindByID(ctx, sess.ID())
	if err != nil {
		t.Fatal(err)
	}
	if reloaded.Status() != files.StatusExpired {
		t.Errorf("session status = %q, want expired", reloaded.Status())
	}
	// The orphan blob was reaped.
	if f.exists(t, sess.FileURI()) || !f.metaGone(t, sess.FileURI().ULID()) {
		t.Errorf("orphan blob from abandoned upload should be reaped")
	}
}

// TestRunGCOnce_NoGCRepo guards the explicit error when the GC repo is unwired.
func TestRunGCOnce_NoGCRepo(t *testing.T) {
	f := newGCFixture(t)
	f.svc.gcRepo = nil
	if _, err := f.svc.RunGCOnce(context.Background(), testGrace); err == nil {
		t.Fatalf("RunGCOnce without GC repo should error")
	}
}
