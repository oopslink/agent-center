// Package cli — admin_client_workforce.go: Client methods for the
// Workforce BC admin surface (workers, proposals, agent instances,
// projects). Mirrors internal/admin/api/workforce.go 1:1.
//
// Naming: methods on Client are named <Resource><Verb> to match the
// admin route segments (e.g. `WorkerEnroll` for
// `POST /admin/workforce/worker/enroll`). Read methods return typed DTO
// structs whose JSON tags match the JSON keys emitted by the admin
// endpoint's projection helpers (`workerMap`, `proposalMap`, etc.).
package cli

import "context"

// =============================================================================
// DTOs — JSON shape returned by admin/api/workforce.go projection helpers.
// Field names match the JSON keys in workerMap / proposalMap / projectMap /
// agentInstanceMap exactly.
// =============================================================================

// WorkerDTO mirrors admin api workerMap.
type WorkerDTO struct {
	WorkerID        string   `json:"worker_id"`
	Status          string   `json:"status"`
	Capabilities    []string `json:"capabilities"`
	Version         int      `json:"version"`
	EnrolledAt      string   `json:"enrolled_at"`
	LastHeartbeatAt string   `json:"last_heartbeat_at,omitempty"`
}

// ProjectDTO mirrors admin api projectMap. v2.7 #131 PR-3: repointed to the
// pm.Project model — tags dropped (pm.Project has none), organization_id
// surfaced.
type ProjectDTO struct {
	ID             string `json:"id"`
	Name           string `json:"name"`
	Description    string `json:"description"`
	OrganizationID string `json:"organization_id"`
	Version        int    `json:"version"`
	CreatedAt      string `json:"created_at"`
}

// AgentInstanceDTO mirrors admin api agentInstanceMap.
type AgentInstanceDTO struct {
	ID            string `json:"id"`
	Name          string `json:"name"`
	State         string `json:"state"`
	AgentCLI      string `json:"agent_cli"`
	WorkerID      string `json:"worker_id"`
	IsBuiltin     bool   `json:"is_builtin"`
	MaxConcurrent int    `json:"max_concurrent"`
	Config        string `json:"config"`
	Version       int    `json:"version"`
	IdentityID    string `json:"identity_id"`
}

// =============================================================================
// Request payloads — match admin/api request structs (kept local so the
// Client doesn't take a compile dependency on the api package).
// =============================================================================

// WorkerEnrollRequest is the POST body for /admin/workforce/worker/enroll.
type WorkerEnrollRequest struct {
	WorkerID     string   `json:"worker_id"`
	Capabilities []string `json:"capabilities"`
}

// WorkerEnrollResponse is the success body.
type WorkerEnrollResponse struct {
	WorkerID string `json:"worker_id"`
	EventID  string `json:"event_id"`
	Version  int    `json:"version"`
}

// EventIDResponse is the generic single-event-id success shape used by
// many admin write endpoints (`{"event_id": "..."}`).
type EventIDResponse struct {
	EventID string `json:"event_id"`
}

// AgentCreateRequest mirrors api agentCreateReq.
type AgentCreateRequest struct {
	Name          string `json:"name"`
	AgentCLI      string `json:"agent_cli"`
	WorkerID      string `json:"worker_id"`
	Config        string `json:"config"`
	MaxConcurrent *int   `json:"max_concurrent"`
}

// AgentCreateResponse mirrors api success projection.
type AgentCreateResponse struct {
	ID         string `json:"id"`
	IdentityID string `json:"identity_id"`
	EventID    string `json:"event_id"`
}

// AgentArchiveRequest mirrors api agentArchiveReq.
type AgentArchiveRequest struct {
	ID      string `json:"id"`
	Reason  string `json:"reason"`
	Message string `json:"message"`
	Version int    `json:"version"`
}

// =============================================================================
// Worker — Enroll + read repo methods
// =============================================================================

// WorkerEnroll POSTs /admin/workforce/worker/enroll.
func (c *Client) WorkerEnroll(ctx context.Context, req WorkerEnrollRequest) (WorkerEnrollResponse, error) {
	var res WorkerEnrollResponse
	err := c.postJSON(ctx, "/admin/workforce/worker/enroll", req, &res)
	return res, err
}

// WorkerFindAll GETs /admin/workforce/worker/find-all.
func (c *Client) WorkerFindAll(ctx context.Context) ([]WorkerDTO, error) {
	var out []WorkerDTO
	if err := c.getJSON(ctx, "/admin/workforce/worker/find-all", &out); err != nil {
		return nil, err
	}
	return out, nil
}

// WorkerFindByID GETs /admin/workforce/worker/find-by-id?id=…
func (c *Client) WorkerFindByID(ctx context.Context, id string) (WorkerDTO, error) {
	var out WorkerDTO
	err := c.getJSON(ctx, "/admin/workforce/worker/find-by-id"+buildQuery("id", id), &out)
	return out, err
}

// WorkerFindByStatus GETs /admin/workforce/worker/find-by-status?status=…
func (c *Client) WorkerFindByStatus(ctx context.Context, status string) ([]WorkerDTO, error) {
	var out []WorkerDTO
	err := c.getJSON(ctx, "/admin/workforce/worker/find-by-status"+buildQuery("status", status), &out)
	return out, err
}

// =============================================================================
// AgentInstance — Create / Archive + reads
// =============================================================================

// AgentInstanceCreate POSTs /admin/workforce/agent-instance/create.
func (c *Client) AgentInstanceCreate(ctx context.Context, req AgentCreateRequest) (AgentCreateResponse, error) {
	var res AgentCreateResponse
	err := c.postJSON(ctx, "/admin/workforce/agent-instance/create", req, &res)
	return res, err
}

// AgentInstanceArchive POSTs /admin/workforce/agent-instance/archive.
func (c *Client) AgentInstanceArchive(ctx context.Context, req AgentArchiveRequest) (EventIDResponse, error) {
	var res EventIDResponse
	err := c.postJSON(ctx, "/admin/workforce/agent-instance/archive", req, &res)
	return res, err
}

// AgentInstanceFindAll GETs /admin/workforce/agent-instance/find-all?state=…&worker_id=…
func (c *Client) AgentInstanceFindAll(ctx context.Context, state, workerID string) ([]AgentInstanceDTO, error) {
	var out []AgentInstanceDTO
	err := c.getJSON(ctx, "/admin/workforce/agent-instance/find-all"+
		buildQuery("state", state, "worker_id", workerID), &out)
	return out, err
}

// AgentInstanceFindByID GETs /admin/workforce/agent-instance/find-by-id?id=…
func (c *Client) AgentInstanceFindByID(ctx context.Context, id string) (AgentInstanceDTO, error) {
	var out AgentInstanceDTO
	err := c.getJSON(ctx, "/admin/workforce/agent-instance/find-by-id"+buildQuery("id", id), &out)
	return out, err
}

// AgentInstanceFindByName GETs /admin/workforce/agent-instance/find-by-name?name=…
func (c *Client) AgentInstanceFindByName(ctx context.Context, name string) (AgentInstanceDTO, error) {
	var out AgentInstanceDTO
	err := c.getJSON(ctx, "/admin/workforce/agent-instance/find-by-name"+buildQuery("name", name), &out)
	return out, err
}

// =============================================================================
// Project — reads (find-all / find-by-id). The write routes (add / remove /
// update) were retired with the workforce project-write surface; only the
// repointed (PR-3) read methods remain.
// =============================================================================

// ProjectFindAll GETs /admin/workforce/project/find-all. v2.5.5 dropped
// the by-kind filter; the param is kept on the signature for callers
// that haven't been updated yet, but is no longer transmitted.
func (c *Client) ProjectFindAll(ctx context.Context, _ string) ([]ProjectDTO, error) {
	var out []ProjectDTO
	err := c.getJSON(ctx, "/admin/workforce/project/find-all", &out)
	return out, err
}

// ProjectFindByID GETs /admin/workforce/project/find-by-id?id=…
func (c *Client) ProjectFindByID(ctx context.Context, id string) (ProjectDTO, error) {
	var out ProjectDTO
	err := c.getJSON(ctx, "/admin/workforce/project/find-by-id"+buildQuery("id", id), &out)
	return out, err
}
