package query

import (
	"errors"
	"fmt"
	"time"

	agentpkg "github.com/oopslink/agent-center/internal/agent"
	"github.com/oopslink/agent-center/internal/conversation"
	"github.com/oopslink/agent-center/internal/discussion"
	"github.com/oopslink/agent-center/internal/observability"
	"github.com/oopslink/agent-center/internal/observability/projection"
	"github.com/oopslink/agent-center/internal/taskruntime/inputrequest"
	"github.com/oopslink/agent-center/internal/taskruntime/task"
	"github.com/oopslink/agent-center/internal/workforce"
)

// projectWorkItemSummary is the compact work-item row used by inspectWorker's
// active_work_items list (v2.7 #107 Phase-2 proj-A: worker→agents→work-items).
// Summary form (id/agent/task/status); full activity detail is via
// inspect execution <work_item_id> (the projection).
func projectWorkItemSummary(wi *agentpkg.AgentWorkItem) map[string]any {
	taskID, _ := fleetTaskIDFromRef(wi.TaskRef())
	return map[string]any{
		"work_item_id": wi.ID(),
		"agent_id":     string(wi.AgentID()),
		"task_id":      taskID,
		"status":       string(wi.Status()),
	}
}

// projectTaskRow builds the row used by `query tasks` / inspect (short form).
func projectTaskRow(t *task.Task) map[string]any {
	return map[string]any{
		"id":              string(t.ID()),
		"project_id":      t.ProjectID(),
		"title":           t.Title(),
		"status":          string(t.Status()),
		"priority":        string(t.Priority()),
		"conversation_id": stringOrNil(t.ConversationID()),
		"created_at":      t.CreatedAt().UTC().Format(time.RFC3339Nano),
		"version":         t.Version(),
	}
}

func projectTaskList(items []*task.Task) []any {
	out := make([]any, 0, len(items))
	for _, t := range items {
		out = append(out, projectTaskRow(t))
	}
	return out
}

func projectWorker(w *workforce.Worker) map[string]any {
	return map[string]any{
		"id":                string(w.ID()),
		"status":            string(w.Status()),
		"capabilities":      w.Capabilities(),
		"enrolled_at":       w.EnrolledAt().UTC().Format(time.RFC3339Nano),
		"last_heartbeat_at": fmtTimePtr(w.LastHeartbeatAt()),
		"working_seconds":   w.WorkingSeconds(),
		"version":           w.Version(),
	}
}

func projectMappingList(items []*workforce.WorkerProjectMapping) []any {
	out := make([]any, 0, len(items))
	for _, m := range items {
		out = append(out, map[string]any{
			"id":         string(m.ID()),
			"worker_id":  string(m.WorkerID()),
			"project_id": string(m.ProjectID()),
			"base_path":  m.BasePath(),
			"status":     string(m.Status()),
			"added_at":   m.AddedAt().UTC().Format(time.RFC3339Nano),
			"version":    m.Version(),
		})
	}
	return out
}

func projectProposal(p *workforce.WorkerProjectProposal) map[string]any {
	return map[string]any{
		"id":                   string(p.ID()),
		"worker_id":            string(p.WorkerID()),
		"candidate_path":       p.CandidatePath(),
		"suggested_project_id": string(p.SuggestedProjectID()),
		"status":               string(p.Status()),
		"proposed_at":          p.ProposedAt().UTC().Format(time.RFC3339Nano),
		"version":              p.Version(),
	}
}

func projectIssueRow(i *discussion.Issue) map[string]any {
	return map[string]any{
		"id":              string(i.ID()),
		"project_id":      i.ProjectID(),
		"title":           i.Title(),
		"status":          string(i.Status()),
		"opened_by":       i.OpenedByIdentityID(),
		"opened_at":       i.OpenedAt().UTC().Format(time.RFC3339Nano),
		"conversation_id": stringOrNil(string(i.ConversationID())),
		"version":         i.Version(),
	}
}

func projectMessageList(items []*conversation.Message) []any {
	out := make([]any, 0, len(items))
	for _, m := range items {
		out = append(out, map[string]any{
			"id":                 string(m.ID()),
			"conversation_id":    string(m.ConversationID()),
			"sender_identity_id": string(m.SenderIdentityID()),
			"content_kind":       string(m.ContentKind()),
			"content":            m.Content(),
			"direction":          string(m.Direction()),
			"posted_at":          m.PostedAt().UTC().Format(time.RFC3339Nano),
			"input_request_ref":  m.InputRequestRef(),
		})
	}
	return out
}

func projectInputRequest(ir *inputrequest.InputRequest) map[string]any {
	return map[string]any{
		"id":                string(ir.ID()),
		"task_execution_id": string(ir.TaskExecutionID()),
		"status":            string(ir.Status()),
		"question":          ir.Question(),
		"urgency":           string(ir.Urgency()),
		"requested_at":      ir.RequestedAt().UTC().Format(time.RFC3339Nano),
		"responded_at":      fmtTimePtr(ir.RespondedAt()),
		"version":           ir.Version(),
	}
}

func projectEventFull(e *observability.Event) map[string]any {
	out := map[string]any{
		"id":           string(e.ID()),
		"occurred_at":  e.OccurredAt().UTC().Format(time.RFC3339Nano),
		"seq":          e.Seq(),
		"event_type":   string(e.Type()),
		"actor":        string(e.Actor()),
		"refs":         e.Refs(),
		"payload":      e.Payload(),
		"created_at":   e.CreatedAt().UTC().Format(time.RFC3339Nano),
	}
	if e.CorrelationID() != "" {
		out["correlation_id"] = e.CorrelationID()
	}
	if e.DecisionID() != "" {
		out["decision_id"] = e.DecisionID()
	}
	return out
}

func projectEventSummaryList(items []*observability.Event) []any {
	out := make([]any, 0, len(items))
	for _, e := range items {
		out = append(out, map[string]any{
			"id":          string(e.ID()),
			"event_type":  string(e.Type()),
			"actor":       string(e.Actor()),
			"occurred_at": e.OccurredAt().UTC().Format(time.RFC3339Nano),
		})
	}
	return out
}

func fmtTimePtr(t *time.Time) any {
	if t == nil {
		return nil
	}
	return t.UTC().Format(time.RFC3339Nano)
}

func fmtTimeOrEmpty(t time.Time) any {
	if t.IsZero() {
		return nil
	}
	return t.UTC().Format(time.RFC3339Nano)
}

func stringOrNil(s string) any {
	if s == "" {
		return nil
	}
	return s
}

// mapNotFound translates BC-level NotFound sentinels to query.ErrInspectNotFound
// so the CLI handler can render a stable exit code (17).
func mapNotFound(err error) error {
	if err == nil {
		return nil
	}
	// known sentinels from BC repos
	if errors.Is(err, projection.ErrProjectionNotFound) {
		return fmt.Errorf("%w: %v", ErrInspectNotFound, err)
	}
	// detect "not found" via message — repos vary slightly; conservative.
	msg := err.Error()
	for _, hint := range []string{"not found", "no such", "ErrNoRows"} {
		if contains(msg, hint) {
			return fmt.Errorf("%w: %v", ErrInspectNotFound, err)
		}
	}
	return err
}

func contains(s, sub string) bool {
	return len(s) >= len(sub) && (indexOf(s, sub) >= 0)
}

func indexOf(s, sub string) int {
	n := len(sub)
	if n == 0 {
		return 0
	}
	for i := 0; i+n <= len(s); i++ {
		if s[i:i+n] == sub {
			return i
		}
	}
	return -1
}
