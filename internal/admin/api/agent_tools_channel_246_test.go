package api

import (
	"net/http"
	"strings"
	"testing"

	"github.com/oopslink/agent-center/internal/conversation"
)

// v2.7.1 #246: find_org_channel resolves a channel name → id (for post_message),
// and post_message gives precise channel errors. The β write-gate is unchanged.

func seedChannel246(t *testing.T, f *writeToolsFixture, id, name, orgID string) {
	t.Helper()
	c, err := conversation.NewConversation(conversation.NewConversationInput{
		ID: conversation.ConversationID(id), Kind: conversation.ConversationKindChannel,
		Name: name, OrganizationID: orgID, CreatedBy: "user:alice", OpenedAt: atNow,
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := f.convRepo.Save(t.Context(), c); err != nil {
		t.Fatal(err)
	}
}

func TestFindOrgChannel_246_ThreeStatesAndOrgScope(t *testing.T) {
	f := newWriteToolsFixture(t)
	f.addWorkerToken(t, "acat_w1", atWorker1)
	seedChannel246(t, f, "ch-1", "cha1", atTestOrg)
	seedChannel246(t, f, "ch-2", "cha2", atTestOrg)
	seedChannel246(t, f, "ch-other", "cha1", "other-org") // same name, DIFFERENT org
	srv := f.server(t)

	// match: name substring "cha1" → only the atTestOrg cha1 (not the other-org one).
	status, body := postBearer(t, srv.URL, "/admin/agent-tools/find_org_channel", "acat_w1",
		map[string]any{"agent_id": atAgent1, "name": "cha1"})
	if status != http.StatusOK {
		t.Fatalf("status=%d body=%v", status, body)
	}
	chs, _ := body["channels"].([]any)
	if len(chs) != 1 {
		t.Fatalf("want 1 channel cha1 in-org (cross-org excluded), got %v", body["channels"])
	}
	row := chs[0].(map[string]any)
	if row["id"] != "ch-1" || row["name"] != "cha1" {
		t.Fatalf("row=%v, want id ch-1 / name cha1 (channel id bare, no prefix)", row)
	}

	// empty name → all org channels (cha1 + cha2), not the other-org one.
	_, body = postBearer(t, srv.URL, "/admin/agent-tools/find_org_channel", "acat_w1",
		map[string]any{"agent_id": atAgent1, "name": ""})
	if chs, _ := body["channels"].([]any); len(chs) != 2 {
		t.Fatalf("empty name should list all 2 in-org channels, got %v", body["channels"])
	}

	// no match → empty list (NOT an error) — the agent reads this as "no such channel".
	status, body = postBearer(t, srv.URL, "/admin/agent-tools/find_org_channel", "acat_w1",
		map[string]any{"agent_id": atAgent1, "name": "nope"})
	if status != http.StatusOK {
		t.Fatalf("no-match must be 200 with empty list, got %d", status)
	}
	if chs, _ := body["channels"].([]any); len(chs) != 0 {
		t.Fatalf("no-match channels = %v, want []", body["channels"])
	}
}

// (a): post_message with a missing/typo'd conversation_id → clear 404
// conversation_not_found (not an opaque error), pointing at find_org_channel.
func TestPostMessage_246_ConversationNotFound(t *testing.T) {
	f := newWriteToolsFixture(t)
	f.addWorkerToken(t, "acat_w1", atWorker1)
	srv := f.server(t)

	status, body := postBearer(t, srv.URL, "/admin/agent-tools/post_message", "acat_w1",
		map[string]any{"agent_id": atAgent1, "target": map[string]any{"type": "conversation", "id": "ghost"}, "content": "hi"})
	if status != http.StatusNotFound {
		t.Fatalf("status=%d, want 404; body=%v", status, body)
	}
	if body["error"] != "conversation_not_found" {
		t.Fatalf("error=%v, want conversation_not_found", body["error"])
	}
	if msg, _ := body["message"].(string); !strings.Contains(msg, "find_org_channel") {
		t.Fatalf("message=%q, want it to point at find_org_channel", msg)
	}
}

// (3) + β: post_message to a channel the agent is NOT a member of → 403 with a
// precise "not a member of channel <name>" message. The write-gate is HELD —
// visibility (find_org_channel) does not grant write.
func TestPostMessage_246_NotChannelMember(t *testing.T) {
	f := newWriteToolsFixture(t)
	f.addWorkerToken(t, "acat_w1", atWorker1)
	seedChannel246(t, f, "ch-priv", "priv", atTestOrg) // AG1 is NOT a participant
	srv := f.server(t)

	status, body := postBearer(t, srv.URL, "/admin/agent-tools/post_message", "acat_w1",
		map[string]any{"agent_id": atAgent1, "target": map[string]any{"type": "conversation", "id": "ch-priv"}, "content": "hi"})
	if status != http.StatusForbidden {
		t.Fatalf("status=%d, want 403 (β write-gate HELD); body=%v", status, body)
	}
	if body["error"] != "not_a_channel_member" {
		t.Fatalf("error=%v, want not_a_channel_member", body["error"])
	}
	if msg, _ := body["message"].(string); !strings.Contains(msg, "priv") || !strings.Contains(strings.ToLower(msg), "not a member") {
		t.Fatalf("message=%q, want 'not a member of channel priv'", msg)
	}
}
