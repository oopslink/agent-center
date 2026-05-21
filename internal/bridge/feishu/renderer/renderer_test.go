package renderer_test

import (
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"github.com/oopslink/agent-center/internal/bridge/feishu/renderer"
)

func mustParseMap(t *testing.T, raw string) map[string]any {
	t.Helper()
	var m map[string]any
	if err := json.Unmarshal([]byte(raw), &m); err != nil {
		t.Fatalf("unmarshal %q: %v", raw, err)
	}
	return m
}

func TestRenderText(t *testing.T) {
	t.Parallel()
	r := renderer.New()
	out, err := r.RenderMessage(renderer.MessageInput{
		MessageID: "M-1", ContentKind: renderer.ContentKindText, Content: "hello",
	}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if out.MessageKind != renderer.MessageKindText {
		t.Fatalf("kind: %s", out.MessageKind)
	}
	m := mustParseMap(t, out.CardJSON)
	if m["text"] != "hello" {
		t.Fatalf("payload: %v", m)
	}
	if out.IdempotencyKey != "M-1" {
		t.Fatal("idempotency key lost")
	}
}

func TestRenderSystem(t *testing.T) {
	t.Parallel()
	r := renderer.New()
	out, err := r.RenderMessage(renderer.MessageInput{
		MessageID: "M-1", ContentKind: renderer.ContentKindSystem, Content: "issue opened",
	}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if out.MessageKind != renderer.MessageKindInteractive {
		t.Fatalf("kind: %s", out.MessageKind)
	}
	if !strings.Contains(out.CardJSON, "system") {
		t.Fatal("system label missing")
	}
}

func TestRenderAgentFindingTextOnly(t *testing.T) {
	t.Parallel()
	r := renderer.New()
	out, err := r.RenderMessage(renderer.MessageInput{
		MessageID: "M-1", ContentKind: renderer.ContentKindAgentFinding,
		Content: "found a thing", Sender: "agent:claudecode",
	}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if out.MessageKind != renderer.MessageKindText {
		t.Fatal("agent_finding without input_request should be text")
	}
	m := mustParseMap(t, out.CardJSON)
	if !strings.Contains(m["text"].(string), "agent:claudecode") {
		t.Fatalf("sender label missing: %v", m)
	}
}

func TestRenderAgentFindingCardOptions(t *testing.T) {
	t.Parallel()
	r := renderer.New()
	for _, n := range []int{1, 3, 10, 0} {
		opts := make([]string, n)
		for i := range opts {
			opts[i] = "opt-" + string(rune('A'+i))
		}
		out, err := r.RenderMessage(renderer.MessageInput{
			MessageID: "M-1", ContentKind: renderer.ContentKindAgentFinding,
			Content: "please choose", InputRequestRef: "IR-1",
		}, &renderer.InputRequestInput{ID: "IR-1", Question: "pick", Options: opts})
		if err != nil {
			t.Fatalf("n=%d: %v", n, err)
		}
		if out.MessageKind != renderer.MessageKindInteractive {
			t.Fatalf("n=%d: kind %s", n, out.MessageKind)
		}
		card := mustParseMap(t, out.CardJSON)
		elements := card["elements"].([]any)
		actions := elements[len(elements)-1].(map[string]any)["actions"].([]any)
		want := n + 2 // + [自己写][取消]
		if len(actions) != want {
			t.Fatalf("n=%d: got %d buttons, want %d", n, len(actions), want)
		}
	}
}

func TestRenderAgentFindingMissingInputRequest(t *testing.T) {
	t.Parallel()
	r := renderer.New()
	if _, err := r.RenderMessage(renderer.MessageInput{
		MessageID: "M-1", ContentKind: renderer.ContentKindAgentFinding,
		Content: "x", InputRequestRef: "IR",
	}, nil); !errors.Is(err, renderer.ErrMissingInputRequest) {
		t.Fatalf("want ErrMissingInputRequest, got %v", err)
	}
}

func TestRenderSupervisorSummary(t *testing.T) {
	t.Parallel()
	r := renderer.New()
	out, err := r.RenderMessage(renderer.MessageInput{
		MessageID: "M-1", ContentKind: renderer.ContentKindSupervisorSummary, Content: "summary",
	}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if out.MessageKind != renderer.MessageKindInteractive {
		t.Fatal("supervisor_summary should be interactive")
	}
	if !strings.Contains(out.CardJSON, "supervisor_summary_confirm") {
		t.Fatal("confirm action missing")
	}
}

func TestRenderConclusionDraft(t *testing.T) {
	t.Parallel()
	r := renderer.New()
	out, err := r.RenderMessage(renderer.MessageInput{
		MessageID: "M-1", ContentKind: renderer.ContentKindConclusionDraft, Content: "concl",
	}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out.CardJSON, "conclusion_confirm") {
		t.Fatal("conclusion_confirm missing")
	}
}

func TestRenderTaskProposal(t *testing.T) {
	t.Parallel()
	r := renderer.New()
	out, err := r.RenderMessage(renderer.MessageInput{
		MessageID: "M-1", ContentKind: renderer.ContentKindTaskProposal, Content: "proposal",
	}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out.CardJSON, "task_proposal") {
		t.Fatal("task_proposal label missing")
	}
}

func TestRenderUnknownContentKind(t *testing.T) {
	t.Parallel()
	r := renderer.New()
	if _, err := r.RenderMessage(renderer.MessageInput{
		MessageID: "M-1", ContentKind: "weird", Content: "x",
	}, nil); !errors.Is(err, renderer.ErrUnknownContentKind) {
		t.Fatalf("want ErrUnknownContentKind, got %v", err)
	}
}

func TestRenderMessageEmptyAndMissingID(t *testing.T) {
	t.Parallel()
	r := renderer.New()
	if _, err := r.RenderMessage(renderer.MessageInput{
		MessageID: "", ContentKind: renderer.ContentKindText, Content: "x",
	}, nil); err == nil {
		t.Fatal("want id required")
	}
	if _, err := r.RenderMessage(renderer.MessageInput{
		MessageID: "M-1", ContentKind: renderer.ContentKindText, Content: "",
	}, nil); !errors.Is(err, renderer.ErrEmptyContent) {
		t.Fatalf("want ErrEmptyContent, got %v", err)
	}
}

func TestRenderRootCard(t *testing.T) {
	t.Parallel()
	r := renderer.New()
	for _, kind := range []string{"task", "issue"} {
		out, err := r.RenderRootCard(renderer.RootCardInput{
			Conversation: renderer.ConversationInput{
				ConversationID: "C-1", Kind: kind, Title: "Some Task",
			},
			SubjectRef: kind + " #42",
		})
		if err != nil {
			t.Fatalf("kind=%s: %v", kind, err)
		}
		if out.MessageKind != renderer.MessageKindInteractive {
			t.Fatalf("kind=%s should be interactive", kind)
		}
		if !strings.Contains(out.CardJSON, "#42") {
			t.Fatalf("subject missing: %s", out.CardJSON)
		}
		if !strings.Contains(out.CardJSON, "Some Task") {
			t.Fatalf("title missing: %s", out.CardJSON)
		}
	}
	if _, err := r.RenderRootCard(renderer.RootCardInput{
		Conversation: renderer.ConversationInput{Kind: "dm"},
	}); !errors.Is(err, renderer.ErrUnknownContentKind) {
		t.Fatalf("want err for non-task/issue kind, got %v", err)
	}
}

func TestRenderRootCardFallbackSubject(t *testing.T) {
	t.Parallel()
	r := renderer.New()
	out, err := r.RenderRootCard(renderer.RootCardInput{
		Conversation: renderer.ConversationInput{ConversationID: "C-1", Kind: "task"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out.CardJSON, "C-1") {
		t.Fatal("fallback subject missing")
	}
}
