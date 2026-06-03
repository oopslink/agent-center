package sqlite

import (
	"context"
	"testing"
	"time"

	"github.com/oopslink/agent-center/internal/files"
	"github.com/oopslink/agent-center/internal/idgen"
	"github.com/oopslink/agent-center/internal/persistence"
)

func newSessRepo(t *testing.T) *FileTransferSessionRepo {
	t.Helper()
	db, err := persistence.Open(persistence.MemoryDSN())
	if err != nil {
		t.Fatal(err)
	}
	if err := persistence.NewMigrator(db).Up(context.Background()); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	return NewFileTransferSessionRepo(db)
}

func mkUpload(t *testing.T, now time.Time) *files.FileTransferSession {
	t.Helper()
	s, err := files.NewUploadSession(files.NewUploadInput{
		FileULID:    idgen.MustNewULID(),
		SessionULID: idgen.MustNewULID(),
		ContentType: "text/plain",
		Size:        10,
		Scope:       files.ScopeTask,
		ScopeID:     "task-1",
		CreatedBy:   "user:x",
		CreatedAt:   now,
	})
	if err != nil {
		t.Fatal(err)
	}
	return s
}

func TestTransferSessionRepo_RoundTrip(t *testing.T) {
	repo := newSessRepo(t)
	ctx := context.Background()
	now := time.Now().UTC().Truncate(time.Millisecond)
	sess := mkUpload(t, now)

	if err := repo.Save(ctx, sess); err != nil {
		t.Fatal(err)
	}

	got, err := repo.FindByID(ctx, sess.ID())
	if err != nil {
		t.Fatal(err)
	}
	if got.ID() != sess.ID() || got.FileURI() != sess.FileURI() || got.TransferURI() != sess.TransferURI() {
		t.Fatalf("FindByID mismatch: %+v", got)
	}
	if got.Direction() != files.DirectionUpload || got.Status() != files.StatusOpen {
		t.Fatalf("dir/status mismatch: %v %v", got.Direction(), got.Status())
	}
	if got.ContentType() != "text/plain" || got.Size() != 10 || got.Scope() != files.ScopeTask || got.ScopeID() != "task-1" {
		t.Fatalf("metadata mismatch: %+v", got)
	}
	if got.CreatedBy() != "user:x" || !got.CreatedAt().Equal(now) || !got.ExpiresAt().Equal(now.Add(files.DefaultTransferTTL)) {
		t.Fatalf("times/createdBy mismatch: %v %v %v", got.CreatedBy(), got.CreatedAt(), got.ExpiresAt())
	}

	byURI, err := repo.FindByTransferURI(ctx, sess.TransferURI())
	if err != nil || byURI.ID() != sess.ID() {
		t.Fatalf("FindByTransferURI = %+v, %v", byURI, err)
	}

	// Complete + Update round-trips status + sha256 + size.
	if err := sess.Complete("deadbeef", 42, now.Add(time.Minute)); err != nil {
		t.Fatal(err)
	}
	if err := repo.Update(ctx, sess); err != nil {
		t.Fatal(err)
	}
	got, _ = repo.FindByID(ctx, sess.ID())
	if got.Status() != files.StatusCompleted || got.SHA256() != "deadbeef" || got.Size() != 42 {
		t.Fatalf("after update: %+v", got)
	}
}

func TestTransferSessionRepo_NotFound(t *testing.T) {
	repo := newSessRepo(t)
	ctx := context.Background()
	if _, err := repo.FindByID(ctx, "missing"); err != files.ErrTransferSessionNotFound {
		t.Fatalf("FindByID want ErrTransferSessionNotFound, got %v", err)
	}
	if _, err := repo.FindByTransferURI(ctx, "ac://transfers/none"); err != files.ErrTransferSessionNotFound {
		t.Fatalf("FindByTransferURI want ErrTransferSessionNotFound, got %v", err)
	}
	// Update of an absent row → ErrTransferSessionNotFound.
	sess := mkUpload(t, time.Now().UTC())
	if err := repo.Update(ctx, sess); err != files.ErrTransferSessionNotFound {
		t.Fatalf("Update absent want ErrTransferSessionNotFound, got %v", err)
	}
}

func TestTransferSessionRepo_ListExpired(t *testing.T) {
	repo := newSessRepo(t)
	ctx := context.Background()
	base := time.Date(2026, 5, 29, 12, 0, 0, 0, time.UTC)

	// Open + expired-by-time (expires at base+1h).
	s1 := mkUpload(t, base)
	// Open but later expiry.
	s2 := mkUpload(t, base.Add(10*time.Hour))
	// Open + expired but then completed → excluded (status != open).
	s3 := mkUpload(t, base)
	for _, s := range []*files.FileTransferSession{s1, s2, s3} {
		if err := repo.Save(ctx, s); err != nil {
			t.Fatal(err)
		}
	}
	_ = s3.Complete("x", 1, base.Add(time.Minute))
	if err := repo.Update(ctx, s3); err != nil {
		t.Fatal(err)
	}

	// Cutoff after s1's expiry (base+1h) but before s2's.
	got, err := repo.ListExpired(ctx, base.Add(2*time.Hour))
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || got[0].ID() != s1.ID() {
		t.Fatalf("ListExpired = %d sessions %+v; want only s1", len(got), got)
	}
}

// TestTransferSessionRepo_ListOpen: ListOpen returns ONLY live in-flight sessions
// (status=open AND expires_at > now). Expired-open, completed, and canceled are
// all excluded. No LIMIT — all live ones come back (the #126 no-truncation rule).
func TestTransferSessionRepo_ListOpen(t *testing.T) {
	repo := newSessRepo(t)
	ctx := context.Background()
	now := time.Now().UTC().Truncate(time.Millisecond)

	mk := func(scopeID string, createdAt time.Time, ttl time.Duration) *files.FileTransferSession {
		s, err := files.NewUploadSession(files.NewUploadInput{
			FileULID: idgen.MustNewULID(), SessionULID: idgen.MustNewULID(),
			ContentType: "text/plain", Size: 1, Scope: files.ScopeProject, ScopeID: scopeID,
			CreatedBy: "user:x", CreatedAt: createdAt, TTL: ttl,
		})
		if err != nil {
			t.Fatal(err)
		}
		if err := repo.Save(ctx, s); err != nil {
			t.Fatal(err)
		}
		return s
	}

	live1 := mk("p-live-1", now, time.Hour)        // open + future expiry → included
	live2 := mk("p-live-2", now, 24*time.Hour)     // open + future expiry → included
	mk("p-expired", now.Add(-2*time.Hour), time.Hour) // open but expired → excluded

	completed := mk("p-completed", now, time.Hour)
	if err := completed.Complete("sha", 1, now); err != nil {
		t.Fatal(err)
	}
	if err := repo.Update(ctx, completed); err != nil {
		t.Fatal(err)
	}
	canceled := mk("p-canceled", now, time.Hour)
	if err := canceled.Cancel(now); err != nil {
		t.Fatal(err)
	}
	if err := repo.Update(ctx, canceled); err != nil {
		t.Fatal(err)
	}

	got, err := repo.ListOpen(ctx, now)
	if err != nil {
		t.Fatal(err)
	}
	ids := map[string]bool{}
	for _, s := range got {
		ids[s.ID()] = true
		if s.Status() != files.StatusOpen {
			t.Fatalf("ListOpen returned non-open session: %v", s.Status())
		}
	}
	if len(got) != 2 || !ids[live1.ID()] || !ids[live2.ID()] {
		t.Fatalf("ListOpen = %d sessions, want exactly the 2 live ones (expired/completed/canceled excluded)", len(got))
	}
}
