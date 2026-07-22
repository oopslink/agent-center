package api

import (
	"testing"
	"time"

	"github.com/oopslink/agent-center/internal/team"
)

// TestTeamMonogram locks the ≤2-char UPPERCASE glyph derivation (plan-32dd9107, the
// hard "no emoji" constraint): multi-word → first letter of the first two words;
// single word → its first two letters; non-letters skipped; nothing derivable → "".
func TestTeamMonogram(t *testing.T) {
	cases := map[string]string{
		"Agent Core":          "AC",
		"Dev Experience Team": "DE", // first two words only, capped at 2
		"Growth":              "GR", // single word → first two letters
		"agent core":          "AC", // uppercased
		"  spaced  name ":     "SN",
		"X":                   "X",  // single-letter single word
		"platform-core":       "PC", // hyphen splits into two words
		"":                    "",
		"123 456":             "",   // no letters
		"7go":                 "GO", // leading digits skipped, single word "go"
	}
	for in, want := range cases {
		if got := teamMonogram(in); got != want {
			t.Errorf("teamMonogram(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestSplitTags(t *testing.T) {
	cases := []struct {
		in   string
		want []string
	}{
		{"go, backend", []string{"go", "backend"}},
		{"go backend review", []string{"go", "backend", "review"}},
		{"  ", nil},
		{"", nil},
		{"single", []string{"single"}},
	}
	for _, c := range cases {
		got := splitTags(c.in)
		if len(got) != len(c.want) {
			t.Fatalf("splitTags(%q) = %v, want %v", c.in, got, c.want)
		}
		for i := range got {
			if got[i] != c.want[i] {
				t.Fatalf("splitTags(%q)[%d] = %q, want %q", c.in, i, got[i], c.want[i])
			}
		}
	}
}

func mkTeam(t *testing.T, name string, roles []team.RoleConfig) *team.Team {
	t.Helper()
	tm, err := team.NewTeam(team.NewTeamInput{
		ID: "team-1", OrgID: "org-1", Name: name, Roles: roles,
		CreatedAt: time.Unix(1_700_000_000, 0).UTC(),
	})
	if err != nil {
		t.Fatalf("NewTeam: %v", err)
	}
	return tm
}

// TestTeamViewMap locks status derivation (members>0→active), the per-role member
// count, glyph, and the counts — the shape-critical TeamView fields.
func TestTeamViewMap(t *testing.T) {
	roles := []team.RoleConfig{
		{Role: "impl", CLI: "claude-code", Model: "claude-opus-4-8", CapabilityTags: []string{"go"}, MaxConcurrency: 3},
		{Role: "reviewer", CLI: "claude-code", Model: "claude-sonnet-5", CapabilityTags: nil, MaxConcurrency: 1},
	}
	tm := mkTeam(t, "Agent Core", roles)
	members := []*team.TeamMember{
		{TeamID: "team-1", Ref: "agent:a1", Kind: team.MemberKindAgent, Role: "impl"},
		{TeamID: "team-1", Ref: "agent:a2", Kind: team.MemberKindAgent, Role: "impl"},
		{TeamID: "team-1", Ref: "user:u1", Kind: team.MemberKindHuman, Role: "reviewer"},
	}

	v := teamViewMap(tm, members, 2)
	if v["glyph"] != "AC" {
		t.Errorf("glyph = %v, want AC", v["glyph"])
	}
	if v["status"] != "active" {
		t.Errorf("status = %v, want active (members>0)", v["status"])
	}
	if v["members_count"] != 3 || v["projects_count"] != 2 {
		t.Errorf("counts = %v/%v, want 3/2", v["members_count"], v["projects_count"])
	}
	rs := v["roles"].([]map[string]any)
	if rs[0]["count"] != 2 {
		t.Errorf("impl per-role count = %v, want 2", rs[0]["count"])
	}
	if rs[1]["count"] != 1 {
		t.Errorf("reviewer per-role count = %v, want 1", rs[1]["count"])
	}
	// capability_tags is never null.
	if tags, ok := rs[1]["capability_tags"].([]string); !ok || tags == nil {
		t.Errorf("reviewer capability_tags = %v, want non-nil []", rs[1]["capability_tags"])
	}

	// empty team → draft, per-role count 0.
	empty := teamViewMap(tm, nil, 0)
	if empty["status"] != "draft" {
		t.Errorf("empty team status = %v, want draft", empty["status"])
	}
	if empty["roles"].([]map[string]any)[0]["count"] != 0 {
		t.Errorf("no-member role count = %v, want 0", empty["roles"].([]map[string]any)[0]["count"])
	}

	multiRole := teamViewMap(tm, []*team.TeamMember{
		{TeamID: "team-1", Ref: "agent:a1", Kind: team.MemberKindAgent, Role: "impl"},
		{TeamID: "team-1", Ref: "agent:a1", Kind: team.MemberKindAgent, Role: "reviewer"},
	}, 0)
	if multiRole["members_count"] != 1 {
		t.Errorf("multi-role members_count = %v, want 1 unique member", multiRole["members_count"])
	}
}

// TestMemberViewMap locks exclusive=false and role-config-sourced tags/cli/model/concurrency.
func TestMemberViewMap(t *testing.T) {
	roleByName := map[string]team.RoleConfig{
		"impl": {Role: "impl", CLI: "claude-code", Model: "claude-opus-4-8", CapabilityTags: []string{"go", "backend"}, MaxConcurrency: 3},
	}
	m := &team.TeamMember{TeamID: "team-1", Ref: "agent:a1", Kind: team.MemberKindAgent, Role: "impl"}
	v := memberViewMap(m, roleByName, "Alice")
	if v["exclusive"] != false {
		t.Errorf("exclusive = %v, want false (Phase-1)", v["exclusive"])
	}
	if v["name"] != "Alice" || v["kind"] != "agent" || v["cli"] != "claude-code" || v["model"] != "claude-opus-4-8" {
		t.Errorf("member view = %v", v)
	}
	if v["concurrency"] != "3" {
		t.Errorf("concurrency = %v, want \"3\"", v["concurrency"])
	}
	if tags := v["tags"].([]string); len(tags) != 2 || tags[0] != "go" {
		t.Errorf("tags = %v, want [go backend]", v["tags"])
	}

	// unknown role (no config) → empty cli/model, non-nil tags.
	v2 := memberViewMap(&team.TeamMember{TeamID: "team-1", Ref: "user:u1", Kind: team.MemberKindHuman, Role: "ghost"}, roleByName, "")
	if v2["cli"] != "" || v2["concurrency"] != "0" {
		t.Errorf("unknown-role view = %v", v2)
	}
	if tags, ok := v2["tags"].([]string); !ok || tags == nil {
		t.Errorf("unknown-role tags = %v, want non-nil []", v2["tags"])
	}
}

func TestMemberViewsAggregatesRoles(t *testing.T) {
	roleByName := map[string]team.RoleConfig{
		"impl":   {Role: "impl", CLI: "claude-code", Model: "sonnet-5", CapabilityTags: []string{"go"}, MaxConcurrency: 3},
		"review": {Role: "review", CLI: "codex", Model: "gpt-5", CapabilityTags: []string{"go", "audit"}, MaxConcurrency: 1},
	}
	members := []*team.TeamMember{
		{TeamID: "team-1", Ref: "agent:a1", Kind: team.MemberKindAgent, Role: "impl"},
		{TeamID: "team-1", Ref: "agent:a1", Kind: team.MemberKindAgent, Role: "review"},
	}
	views := memberViews(members, roleByName, func(ref team.MemberRef) string { return "Ada" })
	if len(views) != 1 {
		t.Fatalf("views len = %d want 1: %v", len(views), views)
	}
	v := views[0]
	roles := v["roles"].([]string)
	if len(roles) != 2 || roles[0] != "impl" || roles[1] != "review" {
		t.Fatalf("roles = %v want [impl review]", roles)
	}
	if v["role"] != "impl, review" || v["cli"] != "mixed" || v["model"] != "mixed" || v["concurrency"] != "3 / 1" {
		t.Fatalf("aggregated view = %v", v)
	}
	tags := v["tags"].([]string)
	if len(tags) != 2 || tags[0] != "go" || tags[1] != "audit" {
		t.Fatalf("tags = %v want [go audit]", tags)
	}
}

// TestProjectLinkMap locks the relation label + glyph derivation.
func TestProjectLinkMap(t *testing.T) {
	tp := &team.TeamProject{TeamID: "team-1", ProjectID: "project-9", CreatedAt: time.Now()}
	v := projectLinkMap(tp, "Growth Experiments", "primary")
	if v["project_id"] != "project-9" || v["name"] != "Growth Experiments" {
		t.Errorf("link = %v", v)
	}
	if v["glyph"] != "GE" {
		t.Errorf("glyph = %v, want GE", v["glyph"])
	}
	if v["relation"] != "primary" {
		t.Errorf("relation = %v, want primary", v["relation"])
	}
	if v["repo"] != "" {
		t.Errorf("repo = %v, want \"\" (Phase-1 placeholder)", v["repo"])
	}
}
