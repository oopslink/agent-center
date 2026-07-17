package orchestrator

// issueinline_test.go — regression locks for I109 ① (spawn-time inline expansion of
// issue references).
//
// The load-bearing test is TestHandleWork_InlinesIssueBodyIntoSpawnedPrompt: it
// drives the REAL HandleWork fork chain and asserts the prompt the runner builder
// actually received contains the issue's BODY TEXT. It deliberately does NOT assert
// "buildPrompt called the expansion function" — that would pass just as happily if
// the expansion produced nothing, which is the exact failure being locked out. The
// executor's only channel is this prompt string; the test asserts that string.

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"
	"testing"

	"github.com/oopslink/agent-center/internal/agentruntime/executor"
	"github.com/oopslink/agent-center/internal/agentruntime/modelrouter"
)

// fakeIssueResolver serves canned issue bodies and records what was asked for.
type fakeIssueResolver struct {
	mu     sync.Mutex
	docs   map[string]*IssueDoc // keyed by ref (lowercased)
	err    error                // when set, every resolve fails with it
	asked  []string
	asked2 []string // projectIDs, positionally aligned with asked
}

func (f *fakeIssueResolver) ResolveIssue(_ context.Context, ref, projectID string) (*IssueDoc, error) {
	f.mu.Lock()
	f.asked = append(f.asked, ref)
	f.asked2 = append(f.asked2, projectID)
	f.mu.Unlock()
	if f.err != nil {
		return nil, f.err
	}
	d, ok := f.docs[strings.ToLower(ref)]
	if !ok {
		return nil, fmt.Errorf("issue %s not found", ref)
	}
	return d, nil
}

// newInlineEngine builds an Engine wired with an issue resolver + a log capture,
// reusing the real pool/routing fixture so HandleWork exercises the genuine chain.
func newInlineEngine(t *testing.T, res IssueResolver, runner RunnerCmdBuilder) (*Engine, *[]string) {
	t.Helper()
	eng, _, _ := newTestEngine(t, 3, modelrouter.Config{DefaultExecutorModel: "m"}, runner)
	var mu sync.Mutex
	logs := &[]string{}
	eng.issues = res
	eng.log = func(format string, args ...any) {
		mu.Lock()
		defer mu.Unlock()
		*logs = append(*logs, fmt.Sprintf(format, args...))
	}
	return eng, logs
}

// THE lock for ①: a task whose brief only POINTS at an issue ("见 issue-…") must
// reach the executor as a prompt that CONTAINS that issue's text. The executor is a
// frozen one-shot process with no center access, so anything not in this string is
// unreachable to it.
func TestHandleWork_InlinesIssueBodyIntoSpawnedPrompt(t *testing.T) {
	const body = "ROOT CAUSE: the git clone sits on the control path and blocks the fork."
	res := &fakeIssueResolver{docs: map[string]*IssueDoc{
		"issue-13e7bfe8": {Ref: "issue-13e7bfe8", Title: "clone blocks the control path", Body: body},
	}}
	fr := &fakeRunner{}
	eng, _ := newInlineEngine(t, res, fr)

	got, err := eng.HandleWork(context.Background(), WorkItem{
		TaskID: "task-1", TaskRef: "task-1", ProjectID: "project-1",
		Goal: executor.Goal{
			Title:       "fix the fork stall",
			Description: "见 issue-13e7bfe8 完整证据与根因。",
		},
	})
	if err != nil {
		t.Fatalf("HandleWork: %v", err)
	}
	defer reap(t, got.Handle)

	// The whole point: the BODY is in the prompt the executor was spawned with.
	if !strings.Contains(fr.lastPrompt, body) {
		t.Fatalf("spawned prompt does NOT contain the issue body — the reference is still dead.\nprompt:\n%s", fr.lastPrompt)
	}
	if !strings.Contains(fr.lastPrompt, "clone blocks the control path") {
		t.Errorf("spawned prompt missing the issue title:\n%s", fr.lastPrompt)
	}
	// The original brief must survive alongside the expansion.
	if !strings.Contains(fr.lastPrompt, "fix the fork stall") {
		t.Errorf("spawned prompt lost the goal title:\n%s", fr.lastPrompt)
	}
	if len(res.asked) != 1 || res.asked[0] != "issue-13e7bfe8" {
		t.Errorf("resolver asked = %v, want [issue-13e7bfe8]", res.asked)
	}
	if res.asked2[0] != "project-1" {
		t.Errorf("resolver got projectID %q, want project-1", res.asked2[0])
	}
}

// An I<n> org ref is the other form an author writes; it must expand too, scoped by
// the task's project.
func TestHandleWork_InlinesOrgRef(t *testing.T) {
	const body = "progress.jsonl 是采样流，不能用来下否定断言。"
	res := &fakeIssueResolver{docs: map[string]*IssueDoc{
		"i109": {Ref: "I109", Title: "executor 信息流", Body: body},
	}}
	fr := &fakeRunner{}
	eng, _ := newInlineEngine(t, res, fr)

	got, err := eng.HandleWork(context.Background(), WorkItem{
		TaskID: "task-2", TaskRef: "task-2", ProjectID: "project-1",
		Goal: executor.Goal{Title: "[I109·dev2] 治死引用"},
	})
	if err != nil {
		t.Fatalf("HandleWork: %v", err)
	}
	defer reap(t, got.Handle)
	if !strings.Contains(fr.lastPrompt, body) {
		t.Fatalf("org-ref I109 not expanded into the prompt:\n%s", fr.lastPrompt)
	}
}

// A resolve failure must be LOUD on both channels — a log line for the operator, and
// an explicit marker in the prompt so the executor is not left following a pointer
// to evidence it cannot fetch. Silently shipping the dead reference is the bug.
func TestInlineIssueRefs_ResolveFailureIsLoud(t *testing.T) {
	res := &fakeIssueResolver{err: errors.New("boom")}
	var logs []string
	logf := func(f string, a ...any) { logs = append(logs, fmt.Sprintf(f, a...)) }

	out := inlineIssueRefs(context.Background(), res, WorkItem{
		TaskRef: "task-9",
		Goal:    executor.Goal{Title: "t", Description: "见 issue-deadbeef"},
	}, logf)

	if !strings.Contains(out, "UNAVAILABLE") || !strings.Contains(out, "issue-deadbeef") {
		t.Errorf("prompt lacks a visible unavailable marker: %q", out)
	}
	if !hasLog(logs, "resolve ref issue-deadbeef FAILED") {
		t.Errorf("no fail-loud log for the resolve failure: %v", logs)
	}
}

// No resolver wired ⇒ the refs stay dead. That is a degraded fork and must be
// logged; a zero-log skip is the failure mode this whole change exists to kill.
func TestInlineIssueRefs_NoResolverIsLogged(t *testing.T) {
	var logs []string
	logf := func(f string, a ...any) { logs = append(logs, fmt.Sprintf(f, a...)) }

	out := inlineIssueRefs(context.Background(), nil, WorkItem{
		Goal: executor.Goal{Title: "t", Description: "见 issue-deadbeef"},
	}, logf)

	if out != "" {
		t.Errorf("expected no section without a resolver, got %q", out)
	}
	if !hasLog(logs, "no resolver wired") {
		t.Errorf("a dead reference was skipped with NO log: %v", logs)
	}
}

// Truncation must be MARKED in the prompt (not just the log): a reader who cannot
// tell the text was cut will read an absence as evidence.
func TestInlineIssueRefs_TruncationIsMarked(t *testing.T) {
	// A filler rune that cannot appear in the section's own prose, so the count below
	// measures the BODY only (an ASCII letter would also match the boilerplate).
	const filler = "ᚠ"
	huge := strings.Repeat(filler, maxInlinedIssueBody+500)
	res := &fakeIssueResolver{docs: map[string]*IssueDoc{
		"issue-deadbeef": {Ref: "issue-deadbeef", Title: "big", Body: huge},
	}}
	var logs []string
	logf := func(f string, a ...any) { logs = append(logs, fmt.Sprintf(f, a...)) }

	out := inlineIssueRefs(context.Background(), res, WorkItem{
		Goal: executor.Goal{Title: "t", Description: "见 issue-deadbeef"},
	}, logf)

	if !strings.Contains(out, "已截断") {
		t.Errorf("truncated body carries no marker in the prompt: %q", out[:min(400, len(out))])
	}
	if n := strings.Count(out, filler); n != maxInlinedIssueBody {
		t.Errorf("body = %d runes, want exactly the per-issue bound %d", n, maxInlinedIssueBody)
	}
	if !hasLog(logs, "TRUNCATED") {
		t.Errorf("truncation not logged: %v", logs)
	}
}

// Over-cap refs are dropped LOUDLY — logged and marked, never quietly.
func TestInlineIssueRefs_RefCapIsLoud(t *testing.T) {
	docs := map[string]*IssueDoc{}
	var refs []string
	for i := 0; i < maxInlinedIssues+2; i++ {
		ref := fmt.Sprintf("issue-0000000%d", i)
		docs[ref] = &IssueDoc{Ref: ref, Title: "t", Body: "b"}
		refs = append(refs, ref)
	}
	res := &fakeIssueResolver{docs: docs}
	var logs []string
	logf := func(f string, a ...any) { logs = append(logs, fmt.Sprintf(f, a...)) }

	inlineIssueRefs(context.Background(), res, WorkItem{
		Goal: executor.Goal{Title: "t", Description: strings.Join(refs, " ")},
	}, logf)

	if len(res.asked) != maxInlinedIssues {
		t.Errorf("expanded %d refs, want the cap %d", len(res.asked), maxInlinedIssues)
	}
	if !hasLog(logs, "ref cap") {
		t.Errorf("over-cap refs dropped with no log: %v", logs)
	}
}

// A resolver that returns (nil, nil) is a contract violation, not a "no issue" —
// it must be loud rather than expanding to a silently empty section.
func TestInlineIssueRefs_NilDocIsLoud(t *testing.T) {
	res := &nilDocResolver{}
	var logs []string
	logf := func(f string, a ...any) { logs = append(logs, fmt.Sprintf(f, a...)) }

	out := inlineIssueRefs(context.Background(), res, WorkItem{
		Goal: executor.Goal{Title: "t", Description: "见 issue-deadbeef"},
	}, logf)

	if !strings.Contains(out, "UNAVAILABLE") {
		t.Errorf("nil doc produced no visible marker: %q", out)
	}
	if !hasLog(logs, "returned no document") {
		t.Errorf("nil doc not logged: %v", logs)
	}
}

// The TOTAL budget is a second, independent bound (a handful of large issues can
// each pass the per-issue cap yet together swamp the brief). Exhausting it must also
// be loud and marked.
func TestInlineIssueRefs_TotalBudgetIsLoud(t *testing.T) {
	// Each issue is at the per-issue cap, so the total budget runs out before the cap
	// on ref COUNT does.
	docs := map[string]*IssueDoc{}
	var refs []string
	for i := 0; i < maxInlinedIssues; i++ {
		ref := fmt.Sprintf("issue-0000000%d", i)
		docs[ref] = &IssueDoc{Ref: ref, Title: "t", Body: strings.Repeat("ᚠ", maxInlinedIssueBody)}
		refs = append(refs, ref)
	}
	res := &fakeIssueResolver{docs: docs}
	var logs []string
	logf := func(f string, a ...any) { logs = append(logs, fmt.Sprintf(f, a...)) }

	out := inlineIssueRefs(context.Background(), res, WorkItem{
		Goal: executor.Goal{Title: "t", Description: strings.Join(refs, " ")},
	}, logf)

	if n := strings.Count(out, "ᚠ"); n > maxInlinedTotal {
		t.Errorf("inlined %d runes, want ≤ the total bound %d", n, maxInlinedTotal)
	}
	if !hasLog(logs, "budget") {
		t.Errorf("budget exhaustion not logged: %v", logs)
	}
	if !strings.Contains(out, "NOT INLINED") {
		t.Errorf("budget-dropped ref carries no marker: %q", out)
	}
}

type nilDocResolver struct{}

func (nilDocResolver) ResolveIssue(context.Context, string, string) (*IssueDoc, error) {
	return nil, nil
}

// A brief citing no issue must produce a byte-for-byte unchanged prompt.
func TestInlineIssueRefs_NoRefsNoSection(t *testing.T) {
	res := &fakeIssueResolver{}
	out := inlineIssueRefs(context.Background(), res, WorkItem{
		Goal: executor.Goal{Title: "plain task", Description: "no refs here"},
	}, nil)
	if out != "" {
		t.Errorf("expected empty section, got %q", out)
	}
	if len(res.asked) != 0 {
		t.Errorf("resolver called for a ref-free brief: %v", res.asked)
	}
}

func TestScanIssueRefs(t *testing.T) {
	cases := []struct {
		name, text string
		want       []string
	}{
		{"canonical", "见 issue-13e7bfe8 完整证据", []string{"issue-13e7bfe8"}},
		{"org ref", "[I109·dev2] executor 信息流", []string{"I109"}},
		{"both, first-appearance order", "I109 then issue-13e7bfe8", []string{"I109", "issue-13e7bfe8"}},
		{"dedupe case-insensitively", "issue-AB12CD34 issue-ab12cd34", []string{"issue-AB12CD34"}},
		{"i18n is not an org ref", "the i18n and I18n layer", nil},
		{"bare I is not an org ref", "I think so", nil},
		{"non-hex is not an issue id", "issue-zzzzzzzz", nil},
		{"none", "nothing to see", nil},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := scanIssueRefs(c.text)
			if strings.Join(got, ",") != strings.Join(c.want, ",") {
				t.Fatalf("scanIssueRefs(%q) = %v, want %v", c.text, got, c.want)
			}
		})
	}
}

func hasLog(logs []string, sub string) bool {
	for _, l := range logs {
		if strings.Contains(l, sub) {
			return true
		}
	}
	return false
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}
