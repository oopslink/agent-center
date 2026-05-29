package sqlite

import (
	"context"
	"testing"
	"time"

	"github.com/oopslink/agent-center/internal/files"
	"github.com/oopslink/agent-center/internal/idgen"
)

// putBlob inserts a blob_metadata row with the given created_at and returns its
// ULID + FileURI.
func putBlob(t *testing.T, repo *BlobMetadataRepo, createdAt time.Time) (string, files.FileURI) {
	t.Helper()
	ulid := idgen.MustNewULID()
	if err := repo.PutMetadata(context.Background(), files.BlobMetadata{
		ULID:      ulid,
		SizeBytes: 4,
		CreatedAt: createdAt,
	}); err != nil {
		t.Fatal(err)
	}
	uri, err := files.NewFileURI(ulid)
	if err != nil {
		t.Fatal(err)
	}
	return ulid, uri
}

func contains(ss []string, want string) bool {
	for _, s := range ss {
		if s == want {
			return true
		}
	}
	return false
}

func TestBlobMetadataRepo_ListCollectable(t *testing.T) {
	refRepo, blobRepo := newDB(t)
	ctx := context.Background()
	now := time.Now().UTC()
	old := now.Add(-30 * 24 * time.Hour)
	cutoff := now.Add(-7 * 24 * time.Hour)

	// (a) orphan, never referenced, created long ago → collectable.
	orphanOld, _ := putBlob(t, blobRepo, old)
	// (b) orphan, never referenced, created recently → NOT collectable.
	orphanNew, _ := putBlob(t, blobRepo, now)
	// (c) blob with a live reference (old created_at) → NOT collectable.
	liveULID, liveURI := putBlob(t, blobRepo, old)
	if err := refRepo.Save(ctx, files.FileReference{
		ID: idgen.MustNewULID(), FileURI: liveURI, Scope: files.ScopeTask,
		ScopeID: "task-1", CreatedBy: "u", CreatedAt: old,
	}); err != nil {
		t.Fatal(err)
	}
	// (d) once-referenced then soft-deleted long ago → collectable (grace from delete).
	delOldULID, delOldURI := putBlob(t, blobRepo, old)
	refDelOld := files.FileReference{ID: idgen.MustNewULID(), FileURI: delOldURI, Scope: files.ScopeTask, ScopeID: "task-2", CreatedBy: "u", CreatedAt: old}
	if err := refRepo.Save(ctx, refDelOld); err != nil {
		t.Fatal(err)
	}
	if err := refRepo.SoftDelete(ctx, refDelOld.ID, old.Add(time.Hour)); err != nil {
		t.Fatal(err)
	}
	// (e) once-referenced then soft-deleted RECENTLY → NOT collectable (within grace).
	delNewULID, delNewURI := putBlob(t, blobRepo, old)
	refDelNew := files.FileReference{ID: idgen.MustNewULID(), FileURI: delNewURI, Scope: files.ScopeTask, ScopeID: "task-3", CreatedBy: "u", CreatedAt: old}
	if err := refRepo.Save(ctx, refDelNew); err != nil {
		t.Fatal(err)
	}
	if err := refRepo.SoftDelete(ctx, refDelNew.ID, now); err != nil {
		t.Fatal(err)
	}

	got, err := blobRepo.ListCollectable(ctx, cutoff, 100)
	if err != nil {
		t.Fatal(err)
	}

	if !contains(got, orphanOld) {
		t.Errorf("orphanOld should be collectable, got %v", got)
	}
	if contains(got, orphanNew) {
		t.Errorf("orphanNew is within grace, must NOT be collectable")
	}
	if contains(got, liveULID) {
		t.Errorf("liveULID has a live reference, must NOT be collectable")
	}
	if !contains(got, delOldULID) {
		t.Errorf("delOldULID (deleted past grace) should be collectable, got %v", got)
	}
	if contains(got, delNewULID) {
		t.Errorf("delNewULID (deleted within grace) must NOT be collectable")
	}
}

func TestBlobMetadataRepo_ListCollectable_LimitOrdering(t *testing.T) {
	_, blobRepo := newDB(t)
	ctx := context.Background()
	old := time.Now().UTC().Add(-30 * 24 * time.Hour)
	cutoff := time.Now().UTC().Add(-7 * 24 * time.Hour)

	for i := 0; i < 5; i++ {
		putBlob(t, blobRepo, old)
	}
	got, err := blobRepo.ListCollectable(ctx, cutoff, 3)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 3 {
		t.Fatalf("limit=3 want 3 rows, got %d", len(got))
	}
}

func TestBlobMetadataRepo_DeleteMetadata(t *testing.T) {
	_, blobRepo := newDB(t)
	ctx := context.Background()
	ulid, _ := putBlob(t, blobRepo, time.Now().UTC())

	if _, err := blobRepo.GetMetadata(ctx, ulid); err != nil {
		t.Fatalf("GetMetadata before delete: %v", err)
	}
	if err := blobRepo.DeleteMetadata(ctx, ulid); err != nil {
		t.Fatalf("DeleteMetadata: %v", err)
	}
	if _, err := blobRepo.GetMetadata(ctx, ulid); err != files.ErrBlobNotFound {
		t.Fatalf("after delete want ErrBlobNotFound, got %v", err)
	}
	// Idempotent: deleting again is a no-op (no error).
	if err := blobRepo.DeleteMetadata(ctx, ulid); err != nil {
		t.Fatalf("idempotent DeleteMetadata: %v", err)
	}
}
