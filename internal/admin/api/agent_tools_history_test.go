package api

import (
	"context"
	"net/http"
	"testing"

	"github.com/oopslink/agent-center/internal/conversation"
	convservice "github.com/oopslink/agent-center/internal/conversation/service"
	"github.com/oopslink/agent-center/internal/observability"
)

// list_messages lets an agent BROWSE a conversation's chat history (the read
// complement to get_my_unread's inbox). Gate == post_message's participant gate.

// seedChannelWithMember creates a channel with the given agent ref as an active
// participant, so the agent passes the list_messages read gate.
func seedChannelWithMember(t *testing.T, f *writeToolsFixture, id, name, orgID, agentRef string) {
	t.Helper()
	c, err := conversation.NewConversation(conversation.NewConversationInput{
		ID: conversation.ConversationID(id), Kind: conversation.ConversationKindChannel,
		Name: name, OrganizationID: orgID, CreatedBy: "user:alice", OpenedAt: atNow,
	})
	if err != nil {
		t.Fatal(err)
	}
	c.SetParticipants([]conversation.ParticipantElement{
		{IdentityID: "user:alice", Role: "owner", JoinedAt: "t"},
		{IdentityID: conversation.IdentityRef(agentRef), Role: "member", JoinedAt: "t"},
	}, atNow)
	if err := f.convRepo.Save(t.Context(), c); err != nil {
		t.Fatal(err)
	}
}

// appendMsg writes one message into a conversation via the real MessageWriter and
// returns its id. The FakeClock does not advance, so ordering is decided by the
// monotonic ULID id tiebreaker — which is exactly the (posted_at, id) keyset the
// repo pages on.
func appendMsg(t *testing.T, f *writeToolsFixture, convID, sender, content string) string {
	t.Helper()
	res, err := f.deps.MessageWriter.AddMessage(context.Background(), convservice.AddMessageCommand{
		ConversationID:   conversation.ConversationID(convID),
		SenderIdentityID: conversation.IdentityRef(sender),
		ContentKind:      conversation.MessageContentText,
		Direction:        conversation.DirectionInbound,
		Content:          content,
		Actor:            observability.Actor(sender),
	})
	if err != nil {
		t.Fatal(err)
	}
	return string(res.MessageID)
}

// A participant agent reads a channel's full history — including messages it was
// never @mentioned in (the gap get_my_unread could not fill).
func TestListMessages_ParticipantReadsHistory(t *testing.T) {
	f := newWriteToolsFixture(t)
	f.addWorkerToken(t, "acat_w1", atWorker1)
	seedChannelWithMember(t, f, "ch-hist", "general", atTestOrg, "agent:"+atAgent1)
	appendMsg(t, f, "ch-hist", "user:alice", "first")
	appendMsg(t, f, "ch-hist", "user:bob", "second (no mention)")
	appendMsg(t, f, "ch-hist", "user:alice", "third")
	srv := f.server(t)

	status, body := postBearer(t, srv.URL, "/admin/agent-tools/list_messages", "acat_w1",
		map[string]any{"agent_id": atAgent1, "conversation_id": "ch-hist"})
	if status != http.StatusOK {
		t.Fatalf("status=%d body=%v", status, body)
	}
	if body["conversation_kind"] != "channel" || body["conversation_name"] != "general" {
		t.Fatalf("conversation meta=%v", body)
	}
	msgs, _ := body["messages"].([]any)
	if len(msgs) != 3 {
		t.Fatalf("want 3 history messages, got %d (%v)", len(msgs), body["messages"])
	}
	// chronological oldest→newest
	want := []string{"first", "second (no mention)", "third"}
	for i, w := range want {
		m := msgs[i].(map[string]any)
		if m["content"] != w {
			t.Fatalf("messages[%d].content=%v want %q", i, m["content"], w)
		}
	}
	if body["has_more"] != false {
		t.Fatalf("has_more=%v want false (all fit)", body["has_more"])
	}
}

// limit + before_message_id keyset paging walks older history without dup/skip.
func TestListMessages_Pagination(t *testing.T) {
	f := newWriteToolsFixture(t)
	f.addWorkerToken(t, "acat_w1", atWorker1)
	seedChannelWithMember(t, f, "ch-pg", "paged", atTestOrg, "agent:"+atAgent1)
	var ids []string
	for _, c := range []string{"m1", "m2", "m3", "m4", "m5"} {
		ids = append(ids, appendMsg(t, f, "ch-pg", "user:alice", c))
	}
	srv := f.server(t)

	// page 1: newest 2 → m4, m5 (oldest→newest), has_more=true
	status, body := postBearer(t, srv.URL, "/admin/agent-tools/list_messages", "acat_w1",
		map[string]any{"agent_id": atAgent1, "conversation_id": "ch-pg", "limit": 2})
	if status != http.StatusOK {
		t.Fatalf("status=%d body=%v", status, body)
	}
	msgs, _ := body["messages"].([]any)
	if len(msgs) != 2 || msgs[0].(map[string]any)["content"] != "m4" || msgs[1].(map[string]any)["content"] != "m5" {
		t.Fatalf("page1 = %v, want [m4 m5]", body["messages"])
	}
	if body["has_more"] != true {
		t.Fatalf("page1 has_more=%v want true", body["has_more"])
	}
	cursor, _ := body["next_before_message_id"].(string)
	if cursor == "" {
		t.Fatalf("page1 missing next_before_message_id")
	}

	// page 2: older than m4 → m2, m3
	_, body = postBearer(t, srv.URL, "/admin/agent-tools/list_messages", "acat_w1",
		map[string]any{"agent_id": atAgent1, "conversation_id": "ch-pg", "limit": 2, "before_message_id": cursor})
	msgs, _ = body["messages"].([]any)
	if len(msgs) != 2 || msgs[0].(map[string]any)["content"] != "m2" || msgs[1].(map[string]any)["content"] != "m3" {
		t.Fatalf("page2 = %v, want [m2 m3]", body["messages"])
	}
	if body["has_more"] != true {
		t.Fatalf("page2 has_more=%v want true (m1 remains)", body["has_more"])
	}

	// page 3: older than m2 → m1, has_more=false
	cursor, _ = body["next_before_message_id"].(string)
	_, body = postBearer(t, srv.URL, "/admin/agent-tools/list_messages", "acat_w1",
		map[string]any{"agent_id": atAgent1, "conversation_id": "ch-pg", "limit": 2, "before_message_id": cursor})
	msgs, _ = body["messages"].([]any)
	if len(msgs) != 1 || msgs[0].(map[string]any)["content"] != "m1" {
		t.Fatalf("page3 = %v, want [m1]", body["messages"])
	}
	if body["has_more"] != false {
		t.Fatalf("page3 has_more=%v want false", body["has_more"])
	}
	_ = ids
}

// A non-member is denied with the actionable channel wording (read gate == write gate).
func TestListMessages_NotChannelMember(t *testing.T) {
	f := newWriteToolsFixture(t)
	f.addWorkerToken(t, "acat_w1", atWorker1)
	seedChannel246(t, f, "ch-closed", "closed", atTestOrg) // AG1 is NOT a participant
	appendMsg(t, f, "ch-closed", "user:alice", "secret")
	srv := f.server(t)

	status, body := postBearer(t, srv.URL, "/admin/agent-tools/list_messages", "acat_w1",
		map[string]any{"agent_id": atAgent1, "conversation_id": "ch-closed"})
	if status != http.StatusForbidden {
		t.Fatalf("status=%d want 403; body=%v", status, body)
	}
	if body["error"] != "not_a_channel_member" {
		t.Fatalf("error=%v want not_a_channel_member", body["error"])
	}
}

// A missing conversation_id → 400; a typo'd id → 404 pointing at find_org_channel.
func TestListMessages_BadConversation(t *testing.T) {
	f := newWriteToolsFixture(t)
	f.addWorkerToken(t, "acat_w1", atWorker1)
	srv := f.server(t)

	status, body := postBearer(t, srv.URL, "/admin/agent-tools/list_messages", "acat_w1",
		map[string]any{"agent_id": atAgent1})
	if status != http.StatusBadRequest || body["error"] != "missing_conversation_id" {
		t.Fatalf("missing id: status=%d error=%v", status, body["error"])
	}

	status, body = postBearer(t, srv.URL, "/admin/agent-tools/list_messages", "acat_w1",
		map[string]any{"agent_id": atAgent1, "conversation_id": "ghost"})
	if status != http.StatusNotFound || body["error"] != "conversation_not_found" {
		t.Fatalf("ghost id: status=%d error=%v", status, body["error"])
	}
}
