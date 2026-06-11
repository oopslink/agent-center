package api

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/oopslink/agent-center/internal/conversation"
	convservice "github.com/oopslink/agent-center/internal/conversation/service"
)

// =============================================================================
// POST /api/conversations — SPA F2 unified create endpoint
// =============================================================================

func TestAPI_CreateConversation_Channel_Happy(t *testing.T) {
	deps, _ := setupAPI(t)
	s := newTestServer(t, deps)
	defer s.Close()

	resp, err := http.Post(s.URL+"/api/orgs/_/conversations",
		"application/json",
		strings.NewReader(`{"kind":"channel","name":"alpha","description":"plan room"}`))
	if err != nil {
		t.Fatal(err)
	}
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("got %d", resp.StatusCode)
	}
	var body map[string]any
	_ = json.NewDecoder(resp.Body).Decode(&body)
	if body["conversation_id"] == "" {
		t.Fatalf("missing conversation_id; got %v", body)
	}
	if body["kind"] != "channel" {
		t.Fatalf("bad kind: %v", body)
	}
	// Verify persisted: list and look for it.
	conv, ferr := deps.ConvRepo.FindByName(context.Background(), "alpha")
	if ferr != nil || conv == nil {
		t.Fatalf("channel not persisted: %v", ferr)
	}
	if conv.Kind() != conversation.ConversationKindChannel {
		t.Fatalf("kind mismatch: %s", conv.Kind())
	}
	parts := conv.Participants()
	if len(parts) != 1 || parts[0].Role != "owner" {
		t.Fatalf("expected single owner participant; got %+v", parts)
	}
}

func TestAPI_CreateConversation_Channel_NameRequired(t *testing.T) {
	deps, _ := setupAPI(t)
	s := newTestServer(t, deps)
	defer s.Close()
	resp, _ := http.Post(s.URL+"/api/orgs/_/conversations",
		"application/json",
		strings.NewReader(`{"kind":"channel"}`))
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("got %d", resp.StatusCode)
	}
	var body map[string]any
	_ = json.NewDecoder(resp.Body).Decode(&body)
	if body["error"] != "invalid_input" {
		t.Fatalf("err shape: %v", body)
	}
}

func TestAPI_CreateConversation_Channel_DuplicateName(t *testing.T) {
	deps, _ := setupAPI(t)
	_, _ = deps.ChannelMgmtSvc.CreateChannel(context.Background(), convservice.CreateChannelCommand{
		Name: "dup", CreatedBy: "user:hayang", Actor: "user:hayang",
	})
	s := newTestServer(t, deps)
	defer s.Close()
	resp, _ := http.Post(s.URL+"/api/orgs/_/conversations",
		"application/json",
		strings.NewReader(`{"kind":"channel","name":"dup"}`))
	if resp.StatusCode != http.StatusConflict {
		t.Fatalf("got %d (want 409 from ErrConversationAlreadyExists)", resp.StatusCode)
	}
}

func TestAPI_CreateConversation_DM_Happy(t *testing.T) {
	deps, _ := setupAPI(t)
	s := newTestServer(t, deps)
	defer s.Close()
	resp, _ := http.Post(s.URL+"/api/orgs/_/conversations",
		"application/json",
		strings.NewReader(`{"kind":"dm","members":["agent:supervisor-1"]}`))
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("got %d", resp.StatusCode)
	}
	var body map[string]any
	_ = json.NewDecoder(resp.Body).Decode(&body)
	if body["kind"] != "dm" {
		t.Fatalf("bad kind: %v", body)
	}
	convID := body["conversation_id"].(string)
	conv, ferr := deps.ConvRepo.FindByID(context.Background(), conversation.ConversationID(convID))
	if ferr != nil {
		t.Fatal(ferr)
	}
	if conv.Kind() != conversation.ConversationKindDM {
		t.Fatalf("kind mismatch: %s", conv.Kind())
	}
	parts := conv.Participants()
	if len(parts) != 2 {
		t.Fatalf("expected caller + 1 peer; got %d (%+v)", len(parts), parts)
	}
	roles := map[string]int{}
	for _, p := range parts {
		roles[p.Role]++
	}
	if roles["owner"] != 1 || roles["member"] != 1 {
		t.Fatalf("expected 1 owner + 1 member; got %v", roles)
	}
}

func TestAPI_CreateConversation_DM_MembersRequired(t *testing.T) {
	deps, _ := setupAPI(t)
	s := newTestServer(t, deps)
	defer s.Close()
	resp, _ := http.Post(s.URL+"/api/orgs/_/conversations",
		"application/json",
		strings.NewReader(`{"kind":"dm"}`))
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("got %d", resp.StatusCode)
	}
}

func TestAPI_CreateConversation_DM_BadMemberIdentity(t *testing.T) {
	deps, _ := setupAPI(t)
	s := newTestServer(t, deps)
	defer s.Close()
	resp, _ := http.Post(s.URL+"/api/orgs/_/conversations",
		"application/json",
		strings.NewReader(`{"kind":"dm","members":["no-prefix-bareid"]}`))
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("got %d", resp.StatusCode)
	}
	var body map[string]any
	_ = json.NewDecoder(resp.Body).Decode(&body)
	if body["error"] != "invalid_input" {
		t.Fatalf("err shape: %v", body)
	}
}

// v2.7.1 #215: DM is strictly 1:1 — more than one member (e.g. caller + a peer, or
// two peers) is rejected (use a channel for group). Supersedes the old
// "dedup caller in members" behavior.
func TestAPI_CreateConversation_DM_StrictOneToOne(t *testing.T) {
	deps, _ := setupAPI(t)
	s := newTestServer(t, deps)
	defer s.Close()
	resp, _ := http.Post(s.URL+"/api/orgs/_/conversations",
		"application/json",
		strings.NewReader(`{"kind":"dm","members":["user:hayang","agent:s-1"]}`))
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("got %d (want 400 — DM is strictly 1:1)", resp.StatusCode)
	}
	var body map[string]any
	_ = json.NewDecoder(resp.Body).Decode(&body)
	if body["error"] != "invalid_input" {
		t.Fatalf("err shape: %v", body)
	}
}

func TestAPI_CreateConversation_BadKind(t *testing.T) {
	deps, _ := setupAPI(t)
	s := newTestServer(t, deps)
	defer s.Close()
	resp, _ := http.Post(s.URL+"/api/orgs/_/conversations",
		"application/json",
		strings.NewReader(`{"kind":"task","name":"x"}`))
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("got %d (task is not directly openable)", resp.StatusCode)
	}
}

func TestAPI_CreateConversation_BadJSON(t *testing.T) {
	deps, _ := setupAPI(t)
	s := newTestServer(t, deps)
	defer s.Close()
	resp, _ := http.Post(s.URL+"/api/orgs/_/conversations",
		"application/json", strings.NewReader(`{not json`))
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("got %d", resp.StatusCode)
	}
}

func TestAPI_CreateConversation_Channel_ChannelSvcNotWired(t *testing.T) {
	deps, _ := setupAPI(t)
	deps.ChannelMgmtSvc = nil
	s := newTestServer(t, deps)
	defer s.Close()
	resp, _ := http.Post(s.URL+"/api/orgs/_/conversations",
		"application/json",
		strings.NewReader(`{"kind":"channel","name":"x"}`))
	if resp.StatusCode != http.StatusNotImplemented {
		t.Fatalf("got %d", resp.StatusCode)
	}
}

func TestAPI_ListRefs_Happy(t *testing.T) {
	deps, db := setupAPIWithAuth(t)
	sess := setupTestSession(t, db, deps)
	ctx := context.Background()
	// Seed: source conv with one message, then materialise a carry-over
	// into a new child conv (both in the caller's org).
	src, _ := deps.ChannelMgmtSvc.CreateChannel(ctx, convservice.CreateChannelCommand{
		Name: "src-refs", OrganizationID: sess.OrgID, CreatedBy: "user:hayang", Actor: "user:hayang",
	})
	addMsg, _ := deps.MessageWriter.AddMessage(ctx, convservice.AddMessageCommand{
		ConversationID:   src.ConversationID,
		SenderIdentityID: "user:hayang",
		ContentKind:      conversation.MessageContentText,
		Content:          "snip",
		Direction:        conversation.DirectionInbound,
		Actor:            "user:hayang",
	})
	child, _ := conversation.NewConversation(conversation.NewConversationInput{
		ID: "CHILD-REFS", Kind: conversation.ConversationKindIssue,
		Name: "x", OrganizationID: sess.OrgID, CreatedBy: "user:hayang", OpenedAt: time.Now().UTC(),
	})
	_ = deps.ConvRepo.Save(ctx, child)
	_, _ = deps.CarryOverSvc.Materialise(ctx, convservice.MaterialiseCommand{
		ChildConversationID:  child.ID(),
		SourceConversationID: src.ConversationID,
		SourceMessageIDs:     []conversation.MessageID{addMsg.MessageID},
		CreatedBy:            "user:hayang",
		Actor:                "user:hayang",
	})

	s := newTestServer(t, deps)
	defer s.Close()
	resp := orgScopedGet(t, s.URL+"/api/conversations/CHILD-REFS/refs", sess)
	if resp.StatusCode != 200 {
		t.Fatalf("got %d", resp.StatusCode)
	}
	var arr []map[string]any
	_ = json.NewDecoder(resp.Body).Decode(&arr)
	if len(arr) != 1 {
		t.Fatalf("expected 1 ref; got %d", len(arr))
	}
	if arr[0]["source_message_id"] != string(addMsg.MessageID) {
		t.Fatalf("ref shape: %v", arr[0])
	}
}

func TestAPI_ListRefs_NotWired(t *testing.T) {
	deps, _ := setupAPI(t)
	deps.CarryOverSvc = nil
	s := newTestServer(t, deps)
	defer s.Close()
	resp, _ := http.Get(s.URL + "/api/orgs/_/conversations/whatever/refs")
	if resp.StatusCode != http.StatusNotImplemented {
		t.Fatalf("got %d", resp.StatusCode)
	}
}

func TestAPI_CreateConversation_DM_MessageWriterNotWired(t *testing.T) {
	deps, _ := setupAPI(t)
	deps.MessageWriter = nil
	s := newTestServer(t, deps)
	defer s.Close()
	resp, _ := http.Post(s.URL+"/api/orgs/_/conversations",
		"application/json",
		strings.NewReader(`{"kind":"dm","members":["agent:x"]}`))
	if resp.StatusCode != http.StatusNotImplemented {
		t.Fatalf("got %d", resp.StatusCode)
	}
}

// v2.7 #198: DELETE /api/conversations/{id} hard-deletes a DM (conversation row +
// messages); a channel returns 400 use_archive; a non-participant gets 403.
func TestDeleteConversation_DMHardDelete(t *testing.T) {
	deps, db := setupAPIWithAuth(t)
	sess := setupTestSession(t, db, deps)
	s := newTestServer(t, deps)
	defer s.Close()

	// Caller opens a DM → caller is the owner participant.
	resp := orgScopedPost(t, s.URL+"/api/conversations", `{"kind":"dm","members":["agent:s-1"]}`, sess)
	if resp.StatusCode != http.StatusCreated && resp.StatusCode != http.StatusOK {
		t.Fatalf("create dm: status %d", resp.StatusCode)
	}
	var created map[string]any
	_ = json.NewDecoder(resp.Body).Decode(&created)
	resp.Body.Close()
	convID, _ := created["conversation_id"].(string)
	if convID == "" {
		t.Fatalf("no conversation_id: %v", created)
	}
	// Post a message so we can assert messages are cascade-deleted.
	if _, err := deps.MessageWriter.AddMessage(context.Background(), convservice.AddMessageCommand{
		ConversationID:   conversation.ConversationID(convID),
		SenderIdentityID: conversation.IdentityRef("agent:s-1"),
		ContentKind:      conversation.MessageContentText,
		Direction:        conversation.DirectionInbound,
		Content:          "hi",
		Actor:            "agent:s-1",
	}); err != nil {
		t.Fatalf("seed message: %v", err)
	}

	del := orgScopedDelete(t, s.URL+"/api/conversations/"+convID, sess)
	if del.StatusCode != http.StatusOK {
		t.Fatalf("delete dm: status %d", del.StatusCode)
	}
	del.Body.Close()

	// Conversation gone.
	if _, err := deps.ConvRepo.FindByID(context.Background(), conversation.ConversationID(convID)); err == nil {
		t.Fatalf("conversation must be gone after delete")
	}
	// Messages gone.
	msgs, _ := deps.MsgRepo.FindByConversationID(context.Background(), conversation.ConversationID(convID), conversation.MessageFilter{})
	if len(msgs) != 0 {
		t.Fatalf("messages must be cascade-deleted, got %d", len(msgs))
	}
}

func TestDeleteConversation_ChannelRejected(t *testing.T) {
	deps, db := setupAPIWithAuth(t)
	sess := setupTestSession(t, db, deps)
	s := newTestServer(t, deps)
	defer s.Close()

	convID := seedOrgChannel(t, deps, sess.OrgID, "general")
	del := orgScopedDelete(t, s.URL+"/api/conversations/"+convID, sess)
	defer del.Body.Close()
	if del.StatusCode != http.StatusBadRequest {
		t.Fatalf("channel delete must be 400 use_archive, got %d", del.StatusCode)
	}
	var e map[string]any
	_ = json.NewDecoder(del.Body).Decode(&e)
	if e["error"] != "use_archive" {
		t.Fatalf("want error use_archive, got %v", e)
	}
}

// v2.7 #201 (Tester repro): POST /api/conversations kind=channel with an agent in
// members[] must add it as an active participant (it was dropped before, breaking
// channel @mention→agent). Mirrors the DM behavior.
func TestCreateChannel_SeedsAgentParticipant(t *testing.T) {
	deps, db := setupAPIWithAuth(t)
	sess := setupTestSession(t, db, deps)
	s := newTestServer(t, deps)
	defer s.Close()

	resp := orgScopedPost(t, s.URL+"/api/conversations",
		`{"kind":"channel","name":"x","members":["agent:agent-0ae4eb1b"]}`, sess)
	if resp.StatusCode != http.StatusCreated && resp.StatusCode != http.StatusOK {
		t.Fatalf("create channel: status %d", resp.StatusCode)
	}
	var created map[string]any
	_ = json.NewDecoder(resp.Body).Decode(&created)
	resp.Body.Close()
	convID, _ := created["conversation_id"].(string)
	if convID == "" {
		t.Fatalf("no conversation_id: %v", created)
	}
	c, err := deps.ConvRepo.FindByID(context.Background(), conversation.ConversationID(convID))
	if err != nil {
		t.Fatal(err)
	}
	if !c.HasActiveParticipant("agent:agent-0ae4eb1b") {
		t.Fatalf("channel must include the agent member as an active participant (#201); got %d participants", len(c.Participants()))
	}
}
