// Package scheduler hosts the SupervisorTriggerCoalescer + Spawner +
// TimeoutHandler + CrashRecovery domain services. Per plan-6 § 3.5-3.9
// and cognition/00-overview § 3.
package scheduler

import (
	"github.com/oopslink/agent-center/internal/cognition"
	"github.com/oopslink/agent-center/internal/observability"
)

// IsWakeEvent reports whether event_type is in the v1 wake whitelist
// (cognition/00-overview § 3.2). Closed set — unknown events return false.
func IsWakeEvent(t observability.EventType) bool {
	_, ok := wakeEventRouter[t]
	return ok
}

// RouteToScope derives an InvocationScope from a wake event. Returns
// (zero, false) if the event isn't on the whitelist or its refs are
// missing/invalid (cognition/00-overview § 3.2 + 3.3).
func RouteToScope(t observability.EventType, refs observability.EventRefs) (cognition.InvocationScope, bool) {
	rule, ok := wakeEventRouter[t]
	if !ok {
		return cognition.InvocationScope{}, false
	}
	return rule(refs)
}

// AllWakeEventTypes returns the closed set in sorted order. Used by
// tests + introspection. Sorting is deterministic via the underlying map
// iteration being wrapped here.
func AllWakeEventTypes() []observability.EventType {
	out := make([]observability.EventType, 0, len(wakeEventRouter))
	for t := range wakeEventRouter {
		out = append(out, t)
	}
	sortEventTypes(out)
	return out
}

func sortEventTypes(s []observability.EventType) {
	// insertion sort — small N, stable, no extra deps.
	for i := 1; i < len(s); i++ {
		x := s[i]
		j := i
		for j > 0 && s[j-1] > x {
			s[j] = s[j-1]
			j--
		}
		s[j] = x
	}
}

// routerFn maps a refs payload to a target scope (or fails).
type routerFn func(refs observability.EventRefs) (cognition.InvocationScope, bool)

// wakeEventRouter is the closed map of wake event_type → scope deriver.
//
// Whitelist sourced from cognition/00-overview § 3.2 (v1 set):
//
// Task-scope:
//   - task.created
//   - task.dispatched / task.dispatch_failed
//   - task_execution.failed
//   - task.priority_changed
//   - task_execution.input_required → task scope (input_request refs.task_id)
//
// Issue-scope:
//   - issue.opened
//   - issue.commented
//
// Conversation-scope:
//   - conversation.opened
//   - conversation.message_added
//
// Worker-scope:
//   - worker.enrolled
//   - worker.online
//   - worker.offline
//
// Global-scope:
//   - supervisor.periodic_review_ticker
//   - input_request.escalated → global
//   - worker_project_proposal.proposed → global
var wakeEventRouter = map[observability.EventType]routerFn{
	"task.created":               routeTask,
	"task.dispatched":            routeTask,
	"task.dispatch_failed":       routeTask,
	"task.priority_changed":      routeTask,
	"task_execution.failed":      routeTask,
	"task_execution.input_required": routeTask,

	"issue.opened":    routeIssue,
	"issue.commented": routeIssue,

	"conversation.opened":       routeConversation,
	"conversation.message_added": routeConversation,

	"worker.enrolled": routeWorker,
	"worker.online":   routeWorker,
	"worker.offline":  routeWorker,

	"supervisor.periodic_review_ticker": routeGlobal,
	"input_request.escalated":           routeGlobal,
	"worker_project_proposal.proposed":  routeGlobal,
}

func routeTask(refs observability.EventRefs) (cognition.InvocationScope, bool) {
	if refs.TaskID == "" {
		return cognition.InvocationScope{}, false
	}
	s, err := cognition.NewInvocationScope(cognition.ScopeTask, refs.TaskID)
	if err != nil {
		return cognition.InvocationScope{}, false
	}
	return s, true
}

func routeIssue(refs observability.EventRefs) (cognition.InvocationScope, bool) {
	if refs.IssueID == "" {
		return cognition.InvocationScope{}, false
	}
	s, err := cognition.NewInvocationScope(cognition.ScopeIssue, refs.IssueID)
	if err != nil {
		return cognition.InvocationScope{}, false
	}
	return s, true
}

func routeConversation(refs observability.EventRefs) (cognition.InvocationScope, bool) {
	if refs.ConversationID == "" {
		return cognition.InvocationScope{}, false
	}
	s, err := cognition.NewInvocationScope(cognition.ScopeConversation, refs.ConversationID)
	if err != nil {
		return cognition.InvocationScope{}, false
	}
	return s, true
}

func routeWorker(refs observability.EventRefs) (cognition.InvocationScope, bool) {
	if refs.WorkerID == "" {
		return cognition.InvocationScope{}, false
	}
	s, err := cognition.NewInvocationScope(cognition.ScopeWorker, refs.WorkerID)
	if err != nil {
		return cognition.InvocationScope{}, false
	}
	return s, true
}

func routeGlobal(refs observability.EventRefs) (cognition.InvocationScope, bool) {
	_ = refs // refs unused — global scope is fixed.
	s, err := cognition.NewInvocationScope(cognition.ScopeGlobal, "")
	if err != nil {
		return cognition.InvocationScope{}, false
	}
	return s, true
}
