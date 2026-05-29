package service

import (
	"bytes"
	"context"
	"io"
	"strings"
	"testing"

	"github.com/oopslink/agent-center/internal/blobstore"
	"github.com/oopslink/agent-center/internal/clock"
	"github.com/oopslink/agent-center/internal/files"
	filessqlite "github.com/oopslink/agent-center/internal/files/sqlite"
	"github.com/oopslink/agent-center/internal/idgen"
	"github.com/oopslink/agent-center/internal/persistence"
)

func newService(t *testing.T) (*Service, blobstore.BlobStore) {
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
	// Root="" → ObjectPath yields a blobstore-relative path (objects/h1/h2/ulid).
	resolver := files.NewLocalResolver("")
	svc := New(Deps{
		DB:        db,
		Sessions:  filessqlite.NewFileTransferSessionRepo(db),
		Resolver:  resolver,
		BlobStore: store,
		IDGen:     idgen.NewGenerator(clock.SystemClock{}),
		Clock:     clock.SystemClock{},
	})
	return svc, store
}

func TestService_UploadFlow_EndToEnd(t *testing.T) {
	svc, _ := newService(t)
	ctx := context.Background()
	data := []byte("hello transfer world")

	sess, err := svc.CreateUploadSession(ctx, CreateUploadCmd{
		ContentType: "text/plain",
		Size:        int64(len(data)),
		Scope:       files.ScopeConversation,
		ScopeID:     "conv-1",
		CreatedBy:   "user:x",
	})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(sess.FileURI().String(), "ac://files/") {
		t.Fatalf("fileURI = %q", sess.FileURI())
	}
	if !strings.HasPrefix(sess.TransferURI(), "ac://transfers/") {
		t.Fatalf("transferURI = %q", sess.TransferURI())
	}

	if err := svc.WriteBlob(ctx, sess.TransferURI(), bytes.NewReader(data), int64(len(data))); err != nil {
		t.Fatalf("WriteBlob: %v", err)
	}

	// Write-once: a second write to the same session is rejected.
	if err := svc.WriteBlob(ctx, sess.TransferURI(), bytes.NewReader(data), int64(len(data))); err != ErrBlobAlreadyExists {
		t.Fatalf("second WriteBlob want ErrBlobAlreadyExists, got %v", err)
	}

	if err := svc.CompleteUpload(ctx, sess.TransferURI(), "sha-xyz", int64(len(data))); err != nil {
		t.Fatalf("CompleteUpload: %v", err)
	}

	// OpenBlob returns the same bytes.
	rc, err := svc.OpenBlob(ctx, sess.FileURI())
	if err != nil {
		t.Fatalf("OpenBlob: %v", err)
	}
	got, _ := io.ReadAll(rc)
	_ = rc.Close()
	if !bytes.Equal(got, data) {
		t.Fatalf("OpenBlob content = %q want %q", got, data)
	}

	// CompleteUpload again → illegal (session already completed).
	if err := svc.CompleteUpload(ctx, sess.TransferURI(), "sha-xyz", int64(len(data))); err != files.ErrIllegalTransferState {
		t.Fatalf("re-complete want ErrIllegalTransferState, got %v", err)
	}
}

func TestService_DownloadFlow(t *testing.T) {
	svc, _ := newService(t)
	ctx := context.Background()
	data := []byte("downloadable")

	up, err := svc.CreateUploadSession(ctx, CreateUploadCmd{ContentType: "application/octet-stream", Size: int64(len(data)), CreatedBy: "u"})
	if err != nil {
		t.Fatal(err)
	}
	if err := svc.WriteBlob(ctx, up.TransferURI(), bytes.NewReader(data), int64(len(data))); err != nil {
		t.Fatal(err)
	}
	if err := svc.CompleteUpload(ctx, up.TransferURI(), "s", int64(len(data))); err != nil {
		t.Fatal(err)
	}

	dl, err := svc.CreateDownloadSession(ctx, up.FileURI(), "user:y")
	if err != nil {
		t.Fatalf("CreateDownloadSession: %v", err)
	}
	if dl.Direction() != files.DirectionDownload || dl.FileURI() != up.FileURI() {
		t.Fatalf("download session: %+v", dl)
	}

	rc, err := svc.OpenBlob(ctx, up.FileURI())
	if err != nil {
		t.Fatal(err)
	}
	got, _ := io.ReadAll(rc)
	_ = rc.Close()
	if !bytes.Equal(got, data) {
		t.Fatalf("content = %q", got)
	}
}

func TestService_DownloadMissingBlob(t *testing.T) {
	svc, _ := newService(t)
	ctx := context.Background()
	uri, _ := files.NewFileURI(idgen.MustNewULID())
	if _, err := svc.CreateDownloadSession(ctx, uri, "u"); err != ErrBlobNotFound {
		t.Fatalf("want ErrBlobNotFound, got %v", err)
	}
	if _, err := svc.OpenBlob(ctx, uri); err != ErrBlobNotFound {
		t.Fatalf("OpenBlob want ErrBlobNotFound, got %v", err)
	}
}

func TestService_WriteBlob_Guards(t *testing.T) {
	svc, _ := newService(t)
	ctx := context.Background()

	// Unknown transfer URI.
	if err := svc.WriteBlob(ctx, "ac://transfers/none", strings.NewReader("x"), 1); err != files.ErrTransferSessionNotFound {
		t.Fatalf("want ErrTransferSessionNotFound, got %v", err)
	}

	// WriteBlob to a download session → ErrNotUploadSession.
	data := []byte("d")
	up, _ := svc.CreateUploadSession(ctx, CreateUploadCmd{Size: 1, CreatedBy: "u"})
	_ = svc.WriteBlob(ctx, up.TransferURI(), bytes.NewReader(data), 1)
	_ = svc.CompleteUpload(ctx, up.TransferURI(), "s", 1)
	dl, _ := svc.CreateDownloadSession(ctx, up.FileURI(), "u")
	if err := svc.WriteBlob(ctx, dl.TransferURI(), strings.NewReader("x"), 1); err != ErrNotUploadSession {
		t.Fatalf("want ErrNotUploadSession, got %v", err)
	}

	// WriteBlob to a canceled (not open) session → ErrSessionNotOpen.
	up2, _ := svc.CreateUploadSession(ctx, CreateUploadCmd{Size: 1, CreatedBy: "u"})
	if err := svc.CancelSession(ctx, up2.TransferURI()); err != nil {
		t.Fatal(err)
	}
	if err := svc.WriteBlob(ctx, up2.TransferURI(), strings.NewReader("x"), 1); err != ErrSessionNotOpen {
		t.Fatalf("want ErrSessionNotOpen, got %v", err)
	}
}

func TestService_CancelAndExpire(t *testing.T) {
	svc, _ := newService(t)
	ctx := context.Background()

	c, _ := svc.CreateUploadSession(ctx, CreateUploadCmd{Size: 0, CreatedBy: "u"})
	if err := svc.CancelSession(ctx, c.TransferURI()); err != nil {
		t.Fatal(err)
	}
	// Canceling again → illegal.
	if err := svc.CancelSession(ctx, c.TransferURI()); err != files.ErrIllegalTransferState {
		t.Fatalf("re-cancel want ErrIllegalTransferState, got %v", err)
	}

	e, _ := svc.CreateUploadSession(ctx, CreateUploadCmd{Size: 0, CreatedBy: "u"})
	if err := svc.ExpireSession(ctx, e.TransferURI()); err != nil {
		t.Fatal(err)
	}
	if err := svc.ExpireSession(ctx, e.TransferURI()); err != files.ErrIllegalTransferState {
		t.Fatalf("re-expire want ErrIllegalTransferState, got %v", err)
	}
}
