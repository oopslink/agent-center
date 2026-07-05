package agentruntime

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/oopslink/agent-center/internal/workerdaemon/skillscan"
)

// countingCaller records every report_installed_skills call and its payload.
type countingCaller struct {
	mu    sync.Mutex
	calls int
	last  map[string]any
}

func (c *countingCaller) CallAgentTool(_ context.Context, tool string, body any, _ *json.RawMessage) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	if tool == "report_installed_skills" {
		c.calls++
		b, _ := json.Marshal(body)
		_ = json.Unmarshal(b, &c.last)
	}
	return nil
}

func (c *countingCaller) snapshot() (int, map[string]any) {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.calls, c.last
}

func writeSkillMD(t *testing.T, root, dir, name, desc string) {
	t.Helper()
	sd := filepath.Join(root, dir)
	if err := os.MkdirAll(sd, 0o755); err != nil {
		t.Fatal(err)
	}
	md := "---\nname: " + name + "\ndescription: " + desc + "\n---\n"
	if err := os.WriteFile(filepath.Join(sd, "SKILL.md"), []byte(md), 0o644); err != nil {
		t.Fatal(err)
	}
}

func newSkillReportRuntime(t *testing.T, caller ToolCaller, roots func() skillscan.LayerRoots) *LocalRuntime {
	t.Helper()
	return NewLocalRuntime(LocalRuntimeConfig{
		AgentID:       "AG1",
		WorkerID:      "W1",
		AgentHomeBase: t.TempDir(),
		ToolCaller:    func() ToolCaller { return caller },
		SkillLayerRoots: func(_, _ string) skillscan.LayerRoots {
			return roots()
		},
		Log: func(string, ...any) {},
	}, &SessionState{})
}

func TestReportInstalledSkills_FingerprintSuppression(t *testing.T) {
	userRoot := t.TempDir()
	writeSkillMD(t, userRoot, "review", "review", "code review")
	caller := &countingCaller{}
	r := newSkillReportRuntime(t, caller, func() skillscan.LayerRoots {
		return skillscan.LayerRoots{User: []string{userRoot}}
	})

	now := time.Date(2026, 7, 5, 10, 0, 0, 0, time.UTC)
	// First (forced boot) report POSTs.
	r.reportInstalledSkillsIfChanged(context.Background(), now, true)
	calls, last := caller.snapshot()
	if calls != 1 {
		t.Fatalf("boot should report once, got %d", calls)
	}
	if last["agent_id"] != "AG1" {
		t.Fatalf("agent_id wrong: %v", last["agent_id"])
	}
	skills, _ := last["skills"].([]any)
	if len(skills) != 1 {
		t.Fatalf("want 1 skill in payload, got %v", last["skills"])
	}

	// Unchanged fingerprint → no re-report (forced bypasses only the scan rate-limit,
	// not the fingerprint check).
	r.reportInstalledSkillsIfChanged(context.Background(), now.Add(time.Minute), true)
	if calls, _ := caller.snapshot(); calls != 1 {
		t.Fatalf("unchanged set must not re-report, got %d calls", calls)
	}

	// Change the skill set → re-report.
	writeSkillMD(t, userRoot, "planning", "planning", "planner")
	r.reportInstalledSkillsIfChanged(context.Background(), now.Add(2*time.Minute), true)
	if calls, _ := caller.snapshot(); calls != 2 {
		t.Fatalf("changed set must re-report, got %d calls", calls)
	}
}

func TestReportInstalledSkills_ScanRateLimit(t *testing.T) {
	userRoot := t.TempDir()
	writeSkillMD(t, userRoot, "review", "review", "d")
	caller := &countingCaller{}
	r := newSkillReportRuntime(t, caller, func() skillscan.LayerRoots {
		return skillscan.LayerRoots{User: []string{userRoot}}
	})
	now := time.Date(2026, 7, 5, 10, 0, 0, 0, time.UTC)

	// First forced report establishes lastSkillScanAt.
	r.reportInstalledSkillsIfChanged(context.Background(), now, true)
	if calls, _ := caller.snapshot(); calls != 1 {
		t.Fatalf("first report expected, got %d", calls)
	}
	// A NON-forced Tick within the scan interval, even after the set changed, is
	// rate-limited (no scan, no report).
	writeSkillMD(t, userRoot, "planning", "planning", "d")
	r.reportInstalledSkillsIfChanged(context.Background(), now.Add(5*time.Second), false)
	if calls, _ := caller.snapshot(); calls != 1 {
		t.Fatalf("within scan interval must not re-scan, got %d", calls)
	}
	// After the interval elapses, the changed set is picked up.
	r.reportInstalledSkillsIfChanged(context.Background(), now.Add(skillReportScanInterval+time.Second), false)
	if calls, _ := caller.snapshot(); calls != 2 {
		t.Fatalf("after interval the change should report, got %d", calls)
	}
}

func TestReportInstalledSkills_NilCallerNoOp(t *testing.T) {
	r := NewLocalRuntime(LocalRuntimeConfig{
		AgentID: "AG1", WorkerID: "W1", AgentHomeBase: t.TempDir(),
		ToolCaller: func() ToolCaller { return nil },
		Log:        func(string, ...any) {},
	}, &SessionState{})
	// Must not panic / must be a no-op.
	r.reportInstalledSkillsIfChanged(context.Background(), time.Now(), true)
}
