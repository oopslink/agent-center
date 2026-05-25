// Package cli — admin_client_observability.go: Client methods for the
// Observability BC admin surface (event find / query / inspect / fleet
// / stats / logs). Mirrors internal/admin/api/observability.go 1:1.
//
// Logs/Open is special: the admin endpoint streams the gzipped blob
// body as `Content-Type: application/gzip` with the blob ref in
// `X-Blob-Ref`. The Client returns the raw body bytes (gzip-compressed)
// and the ref; handlers stream those bytes to stdout the same way the
// pre-refactor LogsSvc.Open path did.
package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
)

// =============================================================================
// DTOs — JSON shape returned by admin/api/observability.go projection helpers.
// EventDTO mirrors eventMap; query/inspect/fleet/stats responses are
// returned by the corresponding domain types and the Client decodes them
// directly into their public structs in the caller's package.
// =============================================================================

// EventDTO mirrors admin api eventMap.
type EventDTO struct {
	ID            string         `json:"id"`
	EventType     string         `json:"event_type"`
	Actor         string         `json:"actor"`
	Refs          map[string]any `json:"refs"`
	Payload       map[string]any `json:"payload"`
	CorrelationID string         `json:"correlation_id"`
	DecisionID    string         `json:"decision_id"`
	OccurredAt    string         `json:"occurred_at"`
}

// QueryRequest mirrors api queryReq (POST body for /observability/query/query).
type QueryRequest struct {
	Resource    string `json:"resource"`
	Status      string `json:"status"`
	ProjectID   string `json:"project_id"`
	WorkerID    string `json:"worker_id"`
	TaskID      string `json:"task_id"`
	ExecutionID string `json:"execution_id"`
	IssueID     string `json:"issue_id"`
	Opener      string `json:"opener"`
	EventType   string `json:"event_type"`
	Limit       int    `json:"limit"`
	Cursor      string `json:"cursor"`
}

// =============================================================================
// EventRepo — FindByID / Find
// =============================================================================

// EventFindByID GETs /admin/observability/event/find-by-id?id=…
func (c *Client) EventFindByID(ctx context.Context, id string) (EventDTO, error) {
	var out EventDTO
	err := c.getJSON(ctx, "/admin/observability/event/find-by-id"+buildQuery("id", id), &out)
	return out, err
}

// EventFind GETs /admin/observability/event/find with optional filters.
// Pass empty strings / 0 to omit a filter; the helper drops them.
func (c *Client) EventFind(ctx context.Context, filter EventFindFilter) ([]EventDTO, error) {
	var out []EventDTO
	limit := ""
	if filter.Limit > 0 {
		limit = fmt.Sprintf("%d", filter.Limit)
	}
	err := c.getJSON(ctx, "/admin/observability/event/find"+
		buildQuery(
			"type", filter.EventType,
			"task_id", filter.TaskID,
			"execution_id", filter.ExecutionID,
			"issue_id", filter.IssueID,
			"conversation_id", filter.ConversationID,
			"worker_id", filter.WorkerID,
			"limit", limit,
		), &out)
	return out, err
}

// EventFindFilter is the wire-side filter for EventFind.
type EventFindFilter struct {
	EventType      string
	TaskID         string
	ExecutionID    string
	IssueID        string
	ConversationID string
	WorkerID       string
	Limit          int
}

// =============================================================================
// QuerySvc — Query / Inspect
// =============================================================================

// QueryRaw POSTs /admin/observability/query/query and decodes the
// success body into `out` (any).
//
// Handlers typically decode into a query.QueryResult; we don't take a
// compile dependency on the query package here so accept any.
func (c *Client) QueryRaw(ctx context.Context, req QueryRequest, out any) error {
	return c.postJSON(ctx, "/admin/observability/query/query", req, out)
}

// InspectRaw GETs /admin/observability/query/inspect?kind=…&id=… and
// decodes into out (typically query.InspectResult).
func (c *Client) InspectRaw(ctx context.Context, kind, id string, out any) error {
	return c.getJSON(ctx, "/admin/observability/query/inspect"+
		buildQuery("kind", kind, "id", id), out)
}

// =============================================================================
// FleetSvc — Snapshot
// =============================================================================

// FleetSnapshotRaw GETs /admin/observability/fleet/snapshot?project_id=…
// and decodes into out (typically query.FleetSnapshot).
func (c *Client) FleetSnapshotRaw(ctx context.Context, projectID string, out any) error {
	return c.getJSON(ctx, "/admin/observability/fleet/snapshot"+
		buildQuery("project_id", projectID), out)
}

// =============================================================================
// StatsSvc — Aggregate
// =============================================================================

// StatsAggregateRaw GETs /admin/observability/stats/aggregate?scope=…&since=…
// and decodes into out (typically query.StatsResult). `since` is the raw
// query parameter value (handlers pre-format duration vs RFC3339).
func (c *Client) StatsAggregateRaw(ctx context.Context, scope, since string, out any) error {
	return c.getJSON(ctx, "/admin/observability/stats/aggregate"+
		buildQuery("scope", scope, "since", since), out)
}

// =============================================================================
// LogsSvc — Open (streams gzipped blob; raw body returned to caller)
// =============================================================================

// LogsOpen GETs /admin/observability/logs/open?kind=…&id=… and returns
// the gzipped body bytes plus the blob ref (from the X-Blob-Ref
// response header). Caller is responsible for decoding / streaming.
//
// We deliberately return []byte rather than an io.ReadCloser because
// the underlying socket connection is closed when this method returns;
// streaming would require a different transport-level helper.
func (c *Client) LogsOpen(ctx context.Context, kind, id string) ([]byte, string, error) {
	if c == nil || c.httpc == nil {
		return nil, "", ErrClientNotConfigured
	}
	path := "/admin/observability/logs/open" + buildQuery("kind", kind, "id", id)
	reqURL := "http://unix" + path
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, reqURL, nil)
	if err != nil {
		return nil, "", fmt.Errorf("admin client: build GET %s: %w", path, err)
	}
	req.Header.Set("Accept", "application/gzip, application/json")
	resp, err := c.httpc.Do(req)
	if err != nil {
		if IsServerUnreachable(err) {
			return nil, "", fmt.Errorf("admin GET %s: %w (%v)", path, ErrServerUnreachable, err)
		}
		return nil, "", fmt.Errorf("admin client: do GET %s: %w", path, err)
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		ce := &ClientError{
			Method: http.MethodGet,
			Path:   path,
			Status: resp.StatusCode,
			Body:   string(body),
		}
		var env struct {
			Error   string `json:"error"`
			Message string `json:"message"`
		}
		if jerr := json.Unmarshal(body, &env); jerr == nil {
			ce.Code = env.Error
			ce.Message = env.Message
		}
		return nil, "", ce
	}
	ref := resp.Header.Get("X-Blob-Ref")
	return body, ref, nil
}

// _ keeps bytes import alive for future helpers that wrap a gzip
// io.Reader over the returned bytes.
var _ = bytes.NewReader
