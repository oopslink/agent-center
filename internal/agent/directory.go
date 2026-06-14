package agent

import (
	"context"
	"errors"
	"strings"
)

// taskRefPrefix is the pm task work-item ref scheme an AgentWorkItem stores for a
// plan/task it executes (pm://tasks/{id}). The agent BC already owns this string
// (the WorkItemProjector stamps it from pm events; agent file-tools trim it), so
// the paused-provider trims it here without importing the pm package.
const taskRefPrefix = "pm://tasks/"

// WorkItemPausedProvider adapts an agent WorkItemRepository to the ProjectManager
// Service's optional PausedTaskPort (T53): it reports which pm tasks currently have
// a PAUSED work item, so the plan read model can derive a `paused` node instead of
// mis-showing a set-aside node as `running`. Like OrgDirectory it is string-typed
// so the agent BC implements the pm port without importing pm.
type WorkItemPausedProvider struct{ repo WorkItemRepository }

// NewWorkItemPausedProvider wraps a WorkItemRepository as a paused-task resolver.
func NewWorkItemPausedProvider(repo WorkItemRepository) *WorkItemPausedProvider {
	return &WorkItemPausedProvider{repo: repo}
}

// PausedTasks returns the subset of taskIDs whose work item is currently paused.
// It runs ONE query (ListByStatus(paused) — the global paused set is small) and
// intersects it with the requested ids, so it stays N+1-free regardless of how
// many tasks a plan has. An empty input short-circuits without a query. A paused
// status is unambiguously the CURRENT item (paused is non-terminal; a resumed or
// superseded item leaves the paused status), so presence ⇒ the task is paused now.
func (p *WorkItemPausedProvider) PausedTasks(ctx context.Context, taskIDs []string) (map[string]bool, error) {
	if len(taskIDs) == 0 {
		return map[string]bool{}, nil
	}
	items, err := p.repo.ListByStatus(ctx, WorkItemPaused)
	if err != nil {
		return nil, err
	}
	pausedBare := make(map[string]bool, len(items))
	for _, wi := range items {
		ref := wi.TaskRef()
		if id := strings.TrimPrefix(ref, taskRefPrefix); id != ref {
			pausedBare[id] = true
		}
	}
	out := make(map[string]bool, len(taskIDs))
	for _, tid := range taskIDs {
		if pausedBare[tid] {
			out[tid] = true
		}
	}
	return out, nil
}

// OrgDirectory adapts an agent Repository to the ProjectManager Service's
// optional AgentDirectory dependency (v2.7 D2 b2/d-i, #5a, ADR-0049/0052/OQ6):
// it resolves an agent's owning Organization so AssignTask can cross-org-guard
// before granting an assignee agent ProjectMember. agentID is the bare id (the
// `agent:` prefix is stripped by the pm Service before calling). An unknown
// agent surfaces ErrAgentNotFound, which the pm Service treats as a cross-org
// rejection (org cannot be verified).
type OrgDirectory struct{ repo Repository }

// NewOrgDirectory wraps an agent Repository as an org resolver.
func NewOrgDirectory(repo Repository) *OrgDirectory { return &OrgDirectory{repo: repo} }

// OrgOfAgent returns the agent's OrganizationID. agentID may be EITHER the
// execution-entity id OR the identity-member id ("agent-<ulid>", v2.7 #185 — the
// business-layer id the assign path now carries).
func (d *OrgDirectory) OrgOfAgent(ctx context.Context, agentID string) (string, error) {
	a, err := d.resolve(ctx, agentID)
	if err != nil {
		return "", err
	}
	return a.OrganizationID(), nil
}

// resolve accepts either the execution-entity id or the identity-member id
// (#185 member→entity bridge): entity-id first (cheap, no collision — member ids
// are "agent-"-prefixed, entity ids bare ULIDs), then FindByIdentityMemberID.
func (d *OrgDirectory) resolve(ctx context.Context, id string) (*Agent, error) {
	a, err := d.repo.FindByID(ctx, AgentID(id))
	if err == nil {
		return a, nil
	}
	if !errors.Is(err, ErrAgentNotFound) {
		return nil, err
	}
	return d.repo.FindByIdentityMemberID(ctx, id)
}
