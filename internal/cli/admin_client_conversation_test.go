// Package cli — admin_client_conversation_test.go: end-to-end tests
// that prove the v2.2 Phase B Client + admin transport works for the
// conversation BC handlers (channel/conversation/message commands).
//
// Mirrors admin_client_workforce_test.go: spin up an in-process admin
// endpoint via setupAdminServerForTests + drive the router with the
// same args a real CLI invocation would use, asserting that `a.Client`
// gets routed through (legacy direct-service fallback NOT exercised
// because the helper wires both Client and Services; the dual-mode
// branch in handlers picks Client when non-nil).
package cli

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestClient_ChannelCreateAndShow_OverAdminEndpoint(t *testing.T) {
	app, cleanup := setupAdminServerForTests(t)
	defer cleanup()

	create := findCmd(app.ChannelCommands(), "create")
	out, _, code := runHandler(t, create, []string{"--name=alpha", "--description=planning", "--format=json"})
	if code != ExitOK {
		t.Fatalf("create exit=%d out=%s", code, out)
	}
	var created map[string]any
	if err := json.Unmarshal([]byte(out), &created); err != nil {
		t.Fatalf("decode create: %v body=%s", err, out)
	}
	if created["conversation_id"] == "" {
		t.Fatal("conversation_id empty")
	}

	show := findCmd(app.ChannelCommands(), "show")
	out2, _, code := runHandler(t, show, []string{"alpha", "--format=json"})
	if code != ExitOK {
		t.Fatalf("show exit=%d out=%s", code, out2)
	}
	var shown map[string]any
	if err := json.Unmarshal([]byte(out2), &shown); err != nil {
		t.Fatalf("decode show: %v body=%s", err, out2)
	}
	if shown["name"] != "alpha" {
		t.Fatalf("show name = %v", shown["name"])
	}

	list := findCmd(app.ChannelCommands(), "list")
	out3, _, code := runHandler(t, list, []string{"--format=json"})
	if code != ExitOK {
		t.Fatalf("list exit=%d out=%s", code, out3)
	}
	if !strings.Contains(out3, "alpha") {
		t.Fatalf("list missing alpha: %s", out3)
	}
}

func TestClient_ConversationOpenAndSendAndTail_OverAdminEndpoint(t *testing.T) {
	app, cleanup := setupAdminServerForTests(t)
	defer cleanup()

	// Open a channel-kind conversation through the conversation open path.
	open := findCmd(app.ConversationCommands(), "open")
	out, _, code := runHandler(t, open, []string{"--kind=channel", "--name=conv-open-test", "--format=json"})
	if code != ExitOK {
		t.Fatalf("open exit=%d out=%s", code, out)
	}
	var opened map[string]any
	_ = json.Unmarshal([]byte(out), &opened)
	convID, _ := opened["conversation_id"].(string)
	if convID == "" {
		t.Fatal("conversation_id empty")
	}

	// Send a message.
	send := findCmd(app.ConversationCommands(), "send")
	_, _, code = runHandler(t, send, []string{convID, "hello", "world", "--format=json"})
	if code != ExitOK {
		t.Fatalf("send exit=%d", code)
	}

	// Tail (non-follow) and verify the message appears.
	tail := findCmd(app.ConversationCommands(), "tail")
	out2, _, code := runHandler(t, tail, []string{convID, "--tail=10", "--format=json"})
	if code != ExitOK {
		t.Fatalf("tail exit=%d out=%s", code, out2)
	}
	if !strings.Contains(out2, "hello world") {
		t.Fatalf("tail output missing message: %s", out2)
	}

	// Show — must include participants array.
	show := findCmd(app.ConversationCommands(), "show")
	out3, _, code := runHandler(t, show, []string{convID, "--format=json"})
	if code != ExitOK {
		t.Fatalf("show exit=%d out=%s", code, out3)
	}
	var shown map[string]any
	_ = json.Unmarshal([]byte(out3), &shown)
	if _, ok := shown["participants"]; !ok {
		t.Fatalf("show missing participants: %s", out3)
	}
}

func TestClient_ConvList_FilterByKind_OverAdminEndpoint(t *testing.T) {
	app, cleanup := setupAdminServerForTests(t)
	defer cleanup()

	// Create two channels so list has something to filter.
	create := findCmd(app.ChannelCommands(), "create")
	if _, _, code := runHandler(t, create, []string{"--name=one", "--format=json"}); code != ExitOK {
		t.Fatalf("create one exit=%d", code)
	}
	if _, _, code := runHandler(t, create, []string{"--name=two", "--format=json"}); code != ExitOK {
		t.Fatalf("create two exit=%d", code)
	}

	list := findCmd(app.ConversationCommands(), "list")
	out, _, code := runHandler(t, list, []string{"--kind=channel", "--format=json"})
	if code != ExitOK {
		t.Fatalf("list exit=%d out=%s", code, out)
	}
	for _, name := range []string{"one", "two"} {
		if !strings.Contains(out, name) {
			t.Fatalf("list output missing %q: %s", name, out)
		}
	}
}

func TestClient_MessageRefs_EmptyResult_OverAdminEndpoint(t *testing.T) {
	app, cleanup := setupAdminServerForTests(t)
	defer cleanup()

	refs := findCmd(app.MessageCommands(), "refs")
	out, _, code := runHandler(t, refs, []string{"M-DOES-NOT-EXIST", "--format=json"})
	if code != ExitOK {
		t.Fatalf("refs exit=%d out=%s", code, out)
	}
	if strings.TrimSpace(out) == "" {
		t.Fatalf("refs JSON should be at minimum []: %q", out)
	}
}

// v2.3-1 (task #24): channel-leave Client path covers the new
// /admin/conversation/participant/leave endpoint. Pre-v2.3 this CLI
// fell back to the legacy direct service.
func TestClient_ChannelLeave_OverAdminEndpoint(t *testing.T) {
	app, cleanup := setupAdminServerForTests(t)
	defer cleanup()

	create := findCmd(app.ChannelCommands(), "create")
	if _, _, code := runHandler(t, create, []string{"--name=leaveme", "--format=json"}); code != ExitOK {
		t.Fatalf("create exit=%d", code)
	}
	leave := findCmd(app.ChannelCommands(), "leave")
	out, errOut, code := runHandler(t, leave, []string{"leaveme"})
	if code != ExitOK {
		t.Fatalf("leave exit=%d out=%s err=%s", code, out, errOut)
	}
	if !strings.Contains(out, "left leaveme") {
		t.Fatalf("leave output unexpected: %s", out)
	}
}

// v2.3-1 (task #24): convReadHandler --tail Client path covers the new
// /admin/conversation/msg/find-recent endpoint. Pre-v2.3 the CLI did
// a client-side trim against the 200-cap find-by-conversation-id helper.
func TestClient_ConversationRead_TailUsesFindRecent_OverAdminEndpoint(t *testing.T) {
	app, cleanup := setupAdminServerForTests(t)
	defer cleanup()

	open := findCmd(app.ConversationCommands(), "open")
	out, _, code := runHandler(t, open, []string{"--kind=channel", "--name=fr-test", "--format=json"})
	if code != ExitOK {
		t.Fatalf("open exit=%d out=%s", code, out)
	}
	var opened map[string]any
	_ = json.Unmarshal([]byte(out), &opened)
	convID, _ := opened["conversation_id"].(string)

	send := findCmd(app.ConversationCommands(), "send")
	for i, body := range []string{"alpha", "beta", "gamma", "delta"} {
		if _, _, code := runHandler(t, send, []string{convID, body, "--format=json"}); code != ExitOK {
			t.Fatalf("send %d exit=%d", i, code)
		}
	}
	read := findCmd(app.ConversationCommands(), "read")
	out2, _, code := runHandler(t, read, []string{convID, "--tail=2", "--format=json"})
	if code != ExitOK {
		t.Fatalf("read exit=%d out=%s", code, out2)
	}
	// Only the 2 most-recent messages should land — earlier ones suppressed
	// at the server (proper FindRecent), not by client-side trim.
	if !strings.Contains(out2, "gamma") || !strings.Contains(out2, "delta") {
		t.Fatalf("read --tail=2 should include gamma+delta: %s", out2)
	}
	if strings.Contains(out2, "alpha") {
		t.Fatalf("read --tail=2 should NOT include alpha: %s", out2)
	}
}
