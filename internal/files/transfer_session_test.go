package files

import (
	"strings"
	"testing"
	"time"

	"github.com/oopslink/agent-center/internal/idgen"
)

func mkSessIDs(t *testing.T) (fileULID, sessULID string) {
	t.Helper()
	return idgen.MustNewULID(), idgen.MustNewULID()
}

func TestNewUploadSession_MintsURIsOpen(t *testing.T) {
	fileULID, sessULID := mkSessIDs(t)
	now := time.Date(2026, 5, 29, 12, 0, 0, 0, time.UTC)
	s, err := NewUploadSession(NewUploadInput{
		FileULID:    fileULID,
		SessionULID: sessULID,
		ContentType: "image/png",
		Size:        123,
		Scope:       ScopeConversation,
		ScopeID:     "conv-1",
		CreatedBy:   "user:x",
		CreatedAt:   now,
	})
	if err != nil {
		t.Fatal(err)
	}
	if want := filesPrefix + fileULID; s.FileURI().String() != want {
		t.Fatalf("fileURI = %q want %q", s.FileURI(), want)
	}
	if want := transPrefix + sessULID; s.TransferURI() != want {
		t.Fatalf("transferURI = %q want %q", s.TransferURI(), want)
	}
	if !strings.HasPrefix(s.TransferURI(), "ac://transfers/") {
		t.Fatalf("transferURI scheme: %q", s.TransferURI())
	}
	if s.ID() != sessULID {
		t.Fatalf("ID = %q want %q", s.ID(), sessULID)
	}
	if s.Status() != StatusOpen || !s.IsOpen() {
		t.Fatalf("status = %q want open", s.Status())
	}
	if s.Direction() != DirectionUpload {
		t.Fatalf("direction = %q", s.Direction())
	}
	if !s.ExpiresAt().Equal(now.Add(DefaultTransferTTL)) {
		t.Fatalf("expiresAt = %v want %v", s.ExpiresAt(), now.Add(DefaultTransferTTL))
	}
	if s.ContentType() != "image/png" || s.Size() != 123 || s.Scope() != ScopeConversation || s.ScopeID() != "conv-1" {
		t.Fatalf("metadata not carried: %+v", s)
	}
}

func TestNewUploadSession_CustomTTLAndInvalidInputs(t *testing.T) {
	fileULID, sessULID := mkSessIDs(t)
	now := time.Now().UTC()
	s, err := NewUploadSession(NewUploadInput{FileULID: fileULID, SessionULID: sessULID, CreatedBy: "u", CreatedAt: now, TTL: 5 * time.Minute})
	if err != nil {
		t.Fatal(err)
	}
	if !s.ExpiresAt().Equal(now.Add(5 * time.Minute)) {
		t.Fatalf("custom TTL ignored: %v", s.ExpiresAt())
	}
	// Bad file ULID.
	if _, err := NewUploadSession(NewUploadInput{FileULID: "not-a-ulid", SessionULID: sessULID, CreatedAt: now}); err != ErrBadULID {
		t.Fatalf("want ErrBadULID, got %v", err)
	}
	// Empty session ULID.
	if _, err := NewUploadSession(NewUploadInput{FileULID: fileULID, SessionULID: "", CreatedAt: now}); err != ErrBadULID {
		t.Fatalf("want ErrBadULID for empty session, got %v", err)
	}
	// Invalid scope.
	if _, err := NewUploadSession(NewUploadInput{FileULID: fileULID, SessionULID: sessULID, Scope: "bogus", CreatedAt: now}); err != ErrInvalidScope {
		t.Fatalf("want ErrInvalidScope, got %v", err)
	}
}

func TestNewDownloadSession(t *testing.T) {
	fileULID, sessULID := mkSessIDs(t)
	now := time.Now().UTC()
	uri, _ := NewFileURI(fileULID)
	s, err := NewDownloadSession(NewDownloadInput{FileURI: uri, SessionULID: sessULID, CreatedBy: "u", CreatedAt: now})
	if err != nil {
		t.Fatal(err)
	}
	if s.Direction() != DirectionDownload || s.Status() != StatusOpen {
		t.Fatalf("download session: %+v", s)
	}
	if s.FileURI() != uri {
		t.Fatalf("fileURI = %q want %q", s.FileURI(), uri)
	}
	// Bad URI.
	if _, err := NewDownloadSession(NewDownloadInput{FileURI: "nope", SessionULID: sessULID, CreatedAt: now}); err == nil {
		t.Fatal("want error for bad fileURI")
	}
	// Empty session.
	if _, err := NewDownloadSession(NewDownloadInput{FileURI: uri, SessionULID: "", CreatedAt: now}); err != ErrBadULID {
		t.Fatalf("want ErrBadULID, got %v", err)
	}
}

func newOpenUpload(t *testing.T) *FileTransferSession {
	t.Helper()
	f, s := mkSessIDs(t)
	sess, err := NewUploadSession(NewUploadInput{FileULID: f, SessionULID: s, CreatedBy: "u", CreatedAt: time.Now().UTC()})
	if err != nil {
		t.Fatal(err)
	}
	return sess
}

func TestComplete(t *testing.T) {
	s := newOpenUpload(t)
	at := time.Now().UTC()
	if err := s.Complete("abc123", 999, at); err != nil {
		t.Fatal(err)
	}
	if s.Status() != StatusCompleted || s.SHA256() != "abc123" || s.Size() != 999 {
		t.Fatalf("after complete: %+v", s)
	}
	// completed → complete again is illegal.
	if err := s.Complete("x", 1, at); err != ErrIllegalTransferState {
		t.Fatalf("want ErrIllegalTransferState, got %v", err)
	}
	// completed → cancel illegal.
	if err := s.Cancel(at); err != ErrIllegalTransferState {
		t.Fatalf("want ErrIllegalTransferState on cancel, got %v", err)
	}
}

func TestCancel(t *testing.T) {
	s := newOpenUpload(t)
	if err := s.Cancel(time.Now().UTC()); err != nil {
		t.Fatal(err)
	}
	if s.Status() != StatusCanceled {
		t.Fatalf("status = %q", s.Status())
	}
	// canceled → complete illegal.
	if err := s.Complete("x", 1, time.Now().UTC()); err != ErrIllegalTransferState {
		t.Fatalf("want ErrIllegalTransferState, got %v", err)
	}
}

func TestExpire(t *testing.T) {
	s := newOpenUpload(t)
	if err := s.Expire(time.Now().UTC()); err != nil {
		t.Fatal(err)
	}
	if s.Status() != StatusExpired {
		t.Fatalf("status = %q", s.Status())
	}
	// expired → expire again illegal.
	if err := s.Expire(time.Now().UTC()); err != ErrIllegalTransferState {
		t.Fatalf("want ErrIllegalTransferState, got %v", err)
	}
}

func TestIsExpired(t *testing.T) {
	now := time.Date(2026, 5, 29, 12, 0, 0, 0, time.UTC)
	f, sid := mkSessIDs(t)
	s, _ := NewUploadSession(NewUploadInput{FileULID: f, SessionULID: sid, CreatedAt: now, TTL: time.Hour})
	if s.IsExpired(now.Add(30 * time.Minute)) {
		t.Fatal("should not be expired before TTL")
	}
	if !s.IsExpired(now.Add(time.Hour)) {
		t.Fatal("should be expired at TTL boundary")
	}
	if !s.IsExpired(now.Add(2 * time.Hour)) {
		t.Fatal("should be expired after TTL")
	}
}

func TestTransferDirectionAndStatusValid(t *testing.T) {
	if !DirectionUpload.IsValid() || !DirectionDownload.IsValid() || TransferDirection("x").IsValid() {
		t.Fatal("direction IsValid")
	}
}
