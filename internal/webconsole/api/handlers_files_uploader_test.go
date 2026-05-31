package api

import (
	"context"
	"testing"

	"github.com/oopslink/agent-center/internal/files"
)

// TestRefReachable_UploaderScope_NotDownloadable (#142): an uploader-scope
// reference must NOT grant a human DOWNLOAD — even to the uploader. Download authz
// is current-scope-membership, not "I once uploaded this" (else an uploader removed
// from a conversation would keep download = access-leak backdoor). Uploader
// reachability is ATTACH-ONLY (see callerUploaded).
func TestRefReachable_UploaderScope_NotDownloadable(t *testing.T) {
	s := &Server{}
	ref := files.FileReference{Scope: files.ScopeUploader, ScopeID: "user:alice"}
	// Even the uploader does not get download via the uploader ref.
	ok, err := s.refReachableForHuman(context.Background(), HandlerDeps{}, "user:alice", "org-1", ref)
	if err != nil || ok {
		t.Fatalf("uploader-scope must NOT grant download (attach-only): ok=%v err=%v", ok, err)
	}
}

// TestScopeUploader_ServerInternal (#142): uploader is a valid (persistable)
// reference scope but server-internal — a client must NOT be able to set it on an
// upload (IsClientSettable false), so uploader reachability is never a client claim.
func TestScopeUploader_ServerInternal(t *testing.T) {
	if !files.ScopeUploader.IsValid() {
		t.Fatal("ScopeUploader must be a valid (persistable) reference scope")
	}
	if files.ScopeUploader.IsClientSettable() {
		t.Fatal("ScopeUploader must NOT be client-settable (server-internal)")
	}
	if !files.ScopeConversation.IsClientSettable() {
		t.Fatal("conversation scope should be client-settable")
	}
}
