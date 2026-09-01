package service

import (
	"context"
	"database/sql"
	"strings"

	"github.com/oopslink/agent-center/internal/persistence"
	pm "github.com/oopslink/agent-center/internal/projectmanager"
)

// Project mutation AppServices (B3-c — needed when the flat /api/projects PATCH
// + DELETE routes repoint to the pm Service). Project rename/describe/archive
// have NO cross-BC effect (a Project owns no Conversation), so these are pure
// PM-state writes — no outbox event needed (OQ1 outbox is only for cross-BC
// effects). Membership-gated like every project write.

// UpdateProjectCommand patches a Project's name/description (nil = unchanged).
type UpdateProjectCommand struct {
	ProjectID   pm.ProjectID
	Name        *string
	Description *string
	Actor       pm.IdentityRef
}

// UpdateProject applies the patch under the membership gate.
func (s *Service) UpdateProject(ctx context.Context, cmd UpdateProjectCommand) error {
	now := s.clock.Now()
	return s.runInTx(ctx, func(txCtx context.Context) error {
		p, err := s.projects.FindByID(txCtx, cmd.ProjectID)
		if err != nil {
			return err
		}
		if err := s.requireProjectMember(txCtx, p.ID(), cmd.Actor); err != nil {
			return err
		}
		if cmd.Name != nil {
			if err := p.Rename(*cmd.Name, now); err != nil {
				return err
			}
		}
		if cmd.Description != nil {
			p.SetDescription(*cmd.Description, now)
		}
		return s.projects.Update(txCtx, p)
	})
}

// ArchiveProject marks a Project archived (the DELETE /api/projects/{id} verb;
// v2.7 archives rather than hard-deletes).
func (s *Service) ArchiveProject(ctx context.Context, projectID pm.ProjectID, actor pm.IdentityRef) error {
	now := s.clock.Now()
	return s.runInTx(ctx, func(txCtx context.Context) error {
		p, err := s.projects.FindByID(txCtx, projectID)
		if err != nil {
			return err
		}
		if err := s.requireProjectMember(txCtx, p.ID(), actor); err != nil {
			return err
		}
		p.Archive(now)
		if err := s.projects.Update(txCtx, p); err != nil {
			return err
		}
		// ADR-0047: cascade-archive the project's built-in pool (it is "archived with
		// its project"). ArchiveWithProject accepts the always-running pool. Other
		// (structured) plans are left as-is — the archived project freezes all child
		// writes via requireProjectMutable.
		if s.plans != nil {
			plans, lerr := s.plans.ListByProject(txCtx, p.ID())
			if lerr != nil {
				return lerr
			}
			for _, pl := range plans {
				if !pl.IsBuiltin() {
					continue
				}
				if aerr := pl.ArchiveWithProject(now); aerr != nil {
					if aerr == pm.ErrPlanArchived {
						continue // already archived — idempotent
					}
					return aerr
				}
				if uerr := s.plans.Update(txCtx, pl); uerr != nil {
					return uerr
				}
			}
		}
		// Terminate the project's in-flight tasks. Archiving is ORTHOGONAL to task
		// status, so without this a non-terminal task (open/running/reopened) is left
		// LIVE under an archived project: it can no longer be worked (child writes are
		// frozen by requireProjectMutable) yet it still surfaces as in-flight work /
		// active-count in the org fleet view AND can no longer be discarded (discard_task
		// rejects an archived project → the orphan is unrecoverable). Conclude every
		// non-terminal task to discarded here — the same FinalizeForArchive the task
		// archive cascade (T339) uses — so archiving leaves no orphan running task.
		// A terminal task keeps its real outcome; an already-archived-but-non-terminal
		// leftover is concluded via the FinalizeArchived escape hatch.
		if s.tasks != nil {
			tasks, terr := s.tasks.ListByProject(txCtx, p.ID())
			if terr != nil {
				return terr
			}
			for _, t := range tasks {
				if t.Status().IsTerminal() {
					continue
				}
				var ferr error
				if t.IsArchived() {
					ferr = t.FinalizeArchived(now)
				} else {
					ferr = t.FinalizeForArchive(now)
				}
				if ferr != nil {
					return ferr
				}
				if uerr := s.tasks.Update(txCtx, t); uerr != nil {
					return uerr
				}
			}
		}
		return nil
	})
}

// DeleteArchivedProject permanently removes an already-archived Project and its
// project-scoped work data. Active projects must go through ArchiveProject first;
// this keeps the existing DELETE-on-active behavior as a soft lifecycle archive.
func (s *Service) DeleteArchivedProject(ctx context.Context, projectID pm.ProjectID, actor pm.IdentityRef) error {
	return s.runInTx(ctx, func(txCtx context.Context) error {
		p, err := s.projects.FindByID(txCtx, projectID)
		if err != nil {
			return err
		}
		if err := s.requireProjectMember(txCtx, p.ID(), actor); err != nil {
			return err
		}
		if p.Status() != pm.ProjectArchived {
			return pm.ErrProjectNotArchived
		}
		return s.deleteProjectCascade(txCtx, p.ID())
	})
}

func (s *Service) deleteProjectCascade(ctx context.Context, projectID pm.ProjectID) error {
	exec, _ := persistence.ExecutorFromCtx(ctx, s.db)
	plans, err := selectStrings(ctx, exec, `SELECT id FROM pm_plans WHERE project_id = ?`, string(projectID))
	if err != nil {
		return err
	}
	tasks, err := selectStrings(ctx, exec, `SELECT id FROM pm_tasks WHERE project_id = ?`, string(projectID))
	if err != nil {
		return err
	}
	issues, err := selectStrings(ctx, exec, `SELECT id FROM pm_issues WHERE project_id = ?`, string(projectID))
	if err != nil {
		return err
	}
	graphs, err := selectStrings(ctx, exec, `SELECT id FROM pm_graphs WHERE plan_id IN (`+placeholdersOrNull(len(plans))+`)`, anySlice(plans)...)
	if err != nil {
		return err
	}
	conversations, err := s.projectConversationIDs(ctx, exec, projectID, plans, tasks, issues)
	if err != nil {
		return err
	}
	if err := deleteConversationsByID(ctx, exec, conversations); err != nil {
		return err
	}
	if err := deleteProjectAuthorizationAssignments(ctx, exec, projectID, plans, tasks, issues); err != nil {
		return err
	}

	if err := deleteByIDs(ctx, exec, "pm_acceptances", "plan_id", plans); err != nil {
		return err
	}
	if err := deleteByIDs(ctx, exec, "pm_acceptances", "task_id", tasks); err != nil {
		return err
	}
	if err := deleteByIDs(ctx, exec, "pm_acceptances", "gate_task_id", tasks); err != nil {
		return err
	}
	if err := deleteByIDs(ctx, exec, "pm_delivery_subjects", "plan_id", plans); err != nil {
		return err
	}
	if err := deleteByIDs(ctx, exec, "pm_delivery_subjects", "task_id", tasks); err != nil {
		return err
	}
	for _, table := range []string{
		"pm_progress_observations", "pm_progress_obligations", "pm_progress_incidents",
		"pm_progress_checkpoints", "pm_progress_prerequisite_subscriptions",
		"pm_progress_wakes", "pm_progress_control_obligations", "pm_progress_control_incidents",
		"pm_progress_holds", "pm_progress_escalations", "pm_progress_watchdog_heartbeats",
		"pm_progress_wake_bucket_diagnostics",
		"pm_gate_verdicts", "pm_plan_continuations", "pm_remediation_proposals",
		"pm_plan_topology_outbox", "pm_stage_gate_reopen_requests", "pm_stages",
		"pm_plan_findings", "pm_plan_generations", "pm_plan_blocked_on",
		"pm_plan_decision_outcomes", "pm_plan_loop_rounds", "pm_plan_review_verdicts",
		"pm_plan_dispatch_records", "pm_task_dependencies",
	} {
		if err := deleteByIDs(ctx, exec, table, "plan_id", plans); err != nil {
			return err
		}
	}
	for _, planID := range plans {
		if _, err := exec.ExecContext(ctx, `DELETE FROM pm_progress_suppressed_wakes WHERE plan_ids_json LIKE ?`, `%`+planID+`%`); err != nil {
			return err
		}
	}
	for _, id := range append(append(append([]string{string(projectID)}, plans...), tasks...), issues...) {
		like := `%` + id + `%`
		if _, err := exec.ExecContext(ctx, `DELETE FROM outbox_events WHERE event_type LIKE 'pm.%' AND (refs LIKE ? OR payload LIKE ?)`, like, like); err != nil {
			return err
		}
	}
	if err := deleteByIDs(ctx, exec, "pm_graph_edges", "graph_id", graphs); err != nil {
		return err
	}
	if err := deleteByIDs(ctx, exec, "pm_graph_nodes", "graph_id", graphs); err != nil {
		return err
	}
	if err := deleteByIDs(ctx, exec, "pm_graphs", "id", graphs); err != nil {
		return err
	}
	if err := deleteByIDs(ctx, exec, "pm_task_subscribers", "task_id", tasks); err != nil {
		return err
	}
	if err := deleteByIDs(ctx, exec, "pm_task_action_logs", "task_id", tasks); err != nil {
		return err
	}
	if err := deleteByIDs(ctx, exec, "pm_assignment_pool_tasks", "task_id", tasks); err != nil {
		return err
	}
	if err := deleteByIDs(ctx, exec, "pm_issue_subscribers", "issue_id", issues); err != nil {
		return err
	}
	for _, stmt := range []string{
		`DELETE FROM pm_assignment_pool_tasks WHERE pool_id IN (SELECT id FROM pm_assignment_pools WHERE project_id = ?)`,
		`DELETE FROM pm_assignment_pools WHERE project_id = ?`,
		`DELETE FROM pm_code_repo_refs WHERE project_id = ?`,
		`DELETE FROM pm_project_members WHERE project_id = ?`,
		`DELETE FROM pm_audit_log WHERE project_id = ?`,
		`DELETE FROM pm_tasks WHERE project_id = ?`,
		`DELETE FROM pm_issues WHERE project_id = ?`,
		`DELETE FROM pm_plans WHERE project_id = ?`,
		`DELETE FROM pm_projects WHERE id = ?`,
	} {
		if _, err := exec.ExecContext(ctx, stmt, string(projectID)); err != nil {
			return err
		}
	}
	return nil
}

func deleteProjectAuthorizationAssignments(ctx context.Context, exec sqlExecutor, projectID pm.ProjectID, plans, tasks, issues []string) error {
	if _, err := exec.ExecContext(ctx, `DELETE FROM authorization_role_assignments WHERE resource_kind = 'project' AND resource_id = ?`, string(projectID)); err != nil {
		return err
	}
	if _, err := exec.ExecContext(ctx, `DELETE FROM authorization_role_assignments WHERE resource_kind IN ('issue','plan','task') AND resource_id = ?`, "project:"+string(projectID)+":*"); err != nil {
		return err
	}
	for _, item := range []struct {
		kind string
		ids  []string
	}{
		{kind: "issue", ids: issues},
		{kind: "plan", ids: plans},
		{kind: "task", ids: tasks},
	} {
		if len(item.ids) == 0 {
			continue
		}
		args := append([]any{item.kind}, anySlice(item.ids)...)
		if _, err := exec.ExecContext(ctx, `DELETE FROM authorization_role_assignments WHERE resource_kind = ? AND resource_id IN (`+placeholdersOrNull(len(item.ids))+`)`, args...); err != nil {
			return err
		}
	}
	return nil
}

func (s *Service) projectConversationIDs(ctx context.Context, exec sqlExecutor, projectID pm.ProjectID, plans, tasks, issues []string) ([]string, error) {
	owners := make([]string, 0, len(plans)+len(tasks)+len(issues))
	for _, id := range plans {
		owners = append(owners, "pm://plans/"+id)
	}
	for _, id := range tasks {
		owners = append(owners, "pm://tasks/"+id)
	}
	for _, id := range issues {
		owners = append(owners, "pm://issues/"+id)
	}
	if len(owners) == 0 {
		return selectStrings(ctx, exec, `SELECT id FROM conversations WHERE project_ref = ?`, string(projectID))
	}
	args := append(anySlice(owners), string(projectID))
	return selectStrings(ctx, exec, `SELECT id FROM conversations WHERE owner_ref IN (`+placeholdersOrNull(len(owners))+`) OR project_ref = ?`, args...)
}

type sqlExecutor interface {
	ExecContext(context.Context, string, ...any) (sql.Result, error)
	QueryContext(context.Context, string, ...any) (*sql.Rows, error)
}

func selectStrings(ctx context.Context, exec sqlExecutor, query string, args ...any) ([]string, error) {
	rows, err := exec.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []string
	for rows.Next() {
		var v string
		if err := rows.Scan(&v); err != nil {
			return nil, err
		}
		if strings.TrimSpace(v) != "" {
			out = append(out, v)
		}
	}
	return out, rows.Err()
}

func deleteConversationsByID(ctx context.Context, exec sqlExecutor, ids []string) error {
	if err := deleteByIDs(ctx, exec, "conversation_message_reference", "child_conversation_id", ids); err != nil {
		return err
	}
	if err := deleteByIDs(ctx, exec, "conversation_message_reference", "source_conversation_id", ids); err != nil {
		return err
	}
	if err := deleteByIDs(ctx, exec, "user_conversation_read_state", "conversation_id", ids); err != nil {
		return err
	}
	if err := deleteByIDs(ctx, exec, "user_conversation_follow_state", "conversation_id", ids); err != nil {
		return err
	}
	if err := deleteByIDs(ctx, exec, "messages", "conversation_id", ids); err != nil {
		return err
	}
	return deleteByIDs(ctx, exec, "conversations", "id", ids)
}

func deleteByIDs(ctx context.Context, exec sqlExecutor, table, column string, ids []string) error {
	if len(ids) == 0 {
		return nil
	}
	_, err := exec.ExecContext(ctx, `DELETE FROM `+table+` WHERE `+column+` IN (`+placeholdersOrNull(len(ids))+`)`, anySlice(ids)...)
	return err
}

func placeholdersOrNull(n int) string {
	if n == 0 {
		return "NULL"
	}
	return strings.TrimRight(strings.Repeat("?,", n), ",")
}

func anySlice(values []string) []any {
	out := make([]any, len(values))
	for i, v := range values {
		out[i] = v
	}
	return out
}
