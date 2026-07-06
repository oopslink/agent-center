package agentruntime

import (
	"testing"

	"github.com/oopslink/agent-center/internal/agent"
)

// routerCandidates must carry the catalog annotations (T950 ②) the center joined onto
// each ExecutorProfile through to the modelrouter.ExecutorCandidate the difficulty
// judge reads — dropping them here would leave the judge blind to tier/cost.
func TestRouterCandidates_CarriesCatalogAnnotations(t *testing.T) {
	got := routerCandidates([]agent.ExecutorProfile{{
		CLI: "claude-code", Model: "opus", DisplayName: "Opus 4.8",
		InputCost: 15, OutputCost: 75, ContextWindow: 200000, Tier: "hardest reasoning",
	}})
	if len(got) != 1 {
		t.Fatalf("got %d candidates, want 1", len(got))
	}
	c := got[0]
	if c.CLI != "claude-code" || c.Model != "opus" || c.DisplayName != "Opus 4.8" ||
		c.InputCost != 15 || c.OutputCost != 75 || c.ContextWindow != 200000 || c.Tier != "hardest reasoning" {
		t.Errorf("annotations not carried through: %+v", c)
	}

	// A plain {cli,model} profile (no catalog join) maps to a neutral candidate — the
	// OFF/unannotated path stays exactly as before.
	plain := routerCandidates([]agent.ExecutorProfile{{CLI: "claude-code", Model: "haiku"}})
	if plain[0].Tier != "" || plain[0].InputCost != 0 || plain[0].ContextWindow != 0 {
		t.Errorf("unannotated profile must map to neutral candidate: %+v", plain[0])
	}
}
