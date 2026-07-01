// Package cli — admin_client_workforce.go: Client methods for the
// Workforce BC admin surface (workers, proposals, projects).
// Mirrors internal/admin/api/workforce.go 1:1.
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
// Field names match the JSON keys in workerMap / proposalMap / projectMap
// exactly.
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
