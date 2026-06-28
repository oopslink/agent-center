package sqlite

import (
	"context"
	"testing"

	"github.com/oopslink/agent-center/internal/agent"
)

// TestOrgDirectory_ConcurrencyCapOfAgent covers the v2.18.0 W4c center cap adapter
// over a REAL agent repo: an enabled profile (max_concurrent>0 + ≥1 allowed model)
// reports its EffectiveMaxConcurrentTasks; a default profile reports 1; an unknown
// agent fails SAFE to 1 (single-active) with no error.
func TestOrgDirectory_ConcurrencyCapOfAgent(t *testing.T) {
	r := newDB(t)
	ctx := context.Background()

	save := func(id agent.AgentID, p agent.Profile) {
		t.Helper()
		p.Name = "coder"
		a, err := agent.NewAgent(agent.NewAgentInput{
			ID: id, OrganizationID: "org", WorkerID: "W1", Profile: p,
			CreatedBy: "user:a", CreatedAt: t0,
		})
		if err != nil {
			t.Fatal(err)
		}
		if err := r.Save(ctx, a); err != nil {
			t.Fatal(err)
		}
	}
	save("enabled", agent.Profile{MaxConcurrentTasks: 3, AllowedModels: []string{"m"}})
	save("defaultNoModels", agent.Profile{MaxConcurrentTasks: 3}) // column-default 3 but no models → disabled → 1
	save("explicitOne", agent.Profile{MaxConcurrentTasks: 1, AllowedModels: []string{"m"}})

	dir := agent.NewOrgDirectory(r)
	cases := []struct {
		id   string
		want int
	}{
		{"enabled", 3},
		{"defaultNoModels", 1},
		{"explicitOne", 1},
		{"unknown-agent", 1}, // unresolvable → fail-safe single-active
	}
	for _, c := range cases {
		t.Run(c.id, func(t *testing.T) {
			got, err := dir.ConcurrencyCapOfAgent(ctx, c.id)
			if err != nil {
				t.Fatalf("ConcurrencyCapOfAgent(%s): %v", c.id, err)
			}
			if got != c.want {
				t.Fatalf("ConcurrencyCapOfAgent(%s) = %d, want %d", c.id, got, c.want)
			}
		})
	}
}
