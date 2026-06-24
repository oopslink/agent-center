package query

import (
	"fmt"
	"time"

	"github.com/oopslink/agent-center/internal/conversation"
	"github.com/oopslink/agent-center/internal/observability"
	pm "github.com/oopslink/agent-center/internal/projectmanager"
	"github.com/oopslink/agent-center/internal/workforce"
)

// projectTaskExecutionSummary is the compact execution row used by
// inspectWorker's active_tasks list (v2.14.0 F7 / issue I14: worker→agents→
// tasks). Summary form (id/agent/task/status); Full detail is via
// `inspect execution <task_id>`.
func projectTaskExecutionSummary(t *pm.Task) map[string]any {
	return map[string]any{
		"agent_id": agentMemberIDFromAssignee(t.Assignee()),
		"task_id":  string(t.ID()),
		"status":   taskExecStatus(t),
	}
}

// projectTaskRow builds the short row used by `query tasks` and the project
// inspect tasks-sublist. v2.7 #107 Phase-2 (proj-B): reads pm.Task. priority /
// conversation_id dropped (no pm.Task field); assignee added (pm has it).
func projectTaskRow(t *pm.Task) map[string]any {
	return map[string]any{
		"id":         string(t.ID()),
		"project_id": string(t.ProjectID()),
		"title":      t.Title(),
		"status":     string(t.Status()),
		"assignee":   stringOrNil(string(t.Assignee())),
		"created_at": t.CreatedAt().UTC().Format(time.RFC3339Nano),
		"version":    t.Version(),
	}
}

func projectTaskList(items []*pm.Task) []any {
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

// projectIssueRow formats a pm issue list row (v2.7 #125: repointed off the
// retired discussion model). opened_by←created_by, opened_at←created_at;
// conversation_id dropped (pm.Issue has no conversation link).
func projectIssueRow(i *pm.Issue) map[string]any {
	return map[string]any{
		"id":         string(i.ID()),
		"project_id": string(i.ProjectID()),
		"title":      i.Title(),
		"status":     string(i.Status()),
		"opened_by":  string(i.CreatedBy()),
		"opened_at":  i.CreatedAt().UTC().Format(time.RFC3339Nano),
		"version":    i.Version(),
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

func projectEventFull(e *observability.Event) map[string]any {
	out := map[string]any{
		"id":          string(e.ID()),
		"occurred_at": e.OccurredAt().UTC().Format(time.RFC3339Nano),
		"seq":         e.Seq(),
		"event_type":  string(e.Type()),
		"actor":       string(e.Actor()),
		"refs":        e.Refs(),
		"payload":     e.Payload(),
		"created_at":  e.CreatedAt().UTC().Format(time.RFC3339Nano),
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
