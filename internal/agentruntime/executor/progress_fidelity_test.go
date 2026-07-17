package executor

// progress_fidelity_test.go — regression locks for I109 ② (progress.jsonl false
// negatives).
//
// THE INCIDENT THESE PIN. `grep -c "push" progress.jsonl` returned 0 for a run whose
// `git reflog show --date=iso refs/remotes/origin/<branch>` recorded `update by push`
// at 23:49:24 — 22 seconds BEFORE the executor wrote state=done, i.e. while it was
// still running, i.e. the executor pushed it itself and its self-report was true. On
// the strength of that 0 the executor was nearly filed as "谎报交付", and the bad
// evidence had already been forwarded to a third party. Two independent causes:
//
//   1. SAMPLING — 123 lines over 47 minutes, with a 32-second gap between the last
//      recorded Bash and state=done. Activity rode a 15s heartbeat, so a tool call
//      landing between beats left no trace. Now every tool call appends its own entry.
//   2. CLIPPING — `message` is bounded (maxDetailLen), so a long command lost its
//      tail; `git add`/`commit`/`rebase` matched only because they sat before the cut.
//      Now Tools carries the command names outside the clipped text.

import (
	"context"
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/oopslink/agent-center/internal/clock"
)

// toolLine builds a stream-json assistant line carrying one Bash tool_use.
func toolLine(cmd string) string {
	b, _ := json.Marshal(cmd)
	return asstTool("Bash", `{"command":`+string(b)+`}`)
}

// THE lock for the clipping half: a command far longer than the message bound must
// still be identifiable as a push. This is the exact shape that produced the 0.
func TestToolNames_LongCommandKeepsCommandNamesIdentifiable(t *testing.T) {
	// A realistic over-long command: a huge commit message body pushes `git push` past
	// the message clip boundary.
	long := "cd /Users/x/workspace && go build ./... && go test ./... && git add -A && " +
		"git commit -m " + strconv.Quote(strings.Repeat("detail ", 500)) + " && git push origin HEAD"

	detail, tools, isTool := streamLineActivity([]byte(toolLine(long)))
	if !isTool {
		t.Fatal("a Bash tool_use was not recognized as a tool call")
	}
	// Precondition: the rendered message really IS clipped — otherwise this test would
	// pass for the wrong reason and stop guarding anything.
	if !strings.HasSuffix(detail, "…") {
		t.Fatalf("precondition failed: message was not clipped (len=%d), test no longer covers the bug", len([]rune(detail)))
	}
	if strings.Contains(detail, "push") {
		t.Fatalf("precondition failed: `push` survived in the clipped message, test no longer covers the bug")
	}
	// The actual guarantee: the command names survive independently of the clip.
	for _, want := range []string{"Bash", "git", "push", "go", "test", "build"} {
		if !hasName(tools, want) {
			t.Errorf("tools = %v, missing %q — a grep for it would be a FALSE NEGATIVE", tools, want)
		}
	}
}

// The && chain must be split per segment: naming only the leading program ("cd")
// would hide everything that actually ran.
func TestCommandNames_SplitsChainsAndPipes(t *testing.T) {
	cases := []struct {
		name, cmd string
		want      []string
	}{
		{"and chain", "cd /x && git push origin HEAD", []string{"cd", "git", "push"}},
		{"pipe", "cat log | grep push", []string{"cat", "grep", "push"}},
		{"semicolons", "go build ./... ; echo $?", []string{"go", "build", "echo"}},
		{"or chain", "test -f x || rm -rf y", []string{"test", "rm"}},
		{"newlines", "git add -A\ngit commit -m ok\ngit push", []string{"git", "add", "commit", "push"}},
		{"env prefix stripped", "TOKEN=abc go test ./...", []string{"go", "test"}},
		{"abs path greps as basename", "/usr/local/bin/git push", []string{"git", "push"}},
		{"flags skipped to reach the verb", "git -C /x push origin", []string{"git", "push"}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := commandNames(c.cmd)
			for _, w := range c.want {
				if !hasName(got, w) {
					t.Errorf("commandNames(%q) = %v, missing %q", c.cmd, got, w)
				}
			}
		})
	}
}

// The name set is bounded — a pathological command must not grow the record without
// limit (the record stays cheap; that is what lets it be per-tool-call).
func TestToolNames_Bounded(t *testing.T) {
	var segs []string
	for i := 0; i < 200; i++ {
		segs = append(segs, fmt.Sprintf("prog%d run", i))
	}
	names := toolNames("Bash", json.RawMessage(`{"command":`+jsonQuote(strings.Join(segs, " && "))+`}`))
	if len(names) > maxToolNames {
		t.Errorf("tools len = %d, want ≤ %d", len(names), maxToolNames)
	}
	// An over-long token is not a plausible command name and is dropped.
	if n := cleanName(strings.Repeat("a", maxToolNameRune+1)); n != "" {
		t.Errorf("over-long token kept: %q", n)
	}
}

// A non-Bash tool contributes just its own name (there is no command to split).
func TestToolNames_NonBash(t *testing.T) {
	names := toolNames("Read", json.RawMessage(`{"file_path":"/x/y.go"}`))
	if len(names) != 1 || names[0] != "Read" {
		t.Errorf("tools = %v, want [Read]", names)
	}
}

// THE lock for the sampling half, end-to-end through RunExecutor: several tool calls
// arriving inside ONE heartbeat window must ALL appear in progress.jsonl. Under the
// old throttle these collapsed into a single sampled line and the rest vanished —
// which is how a real push came to leave no trace.
func TestRunExecutor_EveryToolCallIsRecorded(t *testing.T) {
	cmds := []string{
		"go build ./...",
		"go test ./...",
		"git add -A",
		"git commit -m wip",
		"git push origin HEAD", // the one that went missing in the incident
	}
	var lines []string
	for _, c := range cmds {
		lines = append(lines, toolLine(c))
	}

	fx, id, root := newProgressFixture(t)
	// A frozen clock: NO time passes, so the 15s throttle would suppress every one of
	// these if appends were still coupled to the status write.
	err := RunExecutor(context.Background(), RunConfig{
		AgentRoot:  root,
		ExecutorID: id,
		Runner:     &scriptedRunner{lines: lines},
		Clock:      clock.NewFakeClock(time.Unix(1700000000, 0)),
	})
	if err != nil {
		t.Fatalf("RunExecutor: %v", err)
	}

	entries, err := fx.ReadProgress(id)
	if err != nil {
		t.Fatalf("ReadProgress: %v", err)
	}
	// Every tool call has its own entry.
	var toolEntries int
	for _, e := range entries {
		if e.Phase == phaseTool {
			toolEntries++
		}
	}
	if toolEntries != len(cmds) {
		t.Errorf("recorded %d tool entries, want %d (one per tool call) — entries: %+v", toolEntries, len(cmds), entries)
	}
	// And the incident's exact query now answers truthfully.
	if !progressMentions(entries, "push") {
		t.Error("`push` absent from progress.jsonl though a push tool call ran — THE false negative is back")
	}
	for _, want := range []string{"add", "commit", "test", "build"} {
		if !progressMentions(entries, want) {
			t.Errorf("%q absent from progress.jsonl though it ran", want)
		}
	}
}

// progressMentions answers the question an operator actually asks ("did X run?") the
// way they ask it — against the record's greppable surface.
func progressMentions(entries []ProgressEntry, name string) bool {
	for _, e := range entries {
		if hasName(e.Tools, name) {
			return true
		}
	}
	return false
}

func hasName(names []string, want string) bool {
	for _, n := range names {
		if strings.EqualFold(n, want) {
			return true
		}
	}
	return false
}

// scriptedRunner feeds canned stream-json lines through the REAL CommandRunner
// onLine path, so the test exercises the production progress plumbing rather than a
// reimplementation of it.
type scriptedRunner struct{ lines []string }

func (s *scriptedRunner) Run(ctx context.Context, rc RunContext) (RunResult, error) {
	cr := &CommandRunner{
		cmd: []string{"fake"},
		run: func(_ context.Context, _ string, _ []string, onLine func(string)) (string, error) {
			for _, l := range s.lines {
				onLine(l)
			}
			return "done", nil
		},
	}
	return cr.Run(ctx, rc)
}

func newProgressFixture(t *testing.T) (fx *FileExchange, id, root string) {
	t.Helper()
	root = t.TempDir()
	layout, err := NewLayout(root)
	if err != nil {
		t.Fatalf("NewLayout: %v", err)
	}
	clk := clock.NewFakeClock(time.Unix(1700000000, 0)) // frozen: the throttle stays shut
	fx, err = NewFileExchange(layout, clk)
	if err != nil {
		t.Fatalf("NewFileExchange: %v", err)
	}
	id = "exec-fidelity"
	if _, err := fx.Provision(id); err != nil {
		t.Fatalf("Provision: %v", err)
	}
	in := Input{ExecutorID: id, Goal: Goal{Title: "t"}, Model: "m", CreatedAt: clk.Now()}
	if err := fx.WriteInput(in); err != nil {
		t.Fatalf("WriteInput: %v", err)
	}
	return fx, id, root
}
