package api

import (
	"encoding/json"
	"fmt"
	"testing"
	"time"

	"github.com/oopslink/agent-center/internal/conversation"
	convsqlite "github.com/oopslink/agent-center/internal/conversation/sqlite"
)

// TestAPI_ListMessages_BeyondWindow_IncludesLatest is the T189 regression: a
// conversation with MORE than the 200-message window must still return the LATEST
// top-level messages (incl. the user's own just-sent one) — the old handler used
// Limit (oldest 200), so once a conversation passed 200 top-level messages the
// list froze on the oldest window and new messages never appeared on reload.
func TestAPI_ListMessages_BeyondWindow_IncludesLatest(t *testing.T) {
	deps, db := setupAPIWithAuth(t)
	sess := setupTestSession(t, db, deps)
	cid := seedOrgChannel(t, deps, sess.OrgID, "alpha")
	s := newTestServer(t, deps)
	defer s.Close()

	// Seed 250 top-level messages directly (fast path; distinct increasing
	// posted_at so ordering is unambiguous).
	msgRepo := convsqlite.NewMessageRepo(db)
	now := time.Now().UTC()
	const total = 250
	for i := 0; i < total; i++ {
		m, err := conversation.NewMessage(conversation.NewMessageInput{
			ID:               conversation.MessageID(fmt.Sprintf("m-%04d", i)),
			ConversationID:   conversation.ConversationID(cid),
			SenderIdentityID: "user:h",
			ContentKind:      conversation.MessageContentText,
			Content:          fmt.Sprintf("msg-%04d", i),
			Direction:        conversation.DirectionInbound,
			PostedAt:         now.Add(time.Duration(i) * time.Millisecond),
		})
		if err != nil {
			t.Fatal(err)
		}
		if err := msgRepo.Append(t.Context(), m); err != nil {
			t.Fatal(err)
		}
	}

	resp := orgScopedGet(t, s.URL+"/api/conversations/"+cid+"/messages", sess)
	var msgs []map[string]any
	if err := json.NewDecoder(resp.Body).Decode(&msgs); err != nil {
		t.Fatal(err)
	}
	if len(msgs) != 200 {
		t.Fatalf("expected the newest 200, got %d", len(msgs))
	}
	ids := map[string]bool{}
	for _, m := range msgs {
		ids[m["id"].(string)] = true
	}
	// The newest message MUST be present (the actual bug: it was absent).
	if !ids["m-0249"] {
		t.Fatalf("newest message m-0249 missing from the list (T189 regression)")
	}
	// Oldest-beyond-window dropped; the window is the newest 200 (m-0050..m-0249).
	if ids["m-0049"] {
		t.Fatalf("m-0049 should be outside the newest-200 window")
	}
	// Returned in chronological (oldest→newest) order.
	if msgs[0]["id"].(string) != "m-0050" || msgs[len(msgs)-1]["id"].(string) != "m-0249" {
		t.Fatalf("expected ASC window m-0050..m-0249, got %s..%s",
			msgs[0]["id"], msgs[len(msgs)-1]["id"])
	}
}
