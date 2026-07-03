package orchestrator

import (
	"strings"
	"testing"

	"github.com/oopslink/agent-center/internal/workerdaemon/executor"
)

// fakeProbe reports a fixed workspace presence, so the ladder is tested without a
// real filesystem.
type fakeProbe struct{ present bool }

func (f fakeProbe) Exists(string) bool { return f.present }

func newPlanner(t *testing.T, present bool) *RecoveryPlanner {
	t.Helper()
	layout, err := executor.NewLayout(t.TempDir())
	if err != nil {
		t.Fatalf("NewLayout: %v", err)
	}
	p, err := NewRecoveryPlanner(layout, fakeProbe{present: present})
	if err != nil {
		t.Fatalf("NewRecoveryPlanner: %v", err)
	}
	return p
}

// claudeArgv is a persisted fresh claude launch argv carrying --session-id <sid>.
func claudeArgv(t *testing.T, sid string) []string {
	t.Helper()
	argv, err := NewClaudeRunnerBuilder("claude").Build("m", "goal", sid)
	if err != nil {
		t.Fatalf("Build: %v", err)
	}
	return argv
}

func TestRecoveryPlanner_Tier1_Resume(t *testing.T) {
	// session-id present + workspace present + resumable argv → tier 1 --resume.
	sid := "abcabcab-1111-5222-8333-444444444444"
	p := newPlanner(t, true)
	rec := &executor.Record{ExecutorID: "e1", SessionID: sid, RunnerCmd: claudeArgv(t, sid), RepoKey: "rk", SourcePath: "/src"}

	plan := p.Plan("e1", rec)
	if plan.Action != RecoverResume {
		t.Fatalf("action = %v, want RecoverResume", plan.Action)
	}
	if plan.Workspace == "" {
		t.Error("tier 1 must carry the existing workspace")
	}
	joined := strings.Join(plan.RunnerCmd, " ")
	if !strings.Contains(joined, "--resume "+sid) || strings.Contains(joined, "--session-id") {
		t.Errorf("tier 1 argv should be --resume form: %q", joined)
	}
	if plan.RepoKey != "rk" || plan.SourcePath != "/src" || plan.SessionID != sid {
		t.Errorf("tier 1 dropped durable handles: %+v", plan)
	}
}

func TestRecoveryPlanner_Tier2_Rerun_NoSession(t *testing.T) {
	// workspace present but no session id → tier 2 rerun of the persisted argv.
	p := newPlanner(t, true)
	rec := &executor.Record{ExecutorID: "e1", RunnerCmd: []string{"claude", "-p", "goal"}}

	plan := p.Plan("e1", rec)
	if plan.Action != RecoverRerun {
		t.Fatalf("action = %v, want RecoverRerun", plan.Action)
	}
	if plan.Workspace == "" {
		t.Error("tier 2 must carry the existing workspace")
	}
	if len(plan.RunnerCmd) != 3 || plan.RunnerCmd[0] != "claude" {
		t.Errorf("tier 2 must rerun the persisted argv verbatim: %q", plan.RunnerCmd)
	}
}

func TestRecoveryPlanner_Tier2_Rerun_SessionButNotResumable(t *testing.T) {
	// A session id is recorded but the argv has no --session-id to rewrite (e.g. a
	// codex executor) → still tier 2, not a bogus resume.
	p := newPlanner(t, true)
	rec := &executor.Record{ExecutorID: "e1", SessionID: "x", RunnerCmd: []string{"codex", "exec", "prompt"}}

	plan := p.Plan("e1", rec)
	if plan.Action != RecoverRerun {
		t.Fatalf("action = %v, want RecoverRerun (argv not resumable)", plan.Action)
	}
}

func TestRecoveryPlanner_Tier3_Fresh_WorkspaceGone(t *testing.T) {
	// workspace gone → tier 3 fresh, regardless of session id.
	sid := "abcabcab-1111-5222-8333-444444444444"
	p := newPlanner(t, false)
	rec := &executor.Record{ExecutorID: "e1", SessionID: sid, RunnerCmd: claudeArgv(t, sid), RepoKey: "rk", SourcePath: "/src"}

	plan := p.Plan("e1", rec)
	if plan.Action != RecoverFresh {
		t.Fatalf("action = %v, want RecoverFresh", plan.Action)
	}
	if plan.Workspace != "" || plan.RunnerCmd != nil {
		t.Errorf("tier 3 must not carry a workspace/argv: %+v", plan)
	}
	// Provisioning handles still ride along so the driver can re-provision.
	if plan.RepoKey != "rk" || plan.SourcePath != "/src" {
		t.Errorf("tier 3 should still carry re-provisioning handles: %+v", plan)
	}
}

func TestRecoveryPlanner_Tier3_Fresh_NoRecord(t *testing.T) {
	// A never-tracked executor (nil record) has no durable state → tier 3.
	p := newPlanner(t, true) // even if some dir exists, nil record → fresh
	plan := p.Plan("e1", nil)
	if plan.Action != RecoverFresh {
		t.Fatalf("action = %v, want RecoverFresh for nil record", plan.Action)
	}
}

func TestNewRecoveryPlanner_NilLayout(t *testing.T) {
	if _, err := NewRecoveryPlanner(nil, nil); err == nil {
		t.Error("nil layout must error")
	}
}
