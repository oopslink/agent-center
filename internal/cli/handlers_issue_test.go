package cli

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/oopslink/agent-center/internal/conversation"
	convservice "github.com/oopslink/agent-center/internal/conversation/service"
	"github.com/oopslink/agent-center/internal/discussion"
	"github.com/oopslink/agent-center/internal/observability"
)

func newDMOpenCmd(title string, actor observability.Actor) convservice.OpenCommand {
	return convservice.OpenCommand{
		Kind:      conversation.ConversationKindDM,
		Name:      title,
		CreatedBy: conversation.IdentityRef(actor),
		Actor:     actor,
	}
}

// seedDiscussionIssue persists an open discussion Issue directly via the
// repository (the old `issue open` CLI command was removed in #132). It
// returns the issue_id so the read/observability-side commands that are still
// supported (bind-conversation / link-conversation) can be exercised.
func seedDiscussionIssue(t *testing.T, app *App) string {
	t.Helper()
	id := discussion.IssueID("I-" + t.Name())
	iss, err := discussion.NewIssue(discussion.NewIssueInput{
		ID:                 id,
		ProjectID:          "p-1",
		Title:              "hello",
		OpenedByIdentityID: string(app.DefaultActor()),
		Origin:             discussion.OriginCLI,
		OpenedAt:           time.Now(),
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := app.IssueRepo.Save(context.Background(), iss); err != nil {
		t.Fatal(err)
	}
	return string(id)
}

func TestCLI_IssueBindConversation_Auto(t *testing.T) {
	app := newTestApp(t)
	id := seedDiscussionIssue(t, app)
	bindCmd := findCmd(app.IssueCommands(), "bind-conversation")
	out, _, code := runHandler(t, bindCmd, []string{id, "--auto", "--format=json"})
	if code != ExitOK {
		t.Fatalf("bind: %d out=%s", code, out)
	}
	var p map[string]string
	if err := json.Unmarshal([]byte(strings.TrimSpace(out)), &p); err != nil {
		t.Fatalf("not json: %s", out)
	}
	if p["conversation_id"] == "" {
		t.Fatalf("expected a freshly created conversation_id, got %+v", p)
	}
	if p["issue_id"] != id {
		t.Fatalf("expected issue_id=%s, got %+v", id, p)
	}
}

func TestCLI_IssueBindConversation_FlagMutexAndUsage(t *testing.T) {
	app := newTestApp(t)
	cmd := findCmd(app.IssueCommands(), "bind-conversation")
	cases := []struct {
		args []string
		want ExitCode
	}{
		{[]string{}, ExitUsage},
		{[]string{"X"}, ExitUsage},
		{[]string{"X", "--auto", "--to=Y"}, ExitUsage},
	}
	for i, c := range cases {
		_, _, code := runHandler(t, cmd, c.args)
		if code != c.want {
			t.Errorf("case %d: code=%d want %d", i, code, c.want)
		}
	}
}

func TestCLI_IssueLinkConversation(t *testing.T) {
	app := newTestApp(t)
	id := seedDiscussionIssue(t, app)
	// stand-alone DM conv to link — use MessageWriter facade to get a proper RFC3339Nano row
	openRes, err := app.MessageWriter.OpenConversation(context.Background(),
		newDMOpenCmd("link-target", app.DefaultActor()))
	if err != nil {
		t.Fatal(err)
	}
	cmd := findCmd(app.IssueCommands(), "link-conversation")
	if _, _, code := runHandler(t, cmd, []string{id, "--conversation=" + string(openRes.ConversationID), "--format=json"}); code != ExitOK {
		t.Fatalf("link: %d", code)
	}
	cases := []struct {
		args []string
		want ExitCode
	}{
		{[]string{}, ExitUsage},
		{[]string{id}, ExitUsage},
		{[]string{"--conversation=X"}, ExitUsage},
		{[]string{id, "--conversation=ghost"}, ExitNotFound},
	}
	for i, c := range cases {
		_, _, code := runHandler(t, cmd, c.args)
		if code != c.want {
			t.Errorf("case %d args=%v: code=%d want %d", i, c.args, code, c.want)
		}
	}
}

func TestCLI_IssueErrorMapping_AllSentinels(t *testing.T) {
	// Direct unit check that MapDomainError handles each new sentinel.
	tests := []struct {
		err  error
		want string
	}{
		{discussion.ErrIssueNotFound, "issue_not_found"},
		{discussion.ErrIssueAlreadyExists, "issue_already_exists"},
		{discussion.ErrIssueInvalidTransition, "issue_invalid_transition"},
		{discussion.ErrIssueVersionConflict, "issue_version_conflict"},
		{discussion.ErrIssueAlreadyConcluded, "issue_already_concluded"},
		{discussion.ErrIssueWithdrawn, "issue_withdrawn"},
		{discussion.ErrIssueNoConversationBound, "issue_no_conversation_bound"},
		{discussion.ErrInvalidOrigin, "issue_invalid_origin"},
		{discussion.ErrResolutionInvalid, "issue_invalid_resolution"},
	}
	for _, tc := range tests {
		reason, _, ok := MapDomainError(tc.err)
		if !ok || reason != tc.want {
			t.Errorf("want %s, got reason=%s ok=%v", tc.want, reason, ok)
		}
	}
	// unknown error not mapped
	if _, _, ok := MapDomainError(errors.New("random")); ok {
		t.Fatal("random err should not match")
	}
}
