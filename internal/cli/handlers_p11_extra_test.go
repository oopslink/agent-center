package cli

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/oopslink/agent-center/internal/conversation"
)

// agentList JSON output
func TestCLI_AgentList_JSONHasWorker(t *testing.T) {
	app := newTestApp(t)
	_, _, _ = runOn(t, app, "agent", "create", []string{
		"--name=aa", "--agent-cli=claudecode", "--worker=w-1",
	})
	out, _, _ := runOn(t, app, "agent", "list", []string{"--format=json"})
	var arr []map[string]any
	_ = json.Unmarshal([]byte(strings.TrimSpace(out)), &arr)
	if len(arr) != 1 || arr[0]["worker_id"] != "w-1" {
		t.Fatalf("got %v", arr)
	}
}

// agentList state + worker filters
func TestCLI_AgentList_Filters(t *testing.T) {
	app := newTestApp(t)
	_, _, _ = runOn(t, app, "agent", "create", []string{
		"--name=aa", "--agent-cli=claudecode", "--worker=w-1",
	})
	_, _, code := runOn(t, app, "agent", "list", []string{"--state=idle"})
	if code != ExitOK {
		t.Fatal()
	}
	_, _, code = runOn(t, app, "agent", "list", []string{"--worker=w-1"})
	if code != ExitOK {
		t.Fatal()
	}
}

// agentArchive happy path — needs a non-builtin AgentInstance.
func TestCLI_AgentArchive_Happy(t *testing.T) {
	app := newTestApp(t)
	out, _, _ := runOn(t, app, "agent", "create", []string{
		"--name=ar1", "--agent-cli=claudecode", "--worker=w-1", "--format=json",
	})
	var m map[string]any
	_ = json.Unmarshal([]byte(strings.TrimSpace(out)), &m)
	id := m["id"].(string)
	_, _, code := runOn(t, app, "agent", "archive", []string{id,
		"--message=done", "--version=1"})
	if code != ExitOK {
		t.Fatalf("code %d", code)
	}
}

// agentShow JSON
func TestCLI_AgentShow_JSON(t *testing.T) {
	app := newTestApp(t)
	out, _, _ := runOn(t, app, "agent", "create", []string{
		"--name=as1", "--agent-cli=claudecode", "--worker=w-1", "--format=json",
	})
	var m map[string]any
	_ = json.Unmarshal([]byte(strings.TrimSpace(out)), &m)
	id := m["id"].(string)
	out2, _, _ := runOn(t, app, "agent", "show", []string{id, "--format=json"})
	var m2 map[string]any
	_ = json.Unmarshal([]byte(strings.TrimSpace(out2)), &m2)
	if m2["name"] != "as1" {
		t.Fatalf("got %v", m2)
	}
}

// Conversation refs JSON
func TestCLI_ConvRefs_JSON(t *testing.T) {
	app := newTestApp(t)
	_, _, code := runOn(t, app, "conversation", "refs", []string{"C-X", "--format=json"})
	if code != ExitOK {
		t.Fatalf("code %d", code)
	}
}

// Message refs JSON
func TestCLI_MessageRefs_JSON(t *testing.T) {
	app := newTestApp(t)
	_, _, code := runOn(t, app, "message", "refs", []string{"M-X", "--format=json"})
	if code != ExitOK {
		t.Fatalf("code %d", code)
	}
}

// Secret list with kind + state filters
func TestCLI_SecretList_Filters(t *testing.T) {
	app := newAppWithSecret(t)
	_, _, _ = runOn(t, app, "secret", "create", []string{
		"--name=sk1", "--kind=mcp", "--value-file=" + writeTempFile(t, "v"),
	})
	out, _, _ := runOn(t, app, "secret", "list", []string{"--kind=mcp", "--format=json"})
	var arr []map[string]any
	_ = json.Unmarshal([]byte(strings.TrimSpace(out)), &arr)
	if len(arr) != 1 {
		t.Fatalf("got %d", len(arr))
	}
}

// Secret show by-name JSON
func TestCLI_SecretShow_ByNameJSON(t *testing.T) {
	app := newAppWithSecret(t)
	_, _, _ = runOn(t, app, "secret", "create", []string{
		"--name=ss1", "--value-file=" + writeTempFile(t, "v"),
	})
	out, _, code := runOn(t, app, "secret", "show", []string{"ss1", "--by-name", "--format=json"})
	if code != ExitOK {
		t.Fatalf("code %d", code)
	}
	var m map[string]any
	_ = json.Unmarshal([]byte(strings.TrimSpace(out)), &m)
	if m["name"] != "ss1" {
		t.Fatalf("got %v", m)
	}
}

// Secret show by-id JSON (default mode)
func TestCLI_SecretShow_ByIDHappy(t *testing.T) {
	app := newAppWithSecret(t)
	out, _, _ := runOn(t, app, "secret", "create", []string{
		"--name=ss2", "--value-file=" + writeTempFile(t, "v"), "--format=json",
	})
	var m map[string]any
	_ = json.Unmarshal([]byte(strings.TrimSpace(out)), &m)
	id := m["id"].(string)
	out2, _, code := runOn(t, app, "secret", "show", []string{id})
	if code != ExitOK {
		t.Fatalf("code %d", code)
	}
	if !strings.Contains(out2, "ss2") {
		t.Fatalf("got %s", out2)
	}
}

// Secret revoke with explicit --version
func TestCLI_SecretRevoke_WithVersion(t *testing.T) {
	app := newAppWithSecret(t)
	out, _, _ := runOn(t, app, "secret", "create", []string{
		"--name=sr1", "--value-file=" + writeTempFile(t, "v"), "--format=json",
	})
	var m map[string]any
	_ = json.Unmarshal([]byte(strings.TrimSpace(out)), &m)
	id := m["id"].(string)
	_, _, code := runOn(t, app, "secret", "revoke", []string{id,
		"--message=done", "--version=1"})
	if code != ExitOK {
		t.Fatalf("code %d", code)
	}
}

// convTail JSON
func TestCLI_ConvTail_JSON(t *testing.T) {
	app := newTestApp(t)
	_, _, _ = runOn(t, app, "channel", "create", []string{"--name=tjson"})
	out, _, _ := runOn(t, app, "channel", "show", []string{"tjson", "--format=json"})
	var m map[string]any
	_ = json.Unmarshal([]byte(strings.TrimSpace(out)), &m)
	cid := m["conversation_id"].(string)
	_, _, _ = runOn(t, app, "conversation", "send", []string{cid, "hi"})
	_, _, code := runOn(t, app, "conversation", "tail", []string{cid, "--tail=10", "--format=json"})
	if code != ExitOK {
		t.Fatalf("code %d", code)
	}
}

// Conversation refs (forward) with seeded refs
func TestCLI_ConvRefs_WithSeededRefs(t *testing.T) {
	app := newTestApp(t)
	ctx := context.Background()
	_, _, _ = runOn(t, app, "channel", "create", []string{"--name=csrc"})
	srcConv, _ := app.ConvRepo.FindByName(ctx, "csrc")
	_, _ = app.MessageWriter.AddMessage(ctx, addMsgCmd(app, srcConv.ID()))
	msgs, _ := app.MsgRepo.FindByConversationID(ctx, srcConv.ID(), conversation.MessageFilter{Limit: 1})
	msgID := msgs[0].ID()
	child, _ := conversation.NewConversation(conversation.NewConversationInput{
		ID: "CHILD-CR", Kind: conversation.ConversationKindIssue,
		Name: "X", CreatedBy: "user:hayang", OpenedAt: app.Clock.Now(),
	})
	_ = app.ConvRepo.Save(ctx, child)
	_, _ = app.CarryOverSvc.Materialise(ctx, materialiseCmd(app, child.ID(), srcConv.ID(), msgID))
	out, _, code := runOn(t, app, "conversation", "refs", []string{"CHILD-CR", "--format=json"})
	if code != ExitOK {
		t.Fatalf("code %d", code)
	}
	var arr []map[string]any
	_ = json.Unmarshal([]byte(strings.TrimSpace(out)), &arr)
	if len(arr) != 1 {
		t.Fatalf("got %d refs", len(arr))
	}
}

// MessageRefsHandler JSON with seeded refs
func TestCLI_MessageRefs_WithSeededRefs(t *testing.T) {
	app := newTestApp(t)
	ctx := context.Background()
	_, _, _ = runOn(t, app, "channel", "create", []string{"--name=msrc"})
	srcConv, _ := app.ConvRepo.FindByName(ctx, "msrc")
	_, _ = app.MessageWriter.AddMessage(ctx, addMsgCmd(app, srcConv.ID()))
	msgs, _ := app.MsgRepo.FindByConversationID(ctx, srcConv.ID(), conversation.MessageFilter{Limit: 1})
	child, _ := conversation.NewConversation(conversation.NewConversationInput{
		ID: "CHILD-MR", Kind: conversation.ConversationKindIssue,
		Name: "Y", CreatedBy: "user:hayang", OpenedAt: app.Clock.Now(),
	})
	_ = app.ConvRepo.Save(ctx, child)
	_, _ = app.CarryOverSvc.Materialise(ctx, materialiseCmd(app, child.ID(), srcConv.ID(), msgs[0].ID()))
	out, _, _ := runOn(t, app, "message", "refs", []string{string(msgs[0].ID()), "--format=json"})
	var arr []map[string]any
	_ = json.Unmarshal([]byte(strings.TrimSpace(out)), &arr)
	if len(arr) != 1 {
		t.Fatalf("got %d", len(arr))
	}
}
