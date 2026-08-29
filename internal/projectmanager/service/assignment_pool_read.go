package service

import (
	"context"

	pm "github.com/oopslink/agent-center/internal/projectmanager"
)

type AssignmentPoolTaskView struct {
	Membership pm.AssignmentPoolTask
	Task       *pm.Task
	Claimable  bool
	Starved    bool
}

type AssignmentPoolDetail struct {
	Pool  *pm.AssignmentPool
	Tasks []AssignmentPoolTaskView
}

func (s *Service) GetAssignmentPool(ctx context.Context, projectID pm.ProjectID, actor pm.IdentityRef) (*AssignmentPoolDetail, error) {
	if s.pools == nil {
		return nil, pm.ErrAssignmentPoolNotFound
	}
	if err := s.requireProjectMember(ctx, projectID, actor); err != nil {
		return nil, err
	}
	pool, err := s.pools.FindByProject(ctx, projectID)
	if err != nil {
		return nil, err
	}
	members, err := s.pools.ListTasks(ctx, pool.ID())
	if err != nil {
		return nil, err
	}
	// task.plan_id is the authoritative structured-Plan membership. Filter legacy
	// duplicate pool rows here as a read-side compatibility guard while migration
	// 0147 and Plan-selection writes clean them physically.
	visibleMembers := make([]pm.AssignmentPoolTask, 0, len(members))
	tasks := make([]*pm.Task, 0, len(members))
	for _, member := range members {
		task, err := s.tasks.FindByID(ctx, member.TaskID)
		if err != nil {
			return nil, err
		}
		if task.PlanID() != "" {
			continue
		}
		visibleMembers = append(visibleMembers, member)
		tasks = append(tasks, task)
	}
	starved, err := s.StarvedPoolTasks(ctx, projectID, tasks)
	if err != nil {
		return nil, err
	}
	detail := &AssignmentPoolDetail{Pool: pool, Tasks: make([]AssignmentPoolTaskView, 0, len(visibleMembers))}
	for i, member := range visibleMembers {
		task := tasks[i]
		detail.Tasks = append(detail.Tasks, AssignmentPoolTaskView{
			Membership: member, Task: task,
			Claimable: !task.IsArchived() && task.Status() == pm.TaskOpen && task.Assignee() == "" && member.ClaimedBy == "",
			Starved:   starved[task.ID()],
		})
	}
	return detail, nil
}

func (s *Service) RemoveTaskFromAssignmentPool(ctx context.Context, projectID pm.ProjectID, taskID pm.TaskID, actor pm.IdentityRef) error {
	if s.pools == nil {
		return pm.ErrAssignmentPoolNotFound
	}
	return s.runInTx(ctx, func(txCtx context.Context) error {
		if err := s.requireProjectMember(txCtx, projectID, actor); err != nil {
			return err
		}
		task, err := s.tasks.FindByID(txCtx, taskID)
		if err != nil {
			return err
		}
		if task.ProjectID() != projectID {
			return pm.ErrCrossProject
		}
		if task.Status() == pm.TaskRunning {
			return pm.ErrPlanNodeInFlight
		}
		pool, err := s.pools.FindByProject(txCtx, projectID)
		if err != nil {
			return err
		}
		return s.pools.RemoveTask(txCtx, pool.ID(), taskID)
	})
}
