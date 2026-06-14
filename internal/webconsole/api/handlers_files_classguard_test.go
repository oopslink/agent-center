package api

// v2.9.2 chat-attachments acceptance (Tester1 data/API lane) — inverse-mutation
// class-guards. Each test pins ONE defect *class* the attachments contract must
// never regress to, and is RED under the obvious mutation of the seam it guards:
//
//   CG1  write-once upload      — a 2nd PUT to a transfer whose bytes exist is a
//                                 409 at the HTTP contract (not a silent re-write).
//                                 Mutation: drop WriteBlob's Exists() check, or
//                                 mapFilesError's ErrBlobAlreadyExists→409 case.
//   CG2  cross-org download     — a blob whose only download-granting reference is
//        isolation (org red-line) a conversation in a FOREIGN org is NOT downloadable
//                                 even by a caller who is a participant of that conv,
//                                 when they call via their own org. Mutation: drop
//                                 the `conv.OrganizationID() != orgID` guard in
//                                 refReachableForHuman (would leak via participant
//                                 match). Carries a same-org positive control so the
//                                 403 is provably the ORG dimension, not participation.
//   CG3  multi-attachment       — N attachments round-trip with per-element
//        round-trip + per-ref    URI/filename/mime/size fidelity, persist a message
//        fidelity                with all N in order, mint exactly one conversation
//                                 reference per attachment carrying matching metadata,
//                                 and re-expose all N (in order) on the read-path DTO.
//                                 Mutation: drop/dedupe an attachment, mis-index the
//                                 req.Attachments[i] AddReference loop, or lose a
//                                 metadata field on any hop.
//
// Empty-attachments-default-[] and attach-only-uploader/reachable are already
// guarded (TestV27A0_EmptyContextRefsAndAttachmentsDefault, TestMsgPublicMap_
// Attachments, TestAPI_SendMessage_Attachment{OwnUpload,Unreachable...}); not
// duplicated here.

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"testing"

	"github.com/oopslink/agent-center/internal/conversation"
	convservice "github.com/oopslink/agent-center/internal/conversation/service"
	"github.com/oopslink/agent-center/internal/files"
	filesservice "github.com/oopslink/agent-center/internal/files/service"
	"github.com/oopslink/agent-center/internal/identity"
	"github.com/oopslink/agent-center/internal/observability"
)

// CG1 — write-once at the HTTP transport contract.
func TestCG_Files_WriteOnce_SecondPutIs409(t *testing.T) {
	deps, db := setupAPIWithAuth(t)
	sess := setupTestSession(t, db, deps)
	attachFilesSvc(t, &deps, db)
	s := newTestServer(t, deps)
	defer s.Close()

	resp := orgScopedPost(t, s.URL+"/api/files", `{"content_type":"text/plain","size":5}`, sess)
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("create upload: status=%d body=%s", resp.StatusCode, responseBytes(t, resp))
	}
	var created map[string]any
	if err := json.NewDecoder(resp.Body).Decode(&created); err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	transferID, _ := created["transfer_id"].(string)
	if transferID == "" {
		t.Fatalf("missing transfer_id: %v", created)
	}

	// First PUT streams the bytes → 200.
	first := orgScopedPut(t, s.URL+"/api/files/transfer/"+transferID, []byte("hello"), sess)
	if first.StatusCode != http.StatusOK {
		t.Fatalf("first put: status=%d body=%s", first.StatusCode, responseBytes(t, first))
	}
	first.Body.Close()

	// Second PUT to the SAME transfer (bytes already exist) must be rejected
	// write-once: 409 with the blob_exists code — never a silent overwrite.
	second := orgScopedPut(t, s.URL+"/api/files/transfer/"+transferID, []byte("WORLD"), sess)
	body := responseBytes(t, second)
	if second.StatusCode != http.StatusConflict {
		t.Fatalf("write-once: second PUT must be 409, got %d body=%s", second.StatusCode, body)
	}
	if !strings.Contains(string(body), "blob_exists") {
		t.Fatalf("write-once: expected blob_exists error code, got %s", body)
	}
}

// CG2 — cross-org download isolation (org red-line). Participation in a foreign
// org's conversation must NOT cross the org boundary.
func TestCG_Files_Download_CrossOrgConversationRef_403(t *testing.T) {
	deps, db := setupAPIWithAuth(t)
	sess := setupTestSession(t, db, deps) // member of "testorg"
	svc := attachFilesSvc(t, &deps, db)
	s := newTestServer(t, deps)
	defer s.Close()
	ctx := context.Background()

	caller := conversation.IdentityRef("user:" + sess.IdentityID)

	// A FOREIGN org the caller is not a member of.
	foreignOrg, err := identity.OrganizationFactory{}.New("foreignorg", "Foreign Org", "user:owner-foreign")
	if err != nil {
		t.Fatal(err)
	}
	if err := identity.NewSQLiteOrganizationRepo(db).Save(ctx, foreignOrg); err != nil {
		t.Fatal(err)
	}

	// A conversation in the FOREIGN org that nonetheless lists the caller as a
	// live participant — so the ONLY thing standing between caller and the blob
	// is the org boundary, not participation.
	foreignConv, err := deps.MessageWriter.OpenConversation(ctx, convservice.OpenCommand{
		Kind:           conversation.ConversationKindDM,
		OrganizationID: foreignOrg.ID(),
		Participants:   []conversation.ParticipantElement{{IdentityID: caller, Role: "owner", JoinedAt: "t", JoinedBy: caller}},
		CreatedBy:      caller,
		Actor:          observability.Actor(caller),
	})
	if err != nil {
		t.Fatal(err)
	}

	// Caller uploads a blob (via their own org) and the blob is referenced ONLY
	// from the foreign-org conversation (plus the server-derived uploader ref,
	// which by contract grants NO download).
	ulid := uploadBlob(t, s.URL, sess, []byte("secret"))
	if _, err := svc.AddReference(ctx, filesservice.AddReferenceCmd{
		FileURI: mustURI(t, ulid), Scope: files.ScopeConversation, ScopeID: string(foreignConv.ConversationID),
		Filename: "x.txt", MimeType: "text/plain", CreatedBy: string(caller),
	}); err != nil {
		t.Fatal(err)
	}

	// Download via the caller's OWN org → fail-closed 403 (org mismatch).
	resp := orgScopedGet(t, s.URL+"/api/files/"+ulid, sess)
	if resp.StatusCode != http.StatusForbidden {
		t.Fatalf("cross-org download must be 403, got %d body=%s", resp.StatusCode, responseBytes(t, resp))
	}

	// Positive control: SAME caller + SAME blob, but a conversation in the
	// caller's OWN org → 200. Proves the 403 above is the ORG dimension, not a
	// participation failure (a mutation dropping the org check flips this test's
	// first assertion to 200, while this control stays 200 → RED is unambiguous).
	sameOrgConv, err := deps.MessageWriter.OpenConversation(ctx, convservice.OpenCommand{
		Kind:           conversation.ConversationKindDM,
		OrganizationID: sess.OrgID,
		Participants:   []conversation.ParticipantElement{{IdentityID: caller, Role: "owner", JoinedAt: "t", JoinedBy: caller}},
		CreatedBy:      caller,
		Actor:          observability.Actor(caller),
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := svc.AddReference(ctx, filesservice.AddReferenceCmd{
		FileURI: mustURI(t, ulid), Scope: files.ScopeConversation, ScopeID: string(sameOrgConv.ConversationID),
		Filename: "x.txt", MimeType: "text/plain", CreatedBy: string(caller),
	}); err != nil {
		t.Fatal(err)
	}
	ok := orgScopedGet(t, s.URL+"/api/files/"+ulid, sess)
	if ok.StatusCode != http.StatusOK {
		t.Fatalf("same-org participant download must be 200 (positive control), got %d body=%s", ok.StatusCode, responseBytes(t, ok))
	}
}

// CG3 — multi-attachment round-trip + per-reference metadata fidelity.
func TestCG_SendMessage_MultiAttachment_RoundTripAndPerRef(t *testing.T) {
	deps, db := setupAPIWithAuth(t)
	sess := setupTestSession(t, db, deps)
	svc := attachFilesSvc(t, &deps, db)
	s := newTestServer(t, deps)
	defer s.Close()
	ctx := context.Background()

	convID := seedParticipantDM(t, deps, sess)

	type att struct {
		uri, filename, mime string
		size                int64
	}
	specs := []struct{ content, filename, mime string }{
		{"alpha-bytes", "alpha.txt", "text/plain"},
		{"PNGDATA", "diagram.png", "image/png"},
		{"%PDF-xx", "report.pdf", "application/pdf"},
	}
	var want []att
	var jsonAtts []string
	for _, sp := range specs {
		ulid := uploadBlob(t, s.URL, sess, []byte(sp.content))
		uri := "ac://files/" + ulid
		want = append(want, att{uri, sp.filename, sp.mime, int64(len(sp.content))})
		jsonAtts = append(jsonAtts, fmt.Sprintf(`{"uri":%q,"filename":%q,"mime_type":%q,"size":%d}`,
			uri, sp.filename, sp.mime, len(sp.content)))
	}
	body := fmt.Sprintf(`{"content":"multi","attachments":[%s]}`, strings.Join(jsonAtts, ","))
	resp := orgScopedPost(t, s.URL+"/api/conversations/"+convID+"/messages", body, sess)
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("multi-attach send: status=%d body=%s", resp.StatusCode, responseBytes(t, resp))
	}
	resp.Body.Close()

	// (a) persistence: one message carrying all N attachments, in order, with
	// every metadata field round-tripped exactly.
	msgs, err := deps.MsgRepo.FindByConversationID(ctx, conversation.ConversationID(convID), conversation.MessageFilter{Limit: 10})
	if err != nil {
		t.Fatal(err)
	}
	if len(msgs) != 1 {
		t.Fatalf("want exactly 1 message, got %d", len(msgs))
	}
	got := msgs[0].Attachments()
	if len(got) != len(want) {
		t.Fatalf("want %d attachments, got %d: %+v", len(want), len(got), got)
	}
	for i, w := range want {
		g := got[i]
		if g.URI != w.uri || g.Filename != w.filename || g.MimeType != w.mime || g.Size != w.size {
			t.Fatalf("attachment[%d] roundtrip mismatch: got %+v want %+v", i, g, w)
		}
	}

	// (b) exactly one conversation reference per attachment, each with matching
	// filename/mime/size (mis-indexed AddReference loop would cross these).
	for _, w := range want {
		refs, err := svc.ListReferences(ctx, mustURI(t, files.FileURI(w.uri).ULID()))
		if err != nil {
			t.Fatal(err)
		}
		var convRefs int
		for _, ref := range refs {
			if ref.Scope == files.ScopeConversation && ref.ScopeID == convID {
				convRefs++
				if ref.Filename != w.filename || ref.MimeType != w.mime || ref.SizeBytes != w.size {
					t.Fatalf("conv ref metadata mismatch for %s: got %+v want file=%s mime=%s size=%d",
						w.uri, ref, w.filename, w.mime, w.size)
				}
			}
		}
		if convRefs != 1 {
			t.Fatalf("want exactly 1 conversation ref for %s, got %d", w.uri, convRefs)
		}
	}

	// (c) read-path DTO re-exposes all N attachments, in order, round-tripped.
	getResp := orgScopedGet(t, s.URL+"/api/conversations/"+convID+"/messages", sess)
	dtoBody := responseBytes(t, getResp)
	if getResp.StatusCode != http.StatusOK {
		t.Fatalf("list messages: status=%d body=%s", getResp.StatusCode, dtoBody)
	}
	var arr []map[string]any
	if err := json.Unmarshal(dtoBody, &arr); err != nil {
		t.Fatalf("decode list messages: %v body=%s", err, dtoBody)
	}
	if len(arr) != 1 {
		t.Fatalf("want 1 message in DTO, got %d: %s", len(arr), dtoBody)
	}
	raw, ok := arr[0]["attachments"].([]any)
	if !ok || len(raw) != len(want) {
		t.Fatalf("DTO attachments shape wrong (want %d): %v", len(want), arr[0]["attachments"])
	}
	for i, w := range want {
		a, ok := raw[i].(map[string]any)
		if !ok {
			t.Fatalf("DTO attachment[%d] not an object: %v", i, raw[i])
		}
		size, _ := a["size"].(float64)
		if a["uri"] != w.uri || a["filename"] != w.filename || a["mime_type"] != w.mime || int64(size) != w.size {
			t.Fatalf("DTO attachment[%d] mismatch: got %v want %+v", i, a, w)
		}
	}
}
