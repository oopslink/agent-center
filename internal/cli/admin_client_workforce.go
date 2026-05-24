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

// ProposalDTO mirrors admin api proposalMap.
type ProposalDTO struct {
	ProposalID         string `json:"proposal_id"`
	WorkerID           string `json:"worker_id"`
	Status             string `json:"status"`
	CandidatePath      string `json:"candidate_path"`
	SuggestedProjectID string `json:"suggested_project_id"`
	SuggestedKind      string `json:"suggested_kind"`
	Version            int    `json:"version"`
}

// ProjectDTO mirrors admin api projectMap.
type ProjectDTO struct {
	ID              string `json:"id"`
	Name            string `json:"name"`
	Kind            string `json:"kind"`
	DefaultAgentCLI string `json:"default_agent_cli"`
	Description     string `json:"description"`
	Version         int    `json:"version"`
	CreatedAt       string `json:"created_at"`
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

// ProposalProposeRequest mirrors the api proposeReq.
type ProposalProposeRequest struct {
	WorkerID           string `json:"worker_id"`
	CandidatePath      string `json:"candidate_path"`
	SuggestedProjectID string `json:"suggested_project_id"`
	SuggestedKind      string `json:"suggested_kind"`
}

// ProposalProposeResponse mirrors the projection emitted on success.
type ProposalProposeResponse struct {
	ProposalID    string `json:"proposal_id"`
	EventID       string `json:"event_id"`
	AlreadyExists bool   `json:"already_exists"`
}

// ProposalAcceptRequest mirrors the api proposalAcceptReq.
type ProposalAcceptRequest struct {
	ProposalID          string `json:"proposal_id"`
	OverrideProjectID   string `json:"override_project_id"`
	OverrideKind        string `json:"override_kind"`
	OverrideProjectName string `json:"override_project_name"`
}

// ProposalAcceptResponse mirrors the api success projection.
type ProposalAcceptResponse struct {
	ProposalID     string   `json:"proposal_id"`
	MappingID      string   `json:"mapping_id"`
	ProjectID      string   `json:"project_id"`
	ProjectCreated bool     `json:"project_created"`
	EventIDs       []string `json:"event_ids"`
}

// ProposalIgnoreRequest mirrors api proposalIgnoreReq.
type ProposalIgnoreRequest struct {
	ProposalID string `json:"proposal_id"`
}

// EventIDResponse is the generic single-event-id success shape used by
// many admin write endpoints (`{"event_id": "..."}`).
type EventIDResponse struct {
	EventID string `json:"event_id"`
}

// ProjectAddRequest mirrors api projectAddReq.
type ProjectAddRequest struct {
	ID              string `json:"id"`
	Name            string `json:"name"`
	Kind            string `json:"kind"`
	DefaultAgentCLI string `json:"default_agent_cli"`
	Description     string `json:"description"`
}

// ProjectMutateResponse is the response shape for add/update (both return
// the project + an event id).
type ProjectMutateResponse struct {
	Project  ProjectDTO `json:"project"`
	EventID  string     `json:"event_id"`
}

// ProjectUpdateRequest mirrors api projectUpdateReq.
type ProjectUpdateRequest struct {
	ID              string  `json:"id"`
	Version         int     `json:"version"`
	Name            *string `json:"name"`
	Kind            *string `json:"kind"`
	DefaultAgentCLI *string `json:"default_agent_cli"`
	Description     *string `json:"description"`
}

// ProjectRemoveRequest mirrors api projectRemoveReq.
type ProjectRemoveRequest struct {
	ID string `json:"id"`
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
// Proposals — Propose / Accept / Ignore / Unignore + reads
// =============================================================================

// ProposalPropose POSTs /admin/workforce/proposal/propose.
func (c *Client) ProposalPropose(ctx context.Context, req ProposalProposeRequest) (ProposalProposeResponse, error) {
	var res ProposalProposeResponse
	err := c.postJSON(ctx, "/admin/workforce/proposal/propose", req, &res)
	return res, err
}

// ProposalAccept POSTs /admin/workforce/proposal/accept.
func (c *Client) ProposalAccept(ctx context.Context, req ProposalAcceptRequest) (ProposalAcceptResponse, error) {
	var res ProposalAcceptResponse
	err := c.postJSON(ctx, "/admin/workforce/proposal/accept", req, &res)
	return res, err
}

// ProposalIgnore POSTs /admin/workforce/proposal/ignore.
func (c *Client) ProposalIgnore(ctx context.Context, req ProposalIgnoreRequest) (EventIDResponse, error) {
	var res EventIDResponse
	err := c.postJSON(ctx, "/admin/workforce/proposal/ignore", req, &res)
	return res, err
}

// ProposalUnignore POSTs /admin/workforce/proposal/unignore.
func (c *Client) ProposalUnignore(ctx context.Context, req ProposalIgnoreRequest) (EventIDResponse, error) {
	var res EventIDResponse
	err := c.postJSON(ctx, "/admin/workforce/proposal/unignore", req, &res)
	return res, err
}

// ProposalFindByID GETs /admin/workforce/proposal/find-by-id?id=…
func (c *Client) ProposalFindByID(ctx context.Context, id string) (ProposalDTO, error) {
	var out ProposalDTO
	err := c.getJSON(ctx, "/admin/workforce/proposal/find-by-id"+buildQuery("id", id), &out)
	return out, err
}

// ProposalFindByWorkerID GETs /admin/workforce/proposal/find-by-worker-id?worker_id=…&status=…
func (c *Client) ProposalFindByWorkerID(ctx context.Context, workerID, status string) ([]ProposalDTO, error) {
	var out []ProposalDTO
	err := c.getJSON(ctx, "/admin/workforce/proposal/find-by-worker-id"+
		buildQuery("worker_id", workerID, "status", status), &out)
	return out, err
}

// ProposalFindPending GETs /admin/workforce/proposal/find-pending.
func (c *Client) ProposalFindPending(ctx context.Context) ([]ProposalDTO, error) {
	var out []ProposalDTO
	err := c.getJSON(ctx, "/admin/workforce/proposal/find-pending", &out)
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
// Project — Add / Remove / Update + reads
// =============================================================================

// ProjectAdd POSTs /admin/workforce/project/add.
func (c *Client) ProjectAdd(ctx context.Context, req ProjectAddRequest) (ProjectMutateResponse, error) {
	var res ProjectMutateResponse
	err := c.postJSON(ctx, "/admin/workforce/project/add", req, &res)
	return res, err
}

// ProjectRemove POSTs /admin/workforce/project/remove.
func (c *Client) ProjectRemove(ctx context.Context, req ProjectRemoveRequest) (EventIDResponse, error) {
	var res EventIDResponse
	err := c.postJSON(ctx, "/admin/workforce/project/remove", req, &res)
	return res, err
}

// ProjectUpdate POSTs /admin/workforce/project/update.
func (c *Client) ProjectUpdate(ctx context.Context, req ProjectUpdateRequest) (ProjectMutateResponse, error) {
	var res ProjectMutateResponse
	err := c.postJSON(ctx, "/admin/workforce/project/update", req, &res)
	return res, err
}

// ProjectFindAll GETs /admin/workforce/project/find-all?kind=…
func (c *Client) ProjectFindAll(ctx context.Context, kind string) ([]ProjectDTO, error) {
	var out []ProjectDTO
	err := c.getJSON(ctx, "/admin/workforce/project/find-all"+buildQuery("kind", kind), &out)
	return out, err
}

// ProjectFindByID GETs /admin/workforce/project/find-by-id?id=…
func (c *Client) ProjectFindByID(ctx context.Context, id string) (ProjectDTO, error) {
	var out ProjectDTO
	err := c.getJSON(ctx, "/admin/workforce/project/find-by-id"+buildQuery("id", id), &out)
	return out, err
}
