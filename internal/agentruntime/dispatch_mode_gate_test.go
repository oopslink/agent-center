package agentruntime

import (
	"context"
	"fmt"
	"strings"
	"testing"

	"github.com/oopslink/agent-center/internal/agentruntime/executor"
)

// Explicit fork_executor dispatch gate.
//
// Every test here runs with an executor engine ATTACHED, i.e. concurrency ENABLED
// (ee != nil) — the only configuration in which the gate can change anything. With
// concurrency off the runtime injects regardless and there is nothing to suppress.
//
// The locks fall in three groups:
//
//	① default-path liveness  — absent / empty / executor_fork / unknown ⇒ STILL FORKS.
//	   This is the one that matters most: the gate suppressing a fork on anything
//	   other than an explicit supervisor_inline would starve every Dev node.
//	② the override itself    — explicit supervisor_inline ⇒ no fork; supervisor handles it inline.

// --- ① default-path liveness (red line #1) ----------------------------------

// TestSpawnExecutor_DispatchModeAbsent_StillForks is THE regression lock for I105 red
// line #1: a task with NO dispatch_mode (every pre-I105 task, and every task an older
// center serves) MUST fork exactly as before when concurrency is on. If this ever goes
// red, the gate's default inverted and every ordinary Dev node is starved.
func TestSpawnExecutor_DispatchModeAbsent_StillForks(t *testing.T) {
	sc := &scriptedToolCaller{getTaskBody: map[string]any{
		"id": "task-nodm", "title": "ordinary dev task", "status": "open", "model": "claude-haiku",
		// NOTE: no "dispatch_mode" key at all — the pre-I105 wire shape.
	}}
	rt, _, home := spawn(t, "agent-nodm", "task-nodm", sc)

	assertAdmissionForked(t, sc, "a task with no dispatch_mode must fork")
	if probs := loadRouting(t, home); len(probs) != 1 {
		t.Fatalf("absent dispatch_mode must FORK (default unchanged): problems=%+v", probs)
	}
	if got := rt.CurrentTaskID(); got != "task-nodm" {
		t.Errorf("currentTaskID = %q, want task-nodm (forked)", got)
	}
}

// TestSpawnExecutor_DispatchModeDefaults_StillFork covers the rest of the "must still
// fork" legacy input space: an empty string, a whitespace-only value, and —
// critically — an UNKNOWN value a newer center might send. None of them
// may suppress the fork: the gate is a strict equality test on supervisor_inline, so
// anything unrecognized degrades to today's behavior rather than stranding the node.
func TestSpawnExecutor_DispatchModeDefaults_StillFork(t *testing.T) {
	for _, tc := range []struct {
		name string
		mode any
	}{
		{"empty string", ""},
		{"whitespace only", "   "},
		{"unknown value from a newer center", "some_future_mode"},
		{"case mismatch is NOT inline", "SUPERVISOR_INLINE"},
		{"near-miss typo is NOT inline", "supervisor-inline"},
		{"wrong JSON type (number)", 42},
		{"wrong JSON type (bool)", true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			sc := &scriptedToolCaller{getTaskBody: map[string]any{
				"id": "task-df", "title": "dev task", "status": "open", "model": "claude-haiku",
				"dispatch_mode": tc.mode,
			}}
			_, _, home := spawn(t, "agent-df", "task-df", sc)

			// A wrong JSON type fails to decode into the string field. That is a MALFORMED
			// get_task response, which SpawnExecutor already treats as "leave queued" (it
			// returns before start_task) — the important part for I105 is that it never
			// routes inline. So assert on the two things that must hold for every case:
			// no inline route, and no silent inline suppression of a legitimate fork.
			if _, blocked := sc.callFor("fail_task"); blocked {
				t.Fatalf("dispatch_mode=%v must not take the inline/block path", tc.mode)
			}
			switch tc.mode.(type) {
			case string:
				if probs := loadRouting(t, home); len(probs) != 1 {
					t.Fatalf("dispatch_mode=%q must FORK (only supervisor_inline suppresses): problems=%+v", tc.mode, probs)
				}
			default:
				// Malformed body → decode error → left queued (pre-existing behavior),
				// asserted by the pre-existing TestSpawnExecutor_MalformedGetTaskSkips.
				if seen := sc.toolsSeen(); len(seen) != 1 || seen[0] != "get_task" {
					t.Fatalf("malformed dispatch_mode: tool calls = %v, want [get_task] (left queued)", seen)
				}
			}
		})
	}
}

func TestSpawnExecutor_ExplicitCodeDispatchWithoutRepoFailsClosed(t *testing.T) {
	sc := &scriptedToolCaller{getTaskBody: map[string]any{
		"id": "task-code", "title": "dev task", "status": "open", "model": "claude-haiku",
		"dispatch_mode": "executor_fork",
	}}
	_, _, home := spawn(t, "agent-code", "task-code", sc)

	if probs := loadRouting(t, home); len(probs) != 0 {
		t.Fatalf("missing repo_ref must not fork: %+v", probs)
	}
	body, ok := sc.callFor("fail_task")
	if !ok || !strings.Contains(fmt.Sprint(body["reason"]), "repo_ref") {
		t.Fatalf("missing repo_ref must fail as non-delivery, body=%v", body)
	}
}

// --- ② the override itself ---------------------------------------------------

// TestSpawnExecutor_SupervisorInline_RejectsFork is the explicit-dispatch lock:
// fork_executor is a fork primitive only. If the supervisor asks it to fork a
// supervisor_inline node, it refuses without start_task, injection, block, or fork.
func TestSpawnExecutor_SupervisorInline_RejectsFork(t *testing.T) {
	sc := &scriptedToolCaller{getTaskBody: map[string]any{
		"id": "task-inline", "title": "Deploy v2.31.0", "description": "cut the release",
		"status": "open", "model": "claude-haiku", "dispatch_mode": "supervisor_inline",
	}}
	rt, ee, home := engineForAgent(t, "agent-inline")
	attach(rt, ee) // concurrency ON — the gate must override it
	setToolCaller(rt, sc)
	fs := &fakeSession{}
	rt.withState(func(s *SessionState) { s.Session = fs })

	if _, err := rt.SpawnExecutor(context.Background(), SpawnRequest{TaskID: "task-inline"}); err != nil {
		t.Fatalf("SpawnExecutor: %v", err)
	}

	if probs := loadRouting(t, home); len(probs) != 0 {
		t.Fatalf("supervisor_inline node must NOT fork even with concurrency on: problems=%+v", probs)
	}
	if seen := sc.toolsSeen(); len(seen) != 1 || seen[0] != "get_task" {
		t.Fatalf("tool calls = %v — fork_executor must reject supervisor_inline after get_task only", seen)
	}
	if msgs := fs.msgs(); len(msgs) != 0 {
		t.Fatalf("fork_executor must not inject inline work; supervisor already owns that decision, got %v", msgs)
	}
	if got := rt.CurrentTaskID(); got != "" {
		t.Errorf("currentTaskID = %q — rejected fork must not claim the fork run-slot", got)
	}
}

// TestSpawnExecutor_SupervisorInline_ForeignAssignee_StillSkips locks the gate's
// PLACEMENT: it sits AFTER the issue-d118b5dc ② cross-namespace guard, so an inline
// mark on another agent's task must not become a licence to inject that task into THIS
// agent's supervisor. Ordering bug ⇒ cross-namespace injection.
func TestSpawnExecutor_SupervisorInline_ForeignAssignee_StillSkips(t *testing.T) {
	sc := &scriptedToolCaller{getTaskBody: map[string]any{
		"id": "task-foreign", "title": "someone else's center action", "status": "open",
		"assignee": "agent:agent-OTHER", "dispatch_mode": "supervisor_inline",
	}}
	rt, ee, home := engineForAgent(t, "agent-self")
	attach(rt, ee)
	setToolCaller(rt, sc)
	fs := &fakeSession{}
	rt.withState(func(s *SessionState) { s.Session = fs })

	if _, err := rt.SpawnExecutor(context.Background(), SpawnRequest{TaskID: "task-foreign"}); err != nil {
		t.Fatalf("SpawnExecutor: %v", err)
	}
	if msgs := fs.msgs(); len(msgs) != 0 {
		t.Fatalf("a FOREIGN task must never be injected into this agent's session, got %v", msgs)
	}
	if probs := loadRouting(t, home); len(probs) != 0 {
		t.Fatalf("foreign inline task must not fork either: problems=%+v", probs)
	}
}

// TestSpawnExecutor_SupervisorInline_AlreadyRunning_Skips locks that the gate sits
// after the status precheck too: a non-open task is not re-injected on a re-emitted
// work_available.
func TestSpawnExecutor_SupervisorInline_AlreadyRunning_Skips(t *testing.T) {
	sc := &scriptedToolCaller{getTaskBody: map[string]any{
		"id": "task-run", "title": "already running", "status": "running",
		"dispatch_mode": "supervisor_inline",
	}}
	rt, ee, _ := engineForAgent(t, "agent-run")
	attach(rt, ee)
	setToolCaller(rt, sc)
	fs := &fakeSession{}
	rt.withState(func(s *SessionState) { s.Session = fs })

	if _, err := rt.SpawnExecutor(context.Background(), SpawnRequest{TaskID: "task-run"}); err != nil {
		t.Fatalf("SpawnExecutor: %v", err)
	}
	if msgs := fs.msgs(); len(msgs) != 0 {
		t.Fatalf("a non-open inline task must not be re-injected, got %v", msgs)
	}
}

// --- NotifyWork (defense-in-depth branch) -----------------------------------

// TestNotifyWork_SupervisorInline_InjectsDespiteConcurrency locks the second gate: on
// the agent.work route, an explicit supervisor_inline must inject EVEN WHEN ee != nil.
// Pre-I105 `ee != nil` alone decided the fork.
//
// (No center producer emits agent.work today — the per-WorkItem re-emit was retired
// with AgentWorkItem — so this branch is defense-in-depth kept in sync with the live
// SpawnExecutor gate.)
func TestNotifyWork_SupervisorInline_InjectsDespiteConcurrency(t *testing.T) {
	rt, ee, home := engineForAgent(t, "agent-nwi")
	attach(rt, ee) // concurrency ON
	fs := &fakeSession{}
	rt.withState(func(s *SessionState) { s.Session = fs })

	err := rt.NotifyWork(context.Background(), WorkRequest{
		AgentID: "agent-nwi", TaskID: "t-inline", TaskRef: "task-inline",
		Brief: "do the center action", DispatchMode: "supervisor_inline",
	})
	if err != nil {
		t.Fatalf("NotifyWork: %v", err)
	}
	if msgs := fs.msgs(); len(msgs) != 1 || msgs[0] != "do the center action" {
		t.Fatalf("supervisor_inline work must be injected into the resident session, got %v", msgs)
	}
	if probs := loadRouting(t, home); len(probs) != 0 {
		t.Fatalf("supervisor_inline work must NOT fork despite concurrency: problems=%+v", probs)
	}
}

// TestNotifyWork_DispatchModeDefaults_StillFork is the NotifyWork half of red line #1:
// absent / empty / unknown ⇒ the legacy executor branch still wins whenever
// concurrency is on, and the brief is NOT injected into the resident session.
func TestNotifyWork_DispatchModeDefaults_StillFork(t *testing.T) {
	for _, mode := range []string{"", "   ", "some_future_mode", "SUPERVISOR_INLINE"} {
		t.Run("mode="+mode, func(t *testing.T) {
			rt, ee, home := engineForAgent(t, "agent-nwf")
			attach(rt, ee)
			fs := &fakeSession{}
			rt.withState(func(s *SessionState) { s.Session = fs })

			err := rt.NotifyWork(context.Background(), WorkRequest{
				AgentID: "agent-nwf", TaskID: "t-fork", TaskRef: "task-fork",
				Brief: "write the code", DispatchMode: mode,
			})
			if err != nil {
				t.Fatalf("NotifyWork: %v", err)
			}
			if msgs := fs.msgs(); len(msgs) != 0 {
				t.Errorf("dispatch_mode=%q must take the FORK branch, not inject: %v", mode, msgs)
			}
			if probs := loadRouting(t, home); len(probs) != 1 {
				t.Fatalf("dispatch_mode=%q must FORK when concurrency is on: problems=%+v", mode, probs)
			}
		})
	}
}

func TestNotifyWork_ExplicitCodeDispatchWithoutRepoIsDeclined(t *testing.T) {
	rt, ee, home := engineForAgent(t, "agent-nwf-code")
	attach(rt, ee)
	rt.withState(func(s *SessionState) { s.Session = &fakeSession{} })
	err := rt.NotifyWork(context.Background(), WorkRequest{
		AgentID: "agent-nwf-code", TaskID: "t-code", TaskRef: "task-code",
		Brief: "write the code", DispatchMode: executor.DispatchModeExecutorFork,
	})
	if err == nil || !strings.Contains(err.Error(), "repo_ref required") {
		t.Fatalf("explicit code dispatch without repo must be declined, got %v", err)
	}
	if probs := loadRouting(t, home); len(probs) != 1 {
		t.Fatalf("routing may register before launch validation, got %+v", probs)
	}
}

// TestRoutesSupervisorInline pins the predicate itself — the single point where the
// whole gate's default lives. Only the exact token (modulo surrounding whitespace)
// suppresses a fork.
func TestRoutesSupervisorInline(t *testing.T) {
	for _, tc := range []struct {
		in   string
		want bool
	}{
		{"supervisor_inline", true},
		{"  supervisor_inline  ", true},
		{"", false},
		{"   ", false},
		{"executor_fork", false},
		{"SUPERVISOR_INLINE", false},
		{"supervisor-inline", false},
		{"supervisor_inline_x", false},
		{"inline", false},
		{"garbage", false},
	} {
		if got := routesSupervisorInline(tc.in); got != tc.want {
			t.Errorf("routesSupervisorInline(%q) = %v, want %v", tc.in, got, tc.want)
		}
	}
}
