package cli

import (
	"context"
	"errors"
	"strings"
	"time"

	"github.com/oopslink/agent-center/internal/agent"
	"github.com/oopslink/agent-center/internal/environment"
	pm "github.com/oopslink/agent-center/internal/projectmanager"
	"github.com/oopslink/agent-center/internal/workforce"
)

type dispatchWorkerReads interface {
	FindByID(ctx context.Context, id workforce.WorkerID) (*workforce.Worker, error)
}

type dispatchTaskReads interface {
	GetTask(ctx context.Context, id pm.TaskID) (*pm.Task, error)
}

type dispatchCapabilityGate struct {
	workers dispatchWorkerReads
	waits   environment.CapabilityWaitRepository
	now     func() time.Time
}

type dispatchCapabilityDecision struct {
	OK       bool
	Reason   string
	CLI      string
	WorkerID string
}

const defaultCapabilityWaitTimeout = 15 * time.Minute

func newDispatchCapabilityGate(workers dispatchWorkerReads, waits environment.CapabilityWaitRepository, now func() time.Time) dispatchCapabilityGate {
	if now == nil {
		now = func() time.Time { return time.Now().UTC() }
	}
	return dispatchCapabilityGate{workers: workers, waits: waits, now: now}
}

func (g dispatchCapabilityGate) allows(ctx context.Context, ag *agent.Agent, task *pm.Task) (dispatchCapabilityDecision, error) {
	if g.workers == nil {
		return dispatchCapabilityDecision{OK: true}, nil
	}
	if ag == nil {
		return dispatchCapabilityDecision{OK: false, Reason: workforce.CapabilityMatchMissingCLI}, nil
	}
	workerID := strings.TrimSpace(ag.WorkerID())
	if workerID == "" {
		return dispatchCapabilityDecision{OK: false, Reason: workforce.CapabilityMatchWorkerOffline}, nil
	}
	w, err := g.workers.FindByID(ctx, workforce.WorkerID(workerID))
	if err != nil {
		if errors.Is(err, workforce.ErrWorkerNotFound) {
			return dispatchCapabilityDecision{OK: false, Reason: workforce.CapabilityMatchWorkerOffline, WorkerID: workerID}, nil
		}
		return dispatchCapabilityDecision{}, err
	}
	now := g.now().UTC()
	supervisorCLI := strings.TrimSpace(ag.Profile().CLI)
	if supervisorCLI == "" {
		supervisorCLI = agent.DefaultExecutorCLI
	}
	if match := w.CapabilityMatches(workforce.CapabilityRequirement{AgentCLI: supervisorCLI}, now); !match.OK {
		return dispatchCapabilityDecision{OK: false, Reason: match.Reason, CLI: match.CLI, WorkerID: workerID}, nil
	}
	if task != nil && !task.DispatchMode().RoutesInline() && ag.Profile().ConcurrencyEnabled() {
		decision := dispatchCapabilityDecision{
			OK:       false,
			Reason:   workforce.CapabilityMatchMissingCLI,
			WorkerID: workerID,
		}
		for _, ex := range ag.Profile().AllowedExecutors {
			cli := strings.TrimSpace(ex.CLI)
			if cli == "" {
				continue
			}
			match := w.CapabilityMatches(workforce.CapabilityRequirement{AgentCLI: cli}, now)
			if match.OK {
				return dispatchCapabilityDecision{OK: true, WorkerID: workerID}, nil
			}
			if decision.CLI == "" {
				decision.Reason = match.Reason
				decision.CLI = match.CLI
			}
		}
		if decision.CLI == "" {
			decision.CLI = supervisorCLI
		}
		return decision, nil
	}
	return dispatchCapabilityDecision{OK: true, WorkerID: workerID}, nil
}

func (g dispatchCapabilityGate) resolveWait(ctx context.Context, taskID string, ag *agent.Agent) error {
	if g.waits == nil || ag == nil || taskID == "" {
		return nil
	}
	return g.waits.Resolve(ctx, taskID, string(ag.ID()), g.now().UTC())
}

func (g dispatchCapabilityGate) enterWait(ctx context.Context, assigneeRef, taskID string, ag *agent.Agent, decision dispatchCapabilityDecision) error {
	if g.waits == nil || ag == nil || taskID == "" {
		return nil
	}
	now := g.now().UTC()
	workerID := decision.WorkerID
	if workerID == "" {
		workerID = strings.TrimSpace(ag.WorkerID())
	}
	reason := strings.TrimSpace(decision.Reason)
	if reason == "" {
		reason = workforce.CapabilityMatchMissingCLI
	}
	return g.waits.UpsertWaiting(ctx, environment.CapabilityWait{
		TaskID:      taskID,
		AgentID:     string(ag.ID()),
		AssigneeRef: assigneeRef,
		WorkerID:    workerID,
		RequiredCLI: decision.CLI,
		Reason:      reason,
		Status:      environment.CapabilityWaitWaiting,
		CreatedAt:   now,
		UpdatedAt:   now,
		ExpiresAt:   now.Add(defaultCapabilityWaitTimeout),
	})
}

func taskForCapabilityGate(ctx context.Context, pmr any, taskID string) *pm.Task {
	tr, ok := pmr.(dispatchTaskReads)
	if !ok || strings.TrimSpace(taskID) == "" {
		return nil
	}
	task, err := tr.GetTask(ctx, pm.TaskID(taskID))
	if err != nil {
		return nil
	}
	return task
}
