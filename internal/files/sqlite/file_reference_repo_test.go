package sqlite

import (
	"context"
	"testing"
	"time"

	"github.com/oopslink/agent-center/internal/files"
	"github.com/oopslink/agent-center/internal/idgen"
	"github.com/oopslink/agent-center/internal/persistence"
)

func newDB(t *testing.T) (*FileReferenceRepo, *BlobMetadataRepo) {
	t.Helper()
	db, err := persistence.Open(persistence.MemoryDSN())
	if err != nil {
		t.Fatal(err)
	}
	if err := persistence.NewMigrator(db).Up(context.Background()); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	return NewFileReferenceRepo(db), NewBlobMetadataRepo(db)
}

func mkURI(t *testing.T) files.FileURI {
	t.Helper()
	u, err := files.NewFileURI(idgen.MustNewULID())
	if err != nil {
		t.Fatal(err)
	}
	return u
}

func TestFileReferenceRepo_RoundTripAndCount(t *testing.T) {
	refRepo, _ := newDB(t)
	ctx := context.Background()
	uri := mkURI(t)
	now := time.Now().UTC()

	// Two references to the SAME blob from different scopes (sharing = +ref).
	r1 := files.FileReference{ID: idgen.MustNewULID(), FileURI: uri, Scope: files.ScopeConversation, ScopeID: "conv-1", Filename: "a.png", MimeType: "image/png", SizeBytes: 10, CreatedBy: "user:x", CreatedAt: now}
	r2 := files.FileReference{ID: idgen.MustNewULID(), FileURI: uri, Scope: files.ScopeTask, ScopeID: "task-1", Filename: "a.png", MimeType: "image/png", SizeBytes: 10, CreatedBy: "user:x", CreatedAt: now}
	if err := refRepo.Save(ctx, r1); err != nil {
		t.Fatal(err)
	}
	if err := refRepo.Save(ctx, r2); err != nil {
		t.Fatal(err)
	}

	if n, err := refRepo.CountLiveByURI(ctx, uri); err != nil || n != 2 {
		t.Fatalf("CountLiveByURI = %d, %v; want 2", n, err)
	}
	byScope, err := refRepo.FindByScope(ctx, files.ScopeConversation, "conv-1")
	if err != nil || len(byScope) != 1 || byScope[0].ID != r1.ID {
		t.Fatalf("FindByScope = %+v, %v", byScope, err)
	}

	// Soft-delete one reference: blob stays referenced (GC must not reap).
	if err := refRepo.SoftDelete(ctx, r1.ID, now.Add(time.Second)); err != nil {
		t.Fatal(err)
	}
	if n, err := refRepo.CountLiveByURI(ctx, uri); err != nil || n != 1 {
		t.Fatalf("after soft-delete CountLiveByURI = %d, %v; want 1", n, err)
	}
	got, err := refRepo.FindByID(ctx, r1.ID)
	if err != nil || got.IsLive() {
		t.Fatalf("r1 should be soft-deleted: %+v %v", got, err)
	}

	// Deleting the last live reference drops the count to zero (the phase-D GC
	// reaps the blob when this is zero past the grace period).
	if err := refRepo.SoftDelete(ctx, r2.ID, now.Add(time.Second)); err != nil {
		t.Fatal(err)
	}
	if n, _ := refRepo.CountLiveByURI(ctx, uri); n != 0 {
		t.Fatalf("want 0 live refs, got %d", n)
	}
}

func TestBlobMetadataRepo_RoundTrip(t *testing.T) {
	_, blobRepo := newDB(t)
	ctx := context.Background()
	m := files.BlobMetadata{ULID: idgen.MustNewULID(), SizeBytes: 42, ContentSHA256: "deadbeef", CreatedAt: time.Now().UTC()}
	if err := blobRepo.PutMetadata(ctx, m); err != nil {
		t.Fatal(err)
	}
	got, err := blobRepo.GetMetadata(ctx, m.ULID)
	if err != nil || got.ULID != m.ULID || got.SizeBytes != 42 || got.ContentSHA256 != "deadbeef" {
		t.Fatalf("GetMetadata = %+v, %v", got, err)
	}
	if _, err := blobRepo.GetMetadata(ctx, idgen.MustNewULID()); err != files.ErrBlobNotFound {
		t.Fatalf("want ErrBlobNotFound, got %v", err)
	}
	// Write-once: re-putting the same ULID is rejected.
	if err := blobRepo.PutMetadata(ctx, m); err == nil {
		t.Fatalf("expected write-once conflict on duplicate ULID")
	}
}
