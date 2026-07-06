package agentruntime

// installed_skills_report.go — the agent-runtime's OBSERVED skill collection + report
// (issue-4a45e9cc). On boot and on the heartbeat Tick the runtime walks the four
// claude-code skill layers (built-in / plugin / user / project), fingerprints the
// resolved set, and — only when the fingerprint changed — POSTs it to the center over
// the EXISTING per-agent agent-tools channel (report_installed_skills). The center
// replaces the agent's agent_installed_skills rows with the report.

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"time"

	"github.com/oopslink/agent-center/internal/workerdaemon/skillscan"
)

// skillReportScanInterval rate-limits the disk scan on the (possibly 4s-fast) Tick
// cadence — the skill tree changes rarely, so a per-Tick walk is wasteful.
const skillReportScanInterval = 30 * time.Second

// kickInstalledSkillsReport runs a FORCED report in the background (boot path). It is
// off the caller's goroutine so a slow scan / center never blocks session start.
func (r *LocalRuntime) kickInstalledSkillsReport() {
	r.bg.Add(1)
	go func() {
		defer r.bg.Done()
		r.reportInstalledSkillsIfChanged(context.Background(), r.now(), true)
	}()
}

// reportInstalledSkillsIfChanged scans the agent's skill layers and POSTs the set to
// the center when its fingerprint differs from the last report. force=true bypasses
// the scan rate-limit (boot). Best-effort throughout: a resolve/scan/POST failure is
// logged and dropped — the next Tick retries. No center client ⇒ no-op.
func (r *LocalRuntime) reportInstalledSkillsIfChanged(ctx context.Context, now time.Time, force bool) {
	tc := r.toolCaller()
	if tc == nil {
		return // graceful degrade: no channel wired (e.g. single-claude test path)
	}
	// Rate-limit the disk scan (unless forced).
	r.mu.Lock()
	if !force && !r.lastSkillScanAt.IsZero() && now.Sub(r.lastSkillScanAt) < skillReportScanInterval {
		r.mu.Unlock()
		return
	}
	r.lastSkillScanAt = now
	prevFP := r.lastSkillFingerprint
	r.mu.Unlock()

	home, tasksDir, _, err := r.agentPaths(r.cfg.AgentID)
	if err != nil {
		r.log("agent=%s installed-skills: resolve paths: %v", r.cfg.AgentID, err)
		return
	}
	roots := r.resolveSkillRoots(home, tasksDir)
	skills := skillscan.Scan(roots)
	fp := skillscan.Fingerprint(skills)
	if fp == prevFP {
		return // unchanged since last report — nothing to send
	}

	body := map[string]any{
		"agent_id":     r.cfg.AgentID,
		"collected_at": now.UTC().Format(time.RFC3339Nano),
		"skills":       skillReportEntries(skills),
	}
	if err := tc.CallAgentTool(ctx, "report_installed_skills", body, nil); err != nil {
		r.log("agent=%s installed-skills report: %v", r.cfg.AgentID, err)
		return // keep prevFP so the next Tick retries
	}
	r.mu.Lock()
	r.lastSkillFingerprint = fp
	r.mu.Unlock()
	r.log("agent=%s installed-skills reported (%d skills)", r.cfg.AgentID, len(skills))
}

// skillReportEntries flattens the scanned set into the report payload shape.
func skillReportEntries(skills []skillscan.Skill) []map[string]any {
	out := make([]map[string]any, 0, len(skills))
	for _, s := range skills {
		out = append(out, map[string]any{
			"layer":       string(s.Layer),
			"name":        s.Name,
			"description": s.Description,
			"shadowed":    s.Shadowed,
		})
	}
	return out
}

// resolveSkillRoots returns the four-layer scan roots. It prefers the injected resolver
// (cfg.SkillLayerRoots, used by tests) and otherwise derives the real dirs:
//   - project: the agent's mounted skills (home/skills, the --skill-path target) plus
//     its project cwd (tasks/.claude/skills).
//   - user:    $HOME/.claude/skills (honors $CLAUDE_CONFIG_DIR when set).
//   - plugin:  each $HOME/.claude/plugins/<plugin>/skills.
//   - built-in: $CLAUDE_BUILTIN_SKILLS_DIR when set (the CLI install path is not
//     discoverable from here, so it is opt-in via env rather than fabricated).
func (r *LocalRuntime) resolveSkillRoots(home, tasksDir string) skillscan.LayerRoots {
	if r.cfg.SkillLayerRoots != nil {
		return r.cfg.SkillLayerRoots(home, tasksDir)
	}
	return defaultSkillLayerRoots(home, tasksDir)
}

// defaultSkillLayerRoots derives the layer roots from the agent home + the user's
// claude config dir. Non-existent roots are silently skipped by the scanner, so a host
// missing any layer simply contributes nothing from it.
func defaultSkillLayerRoots(home, tasksDir string) skillscan.LayerRoots {
	claudeConfig := os.Getenv("CLAUDE_CONFIG_DIR")
	if claudeConfig == "" {
		if h, err := os.UserHomeDir(); err == nil {
			claudeConfig = filepath.Join(h, ".claude")
		}
	}
	roots := skillscan.LayerRoots{
		Project: []string{
			filepath.Join(home, "skills"),
			filepath.Join(tasksDir, ".claude", "skills"),
		},
	}
	if b := os.Getenv("CLAUDE_BUILTIN_SKILLS_DIR"); b != "" {
		roots.Builtin = []string{b}
	}
	if claudeConfig != "" {
		roots.User = []string{filepath.Join(claudeConfig, "skills")}
		roots.Plugin = pluginSkillRoots(filepath.Join(claudeConfig, "plugins"))
	}
	return roots
}

// pluginSkillRoots resolves the plugin-layer skill roots from installed_plugins.json
// (F2 — "以 installed_plugins.json 为准"). The pre-fix version enumerated
// <pluginsDir>/<child>/skills over the plugins dir's IMMEDIATE children — but those
// children are cache/, marketplaces/, data/ and the JSON manifests, NOT plugin dirs,
// so every derived root (e.g. <plugins>/cache/skills) is non-existent → the plugin
// layer was ALWAYS empty and real plugin skills (superpowers-class) never surfaced.
//
// The real install layout records each installed plugin's on-disk location in
// <pluginsDir>/installed_plugins.json as
//
//	{ "plugins": { "<name>@<marketplace>": [ { "installPath": "/abs/.../<plugin>/<ver>", ... } ] } }
//
// and that plugin's skills live at <installPath>/skills/<skill>/SKILL.md. We parse the
// manifest defensively (unknown fields ignored; a missing/garbage manifest yields nil →
// no plugin layer, never an error) and return each installPath/skills root, de-duped.
func pluginSkillRoots(pluginsDir string) []string {
	b, err := os.ReadFile(filepath.Join(pluginsDir, "installed_plugins.json"))
	if err != nil {
		return nil // no manifest → no plugin layer
	}
	var manifest struct {
		Plugins map[string][]struct {
			InstallPath string `json:"installPath"`
		} `json:"plugins"`
	}
	if err := json.Unmarshal(b, &manifest); err != nil {
		return nil // unparseable manifest → degrade to no plugin layer
	}
	var out []string
	seen := map[string]bool{}
	for _, installs := range manifest.Plugins {
		for _, inst := range installs {
			if inst.InstallPath == "" {
				continue
			}
			root := filepath.Join(inst.InstallPath, "skills")
			if seen[root] {
				continue
			}
			seen[root] = true
			out = append(out, root)
		}
	}
	return out
}
