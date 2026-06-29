package service

import (
	"context"
	"strings"

	agentpkg "github.com/oopslink/agent-center/internal/agent"
	pm "github.com/oopslink/agent-center/internal/projectmanager"
	"github.com/oopslink/agent-center/internal/workforce"
)

// auto_assign_directory.go (v2.18.3 BE-2, issue-577a7b0e) — the candidate-listing
// port for the auto-assign reconciler. The reconciler must, for a claimable pool
// task, find an eligible idle agent; that decision needs each org agent's
// online-ness, opt-out flag, capability labels, and run-slot cap. Those live in the
// Agent + Environment(Worker) BCs, so the pm BC reads them through this narrow,
// nil-safe port (like AgentDirectory) — it never imports the agent/worker packages
// itself for the matching loop.

// AutoAssignCandidate is one org agent's auto-assign snapshot (BE-2): the inputs the
// reconciler's eligibility gate + tie-break consume, resolved adapter-side. It is
// deliberately STRING/primitive-typed (no agent/worker domain types leak across the
// port). AgentRef is the assignee identity ref form ("agent:<identity-member-id>",
// the same ref AssignTask/ClaimPoolTask carry) so the reconciler can assign + count
// run slots + check project membership with it directly. CapabilityTags is canonical
// (trimmed/lowercased/deduped) so the strict capability gate is a pure subset test.
type AutoAssignCandidate struct {
	AgentRef       pm.IdentityRef
	Online         bool
	AutoAssignable bool
	CapabilityTags []string
	ConcurrencyCap int
}

// AutoAssignDirectory lists an organization's agents with the per-agent snapshot the
// BE-2 reconciler needs to match claimable pool tasks to eligible owners. It is an
// OPTIONAL, nil-safe dependency of the pm Service: nil ⇒ the reconciler has no
// candidate source and auto-assign is a no-op (pre-BE-2 behaviour). Implemented at
// composition by AgentAutoAssignDirectory over the agent + worker repos.
type AutoAssignDirectory interface {
	// ListAutoAssignCandidates returns every agent in orgID with its auto-assign
	// snapshot. The reconciler does the project-membership ∩ capability ∩ slot
	// filtering itself; this port only supplies the agent-side facts.
	ListAutoAssignCandidates(ctx context.Context, orgID string) ([]AutoAssignCandidate, error)
}

// AgentAutoAssignDirectory adapts the Agent repository + the workforce Worker
// repository to the AutoAssignDirectory port (BE-2). Online-ness is the SAME signal
// the agent service's Availability derivation uses (the bound Worker.Status() ==
// WorkerOnline), so center reconciler and dashboard agree on "online". CapabilityTags
// is re-canonicalised through pm.NormalizeCapabilities so the strict gate is
// case-insensitive even though an agent's stored capability_tags keep their original
// case (an agent labelled "Go" matches a task requiring "go"). The cap is the same
// Profile.EffectiveConcurrencyCap the W4c start guard consults, so selection and the
// later start_task cap-check use one source.
type AgentAutoAssignDirectory struct {
	agents  agentpkg.Repository
	workers workforce.WorkerRepository
}

// NewAgentAutoAssignDirectory wires the adapter. Both repos are required; a nil one
// makes ListAutoAssignCandidates return an error (the composition root always wires
// both, and the port itself is treated as optional/nil at the Service level).
func NewAgentAutoAssignDirectory(agents agentpkg.Repository, workers workforce.WorkerRepository) *AgentAutoAssignDirectory {
	return &AgentAutoAssignDirectory{agents: agents, workers: workers}
}

// ListAutoAssignCandidates implements AutoAssignDirectory. It lists the org's agents
// and projects each to an AutoAssignCandidate. The worker-online lookup is memoised
// per worker id within the call so co-located agents (same worker) cost one read.
func (d *AgentAutoAssignDirectory) ListAutoAssignCandidates(ctx context.Context, orgID string) ([]AutoAssignCandidate, error) {
	agents, err := d.agents.ListByOrg(ctx, orgID)
	if err != nil {
		return nil, err
	}
	onlineOf := make(map[string]bool, len(agents))
	out := make([]AutoAssignCandidate, 0, len(agents))
	for _, a := range agents {
		// The assignee identity ref the assign/claim path carries is
		// "agent:<identity-member-id>" (agentActor), falling back to the entity id when
		// no identity member is bound. Mirror that exactly so the reconciler's assign +
		// membership + run-slot reads all key on the same ref.
		memberID := strings.TrimSpace(a.IdentityMemberID())
		if memberID == "" {
			memberID = string(a.ID())
		}
		ref := pm.IdentityRef("agent:" + memberID)

		wid := a.WorkerID()
		online, ok := onlineOf[wid]
		if !ok {
			online = d.workerOnline(ctx, wid)
			onlineOf[wid] = online
		}

		out = append(out, AutoAssignCandidate{
			AgentRef:       ref,
			Online:         online,
			AutoAssignable: a.Profile().AutoAssignable,
			CapabilityTags: pm.NormalizeCapabilities(a.CapabilityTags()),
			ConcurrencyCap: a.Profile().EffectiveConcurrencyCap(),
		})
	}
	return out, nil
}

// workerOnline reports whether the bound worker is online — the same predicate the
// agent service uses (Worker.Status() == WorkerOnline). An empty worker id or any
// lookup error reads as OFFLINE (fail-closed: a directory hiccup can only ever make
// an agent INELIGIBLE, never wrongly auto-assign work to a presumed-online agent).
func (d *AgentAutoAssignDirectory) workerOnline(ctx context.Context, workerID string) bool {
	if strings.TrimSpace(workerID) == "" {
		return false
	}
	w, err := d.workers.FindByID(ctx, workforce.WorkerID(workerID))
	if err != nil || w == nil {
		return false
	}
	return w.Status() == workforce.WorkerOnline
}
