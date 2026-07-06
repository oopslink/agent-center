package agentruntime

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/oopslink/agent-center/internal/agentruntime/skillscan"
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

// writeSkillMDAt creates <dir>/SKILL.md with name+description frontmatter.
func writeSkillMDAt(t *testing.T, dir, name, desc string) {
	t.Helper()
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	fm := "---\nname: " + name + "\ndescription: " + desc + "\n---\n\nbody\n"
	if err := os.WriteFile(filepath.Join(dir, "SKILL.md"), []byte(fm), 0o644); err != nil {
		t.Fatal(err)
	}
}

// TestPluginSkillRoots_FromInstalledManifest is the F2 regression (tester1: plugin
// layer恒空). The plugin roots MUST come from installed_plugins.json installPaths, NOT
// from the plugins dir's immediate children (cache/marketplaces/data + JSON manifests)
// whose <child>/skills never exist. A missing/garbage manifest degrades to no plugin
// layer (never an error).
func TestPluginSkillRoots_FromInstalledManifest(t *testing.T) {
	tmp := t.TempDir()
	pluginsDir := filepath.Join(tmp, "plugins")
	// decoy immediate children the pre-fix code would have wrongly turned into roots.
	for _, d := range []string{"cache", "marketplaces", "data"} {
		if err := os.MkdirAll(filepath.Join(pluginsDir, d), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	instA := filepath.Join(pluginsDir, "cache", "mp", "superpowers", "1.0")
	instB := filepath.Join(pluginsDir, "cache", "mp", "frontend-design", "unknown")
	for _, p := range []string{instA, instB} {
		if err := os.MkdirAll(filepath.Join(p, "skills"), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	manifest := map[string]any{
		"version": 2,
		"plugins": map[string]any{
			// two installs of the same plugin (dup installPath → de-duped to one root) +
			// a blank-installPath install (skipped) exercise both filter branches.
			"superpowers@mp": []any{
				map[string]any{"installPath": instA, "scope": "user"},
				map[string]any{"installPath": instA, "scope": "project"},
				map[string]any{"installPath": ""},
			},
			"frontend-design@mp": []any{map[string]any{"installPath": instB}},
		},
	}
	b, _ := json.Marshal(manifest)
	if err := os.WriteFile(filepath.Join(pluginsDir, "installed_plugins.json"), b, 0o644); err != nil {
		t.Fatal(err)
	}

	got := pluginSkillRoots(pluginsDir)
	want := map[string]bool{filepath.Join(instA, "skills"): true, filepath.Join(instB, "skills"): true}
	if len(got) != 2 {
		t.Fatalf("want 2 plugin roots from manifest, got %d: %v", len(got), got)
	}
	for _, r := range got {
		if !want[r] {
			t.Fatalf("unexpected plugin root %q — decoy child leaked or wrong path; got=%v", r, got)
		}
	}
	// missing plugins dir → nil (graceful, no plugin layer).
	if r := pluginSkillRoots(filepath.Join(tmp, "does-not-exist")); r != nil {
		t.Fatalf("missing plugins dir should yield nil, got %v", r)
	}
	// garbage manifest → nil.
	bad := filepath.Join(tmp, "bad")
	if err := os.MkdirAll(bad, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(bad, "installed_plugins.json"), []byte("{not json"), 0o644); err != nil {
		t.Fatal(err)
	}
	if r := pluginSkillRoots(bad); r != nil {
		t.Fatalf("garbage manifest should yield nil, got %v", r)
	}
}

// TestDefaultSkillLayerRoots_ResolvesPluginAndSymlinkedUserSkills is the F1+F2
// end-to-end proof through the REAL resolver: a symlinked user skill (F1) AND a plugin
// skill declared in installed_plugins.json (F2) both surface in a Scan of the resolved
// roots — i.e. an agent's real "actually usable" skills now show, per acceptance.
func TestDefaultSkillLayerRoots_ResolvesPluginAndSymlinkedUserSkills(t *testing.T) {
	tmp := t.TempDir()
	claude := filepath.Join(tmp, ".claude")

	// user layer: a SYMLINKED skill dir (F1) — the real ~/.claude/skills layout.
	store := filepath.Join(tmp, "store")
	writeSkillMDAt(t, filepath.Join(store, "slack"), "slack", "user slack skill")
	if err := os.MkdirAll(filepath.Join(claude, "skills"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(filepath.Join(store, "slack"), filepath.Join(claude, "skills", "slack")); err != nil {
		t.Fatal(err)
	}

	// plugin layer: installed_plugins.json → installPath/skills/superpowers (F2).
	inst := filepath.Join(claude, "plugins", "cache", "mp", "superpowers", "1.0")
	writeSkillMDAt(t, filepath.Join(inst, "skills", "superpowers"), "superpowers", "the superpowers plugin skill")
	manifest := map[string]any{"plugins": map[string]any{"superpowers@mp": []any{map[string]any{"installPath": inst}}}}
	b, _ := json.Marshal(manifest)
	if err := os.WriteFile(filepath.Join(claude, "plugins", "installed_plugins.json"), b, 0o644); err != nil {
		t.Fatal(err)
	}

	t.Setenv("CLAUDE_CONFIG_DIR", claude)
	roots := defaultSkillLayerRoots(filepath.Join(tmp, "home"), filepath.Join(tmp, "tasks"))
	got := skillscan.Scan(roots)

	byName := map[string]skillscan.Skill{}
	for _, s := range got {
		byName[s.Name] = s
	}
	if s, ok := byName["slack"]; !ok || s.Layer != skillscan.LayerUser {
		t.Fatalf("symlinked user skill 'slack' not resolved (F1); got=%+v", got)
	}
	if s, ok := byName["superpowers"]; !ok || s.Layer != skillscan.LayerPlugin {
		t.Fatalf("plugin skill 'superpowers' not resolved (F2); got=%+v", got)
	}
}
