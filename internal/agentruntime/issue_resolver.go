package agentruntime

// issue_resolver.go — the center-backed orchestrator.IssueResolver (I109 ①).
//
// It lives HERE, not in the orchestrator package, because this is the layer that
// owns the center agent-tool transport. The orchestrator engine only knows the
// narrow port, so it keeps its no-center-dependency shape and stays unit-testable
// with a fake — the same seam discipline the rest of the fork chain uses.
//
// Two reference forms, two lookups:
//   - "issue-<8hex>" — the canonical entity id → get_issue directly.
//   - "I<n>" — the per-org display ref (v2.7.1 #245) → not a primary key, so it is
//     resolved by scanning the PROJECT's issues for a matching org_ref. Needs a
//     project id; without one the ref is UNRESOLVABLE and says so (an error the
//     caller renders into the prompt), rather than quietly expanding to nothing.

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/oopslink/agent-center/internal/agentruntime/orchestrator"
)

// issueRefListPageSize bounds the org-ref scan for an I<n> lookup. The center caps
// list_issues page size independently; a project with more issues than one page
// yields a clear not-found rather than a silent miss (see resolveOrgRef).
const issueRefListPageSize = 200

// centerIssueResolver resolves issue refs through the agent-tool transport on
// behalf of agentID.
type centerIssueResolver struct {
	r       *LocalRuntime
	agentID string
}

// ResolveIssue implements orchestrator.IssueResolver.
func (c centerIssueResolver) ResolveIssue(ctx context.Context, ref, projectID string) (*orchestrator.IssueDoc, error) {
	ref = strings.TrimSpace(ref)
	if ref == "" {
		return nil, fmt.Errorf("empty issue ref")
	}
	caller := c.r.toolCaller()
	if caller == nil {
		return nil, fmt.Errorf("no center transport")
	}
	if strings.HasPrefix(strings.ToLower(ref), "issue-") {
		return c.getIssue(ctx, ref)
	}
	return c.resolveOrgRef(ctx, ref, projectID)
}

// getIssue reads one issue by its canonical id via the get_issue agent-tool
// (project-member read scope — the forking agent is a member of its own project).
func (c centerIssueResolver) getIssue(ctx context.Context, issueID string) (*orchestrator.IssueDoc, error) {
	var raw json.RawMessage
	body := map[string]any{"agent_id": c.agentID, "issue_id": issueID}
	if err := c.r.toolCaller().CallAgentTool(ctx, "get_issue", body, &raw); err != nil {
		return nil, err
	}
	var i centerIssueDetail
	if err := json.Unmarshal(raw, &i); err != nil {
		return nil, fmt.Errorf("decode get_issue response: %w", err)
	}
	return &orchestrator.IssueDoc{Ref: issueID, Title: i.Title, Body: i.Description}, nil
}

// resolveOrgRef maps an "I<n>" display ref onto a canonical issue by scanning the
// project's issues for a matching org_ref. It is a scan because org_ref is a
// per-org display number, not an addressable key — there is no get-by-org-ref tool.
func (c centerIssueResolver) resolveOrgRef(ctx context.Context, ref, projectID string) (*orchestrator.IssueDoc, error) {
	if strings.TrimSpace(projectID) == "" {
		return nil, fmt.Errorf("org-ref %s needs a project id to resolve, task carries none", ref)
	}
	var raw json.RawMessage
	body := map[string]any{"agent_id": c.agentID, "project_id": projectID, "page_size": issueRefListPageSize}
	if err := c.r.toolCaller().CallAgentTool(ctx, "list_issues", body, &raw); err != nil {
		return nil, err
	}
	var page struct {
		Issues  []centerIssueDetail `json:"issues"`
		HasMore bool                `json:"has_more"`
	}
	if err := json.Unmarshal(raw, &page); err != nil {
		return nil, fmt.Errorf("decode list_issues response: %w", err)
	}
	for _, i := range page.Issues {
		if strings.EqualFold(strings.TrimSpace(i.OrgRef), ref) {
			return &orchestrator.IssueDoc{Ref: ref, Title: i.Title, Body: i.Description}, nil
		}
	}
	// Distinguish "scanned everything, genuinely absent" from "ran out of page": the
	// second is a bounded-search miss, and saying so keeps the reader from concluding
	// the issue does not exist.
	if page.HasMore {
		return nil, fmt.Errorf("org-ref %s not found in the first %d issues of project %s (more pages exist; scan is bounded)",
			ref, issueRefListPageSize, projectID)
	}
	return nil, fmt.Errorf("org-ref %s not found in project %s", ref, projectID)
}

// centerIssueDetail is the subset of the get_issue / list_issues projection the
// inline expansion needs (internal/admin/api agentIssueMap).
type centerIssueDetail struct {
	ID          string `json:"id"`
	Title       string `json:"title"`
	Description string `json:"description"`
	OrgRef      string `json:"org_ref"`
}
