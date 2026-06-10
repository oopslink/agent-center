package api

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/oopslink/agent-center/internal/agent"
	agentsql "github.com/oopslink/agent-center/internal/agent/sqlite"
	"github.com/oopslink/agent-center/internal/conversation"
	"github.com/oopslink/agent-center/internal/identity"
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

	// participants = ≤5 PREVIEW array + separate participant_count (total). Contract
	// lock (PD/Dev2): list `participants` is NOT the full set; participant_count is.
	members, _ := general["participants"].([]any)
	if len(members) != 2 {
		t.Fatalf("participants (preview) len = %d, want 2: %+v", len(members), general["participants"])
	}
	if cnt, _ := general["participant_count"].(float64); cnt != 2 {
		t.Fatalf("participant_count = %v, want 2", general["participant_count"])
	}
	// kind label: agent member → "agent".
	var sawAgent bool
	for _, mm := range members {
		m := mm.(map[string]any)
		if m["identity_id"] == "agent:agent-bot" {
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

	// recent_messages: <=3, newest-first, plain-text content (fence stripped).
	recent, _ := general["recent_messages"].([]any)
	if len(recent) != 3 {
		t.Fatalf("recent_messages len = %d, want 3", len(recent))
	}
	newest := recent[0].(map[string]any)
	content, _ := newest["content"].(string)
	if content == "" {
		t.Fatal("newest content empty")
	}
	if strings.Contains(content, "```") || strings.Contains(content, "fmt.Println") {
		t.Errorf("content not plain-text (fence leaked): %q", content)
	}
	if newest["posted_at"] == "" || newest["posted_at"] == nil {
		t.Error("recent message missing posted_at")
	}
	if newest["sender_display_name"] == "" || newest["sender_display_name"] == nil {
		t.Error("recent message missing sender_display_name")
	}
	// newest-first: first content is the third (markdown) message.
	if !strings.Contains(content, "third") {
		t.Errorf("recent_messages not newest-first; first content = %q", content)
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
// member row does not resolve must NOT crash — sender_display_name = "" (F1 miss-sentinel).
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
		// Contract lock (a): an unresolved (deleted) sender returns EMPTY
		// sender_display_name (the F1 miss-sentinel — FE renders "(deleted)"),
		// NOT a cleaned handle. The sender_identity_id is retained (for hover).
		// No crash / no 500.
		if dn, _ := m["sender_display_name"].(string); dn != "" {
			t.Errorf("soft-ref sender display = %q, want empty (miss-sentinel for FE (deleted))", dn)
		}
		if sid, _ := m["sender_identity_id"].(string); sid != "user:ghost-deleted" {
			t.Errorf("soft-ref sender_identity_id = %q, want retained user:ghost-deleted", sid)
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

// v2.8.1 #278 RED-LINE (PD pre-tag): the list enrich (recent_messages / last_activity)
// adds message + activity CONTENT to the list response — assert it never leaks across
// the org boundary. org-A's channel list must contain neither org-B's channel nor its
// message content. The enrich already runs only on the org-scoped page's IDs, but this
// locks the boundary so a future list-query change can't silently regress it.
func TestConversationsList_Enrich_NoCrossOrgLeak_278(t *testing.T) {
	deps, db := setupAPIWithAuth(t)
	sess := setupTestSession(t, db, deps) // org-A (the caller's org)
	s := newTestServer(t, deps)
	defer s.Close()
	ctx := context.Background()
	selfRef := conversation.IdentityRef("user:" + sess.IdentityID)
	selfActor := observability.Actor("user:" + sess.IdentityID)

	// org-A channel.
	if _, err := deps.ChannelMgmtSvc.CreateChannel(ctx, convservice.CreateChannelCommand{
		Name: "alpha", OrganizationID: sess.OrgID, CreatedBy: selfRef, Actor: selfActor,
	}); err != nil {
		t.Fatal(err)
	}

	// A SECOND org (org-B) the same user also belongs to, with its own channel + a
	// secret message. Same user in both orgs makes CreateChannel's membership pass;
	// the LIST stays scoped to org-A via ?org_slug.
	orgB, err := identity.OrganizationFactory{}.New("orgb", "Org B", sess.IdentityID)
	if err != nil {
		t.Fatal(err)
	}
	if err := identity.NewSQLiteOrganizationRepo(db).Save(ctx, orgB); err != nil {
		t.Fatal(err)
	}
	memberB, err := identity.MemberFactory{}.New(orgB.ID(), sess.IdentityID, identity.RoleOwner, nil)
	if err != nil {
		t.Fatal(err)
	}
	if err := identity.NewSQLiteMemberRepo(db).Save(ctx, memberB); err != nil {
		t.Fatal(err)
	}
	resB, err := deps.ChannelMgmtSvc.CreateChannel(ctx, convservice.CreateChannelCommand{
		Name: "bravo", OrganizationID: orgB.ID(), CreatedBy: selfRef, Actor: selfActor,
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, aerr := deps.MessageWriter.AddMessage(ctx, convservice.AddMessageCommand{
		ConversationID: resB.ConversationID, SenderIdentityID: selfRef,
		ContentKind: conversation.MessageContentText, Content: "ORG-B-SECRET-do-not-leak",
		Direction: conversation.DirectionInbound, Actor: selfActor,
	}); aerr != nil {
		t.Fatal(aerr)
	}

	// List scoped to org-A.
	resp := orgScopedGet(t, s.URL+"/api/conversations?kind=channel", sess)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("list got %d", resp.StatusCode)
	}
	body, _ := io.ReadAll(resp.Body)
	raw := string(body)
	if strings.Contains(raw, "ORG-B-SECRET-do-not-leak") {
		t.Fatal("CROSS-ORG LEAK: org-B message content present in org-A list enrich")
	}
	if strings.Contains(raw, "bravo") {
		t.Fatal("CROSS-ORG LEAK: org-B channel present in org-A list")
	}
	if !strings.Contains(raw, "alpha") {
		t.Fatalf("org-A channel missing from its own list: %s", raw)
	}
}
