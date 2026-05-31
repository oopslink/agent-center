package api

import (
	"context"
	"testing"

	"github.com/oopslink/agent-center/internal/files"
)

// TestRefReachable_UploaderScope (#142): an uploader-scope reference grants
// reachability to EXACTLY the uploader identity (per-user), and to nobody else —
// not a different user (even same org), not an empty caller.
func TestRefReachable_UploaderScope(t *testing.T) {
	s := &Server{}
	ref := files.FileReference{Scope: files.ScopeUploader, ScopeID: "user:alice"}

	// The uploader reaches their own blob.
	ok, err := s.refReachableForHuman(context.Background(), HandlerDeps{}, "user:alice", "org-1", ref)
	if err != nil || !ok {
		t.Fatalf("uploader should reach own blob: ok=%v err=%v", ok, err)
	}
	// A different identity does NOT (per-user, not per-org).
	ok, _ = s.refReachableForHuman(context.Background(), HandlerDeps{}, "user:bob", "org-1", ref)
	if ok {
		t.Fatal("a non-uploader identity must NOT reach the blob (per-user)")
	}
	// Empty caller does not.
	ok, _ = s.refReachableForHuman(context.Background(), HandlerDeps{}, "", "org-1", ref)
	if ok {
		t.Fatal("empty caller must not reach an uploader-scope ref")
	}
}

// TestScopeUploader_NotClientValid (#142): uploader is server-internal — IsValid
// must reject it so a client can never set scope=uploader on an upload.
func TestScopeUploader_NotClientValid(t *testing.T) {
	if files.ScopeUploader.IsValid() {
		t.Fatal("ScopeUploader must NOT be client-valid (server-internal reachability scope)")
	}
}
