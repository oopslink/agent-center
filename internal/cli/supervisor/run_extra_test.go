package supervisor

import (
	"strings"
	"testing"

	"github.com/oopslink/agent-center/internal/observability"
)

func refsAll() observability.EventRefs {
	return observability.EventRefs{
		TaskID:         "T",
		IssueID:        "I",
		ExecutionID:    "E",
		WorkerID:       "W",
		ConversationID: "C",
		InputRequestID: "IR",
		ProjectID:      "P",
	}
}

func TestParseTriggers_HappyAndEmpty(t *testing.T) {
	got, err := parseTriggers("a,b,c")
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 3 {
		t.Errorf("len = %d", len(got))
	}
	if _, err := parseTriggers(""); err == nil {
		t.Error("expected empty err")
	}
	if _, err := parseTriggers(",  ,"); err == nil {
		t.Error("expected all-empty err")
	}
}

func TestBufioScanner_MultiLine(t *testing.T) {
	r := strings.NewReader("a\nb\nc")
	scan := bufioScanner(r)
	var got []string
	for {
		line, ok := scan()
		if !ok {
			break
		}
		got = append(got, string(line))
	}
	if len(got) != 3 || got[0] != "a" || got[2] != "c" {
		t.Errorf("got %v", got)
	}
}

func TestBufioScanner_Empty(t *testing.T) {
	r := strings.NewReader("")
	scan := bufioScanner(r)
	if _, ok := scan(); ok {
		t.Error("empty should be !ok")
	}
}

func TestPayloadOneLine_Long(t *testing.T) {
	p := map[string]any{
		"short":  "x",
		"longer": strings.Repeat("y", 200),
		"int":    42,
	}
	out := payloadOneLine(p)
	if !strings.Contains(out, "truncated") {
		t.Errorf("missing truncation: %s", out)
	}
}

func TestRefsOneLine_AllFields(t *testing.T) {
	out := refsOneLine(refsAll())
	want := []string{"task_id=T", "issue_id=I", "execution_id=E", "worker_id=W", "conversation_id=C", "input_request_id=IR", "project_id=P"}
	for _, w := range want {
		if !strings.Contains(out, w) {
			t.Errorf("missing %s in %s", w, out)
		}
	}
}
