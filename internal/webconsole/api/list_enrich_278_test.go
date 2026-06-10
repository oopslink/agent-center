package api

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/oopslink/agent-center/internal/agent"
	agentsql "github.com/oopslink/agent-center/internal/agent/sqlite"
	"github.com/oopslink/agent-center/internal/conversation"
	convservice "github.com/oopslink/agent-center/internal/conversation/service"
	"github.com/oopslink/agent-center/internal/observability"
)

// t0Enrich is a fixed activity timestamp for the agents-list enrich test.
var t0Enrich = time.Date(2026, 6, 1, 9, 30, 0, 0, time.UTC)

// v2.8.1 #278: each conversations-list row carries created_at + participants
// {count, members} + recent_messages (<=3, newest-first, plain-text preview).
func TestConversationsList_Enrich_278(t *testing.T) {
	deps, db := setupAPIWithAuth(t)
	sess := setupTestSession(t, db, deps)
	s := newTestServer(t, deps)
	defer s.Close()
	ctx := context.Background()
	selfRef := conversation.IdentityRef("user:" + sess.IdentityID)
	selfActor := observability.Actor("user:" + sess.IdentityID)

	// Channel with the caller-owner + an agent member (2 active participants).
	res, err := deps.ChannelMgmtSvc.CreateChannel(ctx, convservice.CreateChannelCommand{
		Name: "general", OrganizationID: sess.OrgID,
		CreatedBy: selfRef,
		Actor:     selfActor,
		Members:   []conversation.IdentityRef{"agent:agent-bot"},
	})
	if err != nil {
		t.Fatal(err)
	}

	// Three messages oldest→newest; the last carries markdown + a fenced block.
	contents := []string{
		"first message",
		"second message",
		"third **bold** message\n```go\nfmt.Println(1)\n```\nafter code",
	}
	for _, c := range contents {
		if _, aerr := deps.MessageWriter.AddMessage(ctx, convservice.AddMessageCommand{
			ConversationID:   res.ConversationID,
			SenderIdentityID: selfRef,
			ContentKind:      conversation.MessageContentText,
			Content:          c,
			Direction:        conversation.DirectionInbound,
			Actor:            selfActor,
		}); aerr != nil {
			t.Fatal(aerr)
		}
	}

	// Empty channel → recent_messages [].
	if _, err := deps.ChannelMgmtSvc.CreateChannel(ctx, convservice.CreateChannelCommand{
		Name: "silent", OrganizationID: sess.OrgID,
		CreatedBy: selfRef,
		Actor:     selfActor,
	}); err != nil {
		t.Fatal(err)
	}

	resp := orgScopedGet(t, s.URL+"/api/conversations?kind=channel", sess)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("list got %d", resp.StatusCode)
	}
	var rows []map[string]any
	if err := json.NewDecoder(resp.Body).Decode(&rows); err != nil {
		t.Fatal(err)
	}

	var general, silent map[string]any
	for _, row := range rows {
		switch row["name"] {
		case "general":
			general = row
		case "silent":
			silent = row
		}
	}
	if general == nil || silent == nil {
		t.Fatalf("missing rows: %+v", rows)
	}

	// created_at present + RFC3339 (parseable).
	if ca, _ := general["created_at"].(string); ca == "" {
		t.Fatal("general missing created_at")
	}

	// participants{count, members}.
	parts, _ := general["participants"].(map[string]any)
	if parts == nil {
		t.Fatalf("general missing participants: %+v", general)
	}
	if cnt, _ := parts["count"].(float64); cnt != 2 {
		t.Fatalf("participants.count = %v, want 2", parts["count"])
	}
	members, _ := parts["members"].([]any)
	if len(members) != 2 {
		t.Fatalf("participants.members len = %d, want 2", len(members))
	}
	// kind label: agent member → "agent".
	var sawAgent bool
	for _, mm := range members {
		m := mm.(map[string]any)
		if m["identity_ref"] == "agent:agent-bot" {
			sawAgent = true
			if m["kind"] != "agent" {
				t.Errorf("agent member kind = %v, want agent", m["kind"])
			}
		}
		if m["display_name"] == "" || m["display_name"] == nil {
			t.Errorf("member missing display_name: %+v", m)
		}
	}
	if !sawAgent {
		t.Errorf("agent member not in participants: %+v", members)
	}

	// recent_messages: <=3, newest-first, plain-text preview (fence stripped).
	recent, _ := general["recent_messages"].([]any)
	if len(recent) != 3 {
		t.Fatalf("recent_messages len = %d, want 3", len(recent))
	}
	newest := recent[0].(map[string]any)
	preview, _ := newest["preview"].(string)
	if preview == "" {
		t.Fatal("newest preview empty")
	}
	if strings.Contains(preview, "```") || strings.Contains(preview, "fmt.Println") {
		t.Errorf("preview not plain-text (fence leaked): %q", preview)
	}
	if newest["posted_at"] == "" || newest["posted_at"] == nil {
		t.Error("recent message missing posted_at")
	}
	if newest["sender_display_name"] == "" || newest["sender_display_name"] == nil {
		t.Error("recent message missing sender_display_name")
	}
	// newest-first: first preview is the third (markdown) message.
	if !strings.Contains(preview, "third") {
		t.Errorf("recent_messages not newest-first; first preview = %q", preview)
	}

	// Empty channel → recent_messages is [] (present, non-null, empty).
	srecent, ok := silent["recent_messages"].([]any)
	if !ok {
		t.Fatalf("silent recent_messages not an array: %v", silent["recent_messages"])
	}
	if len(srecent) != 0 {
		t.Fatalf("silent recent_messages = %v, want []", srecent)
	}
}

// v2.8.1 #278 soft-ref orphan tolerance: a recent message from a sender whose
// member row does not resolve must NOT crash — it degrades to a friendly handle.
func TestConversationsList_Enrich_SoftRefSender_278(t *testing.T) {
	deps, db := setupAPIWithAuth(t)
	sess := setupTestSession(t, db, deps)
	s := newTestServer(t, deps)
	defer s.Close()
	ctx := context.Background()
	selfRef := conversation.IdentityRef("user:" + sess.IdentityID)
	selfActor := observability.Actor("user:" + sess.IdentityID)

	res, err := deps.ChannelMgmtSvc.CreateChannel(ctx, convservice.CreateChannelCommand{
		Name: "orphan", OrganizationID: sess.OrgID,
		CreatedBy: selfRef,
		Actor:     selfActor,
	})
	if err != nil {
		t.Fatal(err)
	}
	// Sender ref points at a member that was never created (deleted member / soft
	// string ref retained).
	if _, aerr := deps.MessageWriter.AddMessage(ctx, convservice.AddMessageCommand{
		ConversationID:   res.ConversationID,
		SenderIdentityID: "user:ghost-deleted",
		ContentKind:      conversation.MessageContentText,
		Content:          "left before you arrived",
		Direction:        conversation.DirectionInbound,
		Actor:            selfActor,
	}); aerr != nil {
		t.Fatal(aerr)
	}

	resp := orgScopedGet(t, s.URL+"/api/conversations?kind=channel", sess)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("list got %d (should not 500 on soft-ref sender)", resp.StatusCode)
	}
	var rows []map[string]any
	if err := json.NewDecoder(resp.Body).Decode(&rows); err != nil {
		t.Fatal(err)
	}
	for _, row := range rows {
		if row["name"] != "orphan" {
			continue
		}
		recent := row["recent_messages"].([]any)
		if len(recent) != 1 {
			t.Fatalf("orphan recent len = %d", len(recent))
		}
		m := recent[0].(map[string]any)
		// Friendly handle (cleaned bare id), never empty, never a crash.
		if dn, _ := m["sender_display_name"].(string); dn != "ghost-deleted" {
			t.Errorf("soft-ref sender display = %q, want friendly handle ghost-deleted", dn)
		}
	}
}

// v2.8.1 #278: each agents-list row carries last_activity_at + last_activity_content
// (plain-text preview), and BOTH null when the agent has no activity.
func TestAgentsList_Enrich_LastActivity_278(t *testing.T) {
	deps, db := setupAPIWithAuth(t)
	sess := setupTestSession(t, db, deps)
	saveWorkerInOrg(t, db, sess.OrgID, "w-enrich")
	s := newTestServer(t, deps)
	defer s.Close()
	ctx := context.Background()

	// Create two agents via the canonical one-step endpoint (identity+member+agent).
	mk := func(name string) string {
		resp := orgScopedPost(t, s.URL+"/api/members/agent",
			`{"display_name":"`+name+`","description":"d","model":"claude","cli":"claude-code","worker_id":"w-enrich"}`, sess)
		if resp.StatusCode != http.StatusCreated {
			t.Fatalf("create agent %s: got %d", name, resp.StatusCode)
		}
		var created map[string]any
		_ = json.NewDecoder(resp.Body).Decode(&created)
		id, _ := created["identity_id"].(string)
		if id == "" {
			t.Fatalf("create agent %s: missing identity_id", name)
		}
		return id
	}
	withActivity := mk("ActiveBot")
	_ = mk("QuietBot")

	// Append an activity event for ActiveBot only. Resolve its entity id (the
	// activity partition key) via the service.
	a, err := deps.AgentSvc.ResolveAgent(ctx, withActivity)
	if err != nil {
		t.Fatal(err)
	}
	// Payload carries markdown + a fenced code block (JSON-escaped newlines + the
	// three-backtick fence) to assert the preview strips it to plain text.
	fence := "```"
	payload := "{\"text\":\"working on **task** now\\n" + fence + "sh\\nls\\n" + fence + "\"}"
	ev, err := agent.NewActivityEvent(agent.NewActivityEventInput{
		ID: "ev-1", AgentID: a.ID(), EventType: agent.EventTypeAssistantText,
		Payload: payload, OccurredAt: t0Enrich,
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := agentsql.NewActivityEventRepo(db).Append(ctx, ev); err != nil {
		t.Fatal(err)
	}

	resp := orgScopedGet(t, s.URL+"/api/agents", sess)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("list got %d", resp.StatusCode)
	}
	var body struct {
		Agents []map[string]any `json:"agents"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		t.Fatal(err)
	}
	var active, quiet map[string]any
	for _, row := range body.Agents {
		switch row["name"] {
		case "ActiveBot":
			active = row
		case "QuietBot":
			quiet = row
		}
	}
	if active == nil || quiet == nil {
		t.Fatalf("missing agent rows: %+v", body.Agents)
	}
	// ActiveBot: last_activity_at present + content plain-text (fence stripped).
	if la, _ := active["last_activity_at"].(string); la == "" {
		t.Fatalf("ActiveBot last_activity_at empty: %+v", active)
	}
	lc, _ := active["last_activity_content"].(string)
	if lc == "" {
		t.Fatal("ActiveBot last_activity_content empty")
	}
	if strings.Contains(lc, "```") {
		t.Errorf("last_activity_content fence leaked, not plain-text: %q", lc)
	}
	// QuietBot: both null (key present, value null).
	if v, ok := quiet["last_activity_at"]; !ok || v != nil {
		t.Errorf("QuietBot last_activity_at = %v, want null", v)
	}
	if v, ok := quiet["last_activity_content"]; !ok || v != nil {
		t.Errorf("QuietBot last_activity_content = %v, want null", v)
	}
}
