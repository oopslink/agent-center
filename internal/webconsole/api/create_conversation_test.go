package api

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"
	"testing"

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

	resp, err := http.Post(s.URL+"/api/conversations",
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
	resp, _ := http.Post(s.URL+"/api/conversations",
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
	resp, _ := http.Post(s.URL+"/api/conversations",
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
	resp, _ := http.Post(s.URL+"/api/conversations",
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
	resp, _ := http.Post(s.URL+"/api/conversations",
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
	resp, _ := http.Post(s.URL+"/api/conversations",
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

func TestAPI_CreateConversation_DM_DedupesCallerInMembers(t *testing.T) {
	deps, _ := setupAPI(t)
	s := newTestServer(t, deps)
	defer s.Close()
	// Caller is user:hayang; supplying user:hayang explicitly in members
	// shouldn't add a duplicate participant.
	resp, _ := http.Post(s.URL+"/api/conversations",
		"application/json",
		strings.NewReader(`{"kind":"dm","members":["user:hayang","agent:s-1"]}`))
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("got %d", resp.StatusCode)
	}
	var body map[string]any
	_ = json.NewDecoder(resp.Body).Decode(&body)
	conv, _ := deps.ConvRepo.FindByID(context.Background(), conversation.ConversationID(body["conversation_id"].(string)))
	if got := len(conv.Participants()); got != 2 {
		t.Fatalf("expected 2 participants (dedup caller); got %d", got)
	}
}

func TestAPI_CreateConversation_BadKind(t *testing.T) {
	deps, _ := setupAPI(t)
	s := newTestServer(t, deps)
	defer s.Close()
	resp, _ := http.Post(s.URL+"/api/conversations",
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
	resp, _ := http.Post(s.URL+"/api/conversations",
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
	resp, _ := http.Post(s.URL+"/api/conversations",
		"application/json",
		strings.NewReader(`{"kind":"channel","name":"x"}`))
	if resp.StatusCode != http.StatusNotImplemented {
		t.Fatalf("got %d", resp.StatusCode)
	}
}

func TestAPI_CreateConversation_DM_MessageWriterNotWired(t *testing.T) {
	deps, _ := setupAPI(t)
	deps.MessageWriter = nil
	s := newTestServer(t, deps)
	defer s.Close()
	resp, _ := http.Post(s.URL+"/api/conversations",
		"application/json",
		strings.NewReader(`{"kind":"dm","members":["agent:x"]}`))
	if resp.StatusCode != http.StatusNotImplemented {
		t.Fatalf("got %d", resp.StatusCode)
	}
}
