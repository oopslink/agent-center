package api

import (
	"net/http"
	"testing"
)

// T460 ③: post_message returns a structured mentions report when an intended
// @mention resolves to nobody — resolved vs unresolved, each unresolved token
// carrying a "did you mean" suggestion — while STILL sending the message. A
// fully-resolved post carries no mentions block (no noise on the happy path).

// startAgentDM opens a DM between AG1 and AG2 and returns its conversation id, so
// the post_message participant gate passes for AG1.
func startAgentDM(t *testing.T, f *writeToolsFixture, srvURL string) string {
	t.Helper()
	status, body := postBearer(t, srvURL, "/admin/agent-tools/start_dm", "acat_w1",
		map[string]any{"agent_id": atAgent1, "target_agent": atAgent2, "content": "hi"})
	if status != http.StatusOK {
		t.Fatalf("start_dm status=%d body=%v", status, body)
	}
	convID, _ := body["conversation_id"].(string)
	if convID == "" {
		t.Fatalf("start_dm returned no conversation_id: %v", body)
	}
	return convID
}

func TestPostMessage_UnresolvedMention_ReportsDidYouMean(t *testing.T) {
	f := newWriteToolsFixture(t)
	f.addWorkerToken(t, "acat_w1", atWorker1)
	srv := f.server(t)
	convID := startAgentDM(t, f, srv.URL)

	// @AG1 is a real org agent (resolves); @AG3 matches nobody (a near-miss of AG1/AG2).
	status, body := postBearer(t, srv.URL, "/admin/agent-tools/post_message", "acat_w1",
		map[string]any{
			"agent_id": atAgent1,
			"target":   map[string]any{"type": "conversation", "id": convID},
			"content":  "@AG1 hi and @AG3 please look",
		})
	if status != http.StatusOK {
		t.Fatalf("post status=%d body=%v", status, body)
	}
	if body["message_id"] == nil || body["message_id"] == "" {
		t.Fatalf("message must still be sent (message_id present), got %v", body)
	}
	rep, ok := body["mentions"].(map[string]any)
	if !ok {
		t.Fatalf("expected a mentions report, got %v", body["mentions"])
	}
	resolved, _ := rep["resolved"].([]any)
	if !containsStr(resolved, "@AG1") {
		t.Fatalf("resolved should contain @AG1, got %v", resolved)
	}
	unresolved, _ := rep["unresolved"].([]any)
	if len(unresolved) != 1 {
		t.Fatalf("expected exactly one unresolved token, got %v", unresolved)
	}
	u0, _ := unresolved[0].(map[string]any)
	if u0["token"] != "@ag3" {
		t.Fatalf("unresolved token should be @ag3, got %v", u0["token"])
	}
	if dym, _ := u0["did_you_mean"].(string); dym == "" {
		t.Fatalf("unresolved @ag3 should carry a did_you_mean suggestion, got %v", u0)
	}
}

func TestPostMessage_AllResolved_NoMentionsBlock(t *testing.T) {
	f := newWriteToolsFixture(t)
	f.addWorkerToken(t, "acat_w1", atWorker1)
	srv := f.server(t)
	convID := startAgentDM(t, f, srv.URL)

	status, body := postBearer(t, srv.URL, "/admin/agent-tools/post_message", "acat_w1",
		map[string]any{
			"agent_id": atAgent1,
			"target":   map[string]any{"type": "conversation", "id": convID},
			"content":  "@AG2 looks good",
		})
	if status != http.StatusOK {
		t.Fatalf("post status=%d body=%v", status, body)
	}
	if _, present := body["mentions"]; present {
		t.Fatalf("a fully-resolved post must carry no mentions block, got %v", body["mentions"])
	}
}

func TestPostMessage_MentionRefs_UnknownRefReported(t *testing.T) {
	f := newWriteToolsFixture(t)
	f.addWorkerToken(t, "acat_w1", atWorker1)
	srv := f.server(t)
	convID := startAgentDM(t, f, srv.URL)

	// A known ref (agent:AG2) resolves; a bogus ref is reported unresolved. The
	// message still sends.
	status, body := postBearer(t, srv.URL, "/admin/agent-tools/post_message", "acat_w1",
		map[string]any{
			"agent_id":     atAgent1,
			"target":       map[string]any{"type": "conversation", "id": convID},
			"content":      "handing off",
			"mention_refs": []string{"agent:AG2", "agent:NOPE-404"},
		})
	if status != http.StatusOK {
		t.Fatalf("post status=%d body=%v", status, body)
	}
	if body["message_id"] == nil || body["message_id"] == "" {
		t.Fatalf("message must still be sent, got %v", body)
	}
	rep, ok := body["mentions"].(map[string]any)
	if !ok {
		t.Fatalf("expected a mentions report, got %v", body["mentions"])
	}
	resolved, _ := rep["resolved"].([]any)
	if !containsStr(resolved, "agent:AG2") {
		t.Fatalf("resolved should contain agent:AG2, got %v", resolved)
	}
	unresolved, _ := rep["unresolved"].([]any)
	if len(unresolved) != 1 {
		t.Fatalf("expected one unresolved ref, got %v", unresolved)
	}
	if u0, _ := unresolved[0].(map[string]any); u0["token"] != "agent:NOPE-404" {
		t.Fatalf("unresolved ref should be agent:NOPE-404, got %v", unresolved[0])
	}
}

func containsStr(xs []any, want string) bool {
	for _, x := range xs {
		if s, _ := x.(string); s == want {
			return true
		}
	}
	return false
}
