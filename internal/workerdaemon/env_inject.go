// Package workerdaemon implements the worker daemon's dispatch loop, shim
// supervisor, reconcile responder, and workspace manager (02-task-execution
// § 9 + ADR-0018).
package workerdaemon

import (
	"github.com/oopslink/agent-center/internal/taskruntime/dispatch"
)

// EnvInjection holds the environment variables a daemon injects when
// spawning a shim (02-task-execution § 9.3). SHIM_TOKEN is daemon→shim
// only; the shim must strip it before launching the agent.
type EnvInjection struct {
	ExecutionID    string
	TaskID         string
	ProjectID      string
	ConversationID string
	WorkspaceMode  string
	CWD            string
	Priority       string
	EtaAt          string
	ShimToken      string
	WorkerSocket   string
}

// ForShim returns env vars suitable for spawning the shim subprocess
// (includes SHIM_TOKEN but not WORKER_SOCK).
func (e EnvInjection) ForShim() map[string]string {
	out := map[string]string{
		"AGENT_CENTER_EXECUTION_ID":   e.ExecutionID,
		"AGENT_CENTER_TASK_ID":        e.TaskID,
		"AGENT_CENTER_PROJECT_ID":     e.ProjectID,
		"AGENT_CENTER_WORKSPACE_MODE": e.WorkspaceMode,
		"AGENT_CENTER_CWD":            e.CWD,
		"AGENT_CENTER_PRIORITY":       e.Priority,
		"AGENT_CENTER_SHIM_TOKEN":     e.ShimToken,
	}
	if e.ConversationID != "" {
		out["AGENT_CENTER_CONVERSATION_ID"] = e.ConversationID
	} else {
		out["AGENT_CENTER_CONVERSATION_ID"] = ""
	}
	if e.EtaAt != "" {
		out["AGENT_CENTER_ETA_AT"] = e.EtaAt
	} else {
		out["AGENT_CENTER_ETA_AT"] = ""
	}
	return out
}

// ForAgent returns env vars the shim hands to the actual agent CLI
// (SHIM_TOKEN stripped, WORKER_SOCK pointing at the shim's local socket).
func (e EnvInjection) ForAgent(shimSocketPath string) map[string]string {
	envs := e.ForShim()
	delete(envs, "AGENT_CENTER_SHIM_TOKEN")
	envs["AGENT_CENTER_WORKER_SOCK"] = shimSocketPath
	return envs
}

// FromEnvelope builds an EnvInjection from a DispatchEnvelope (the worker
// daemon's main consumer of envelope data).
func FromEnvelope(env dispatch.DispatchEnvelope, cwd, shimToken string) EnvInjection {
	eta := ""
	if env.EtaAt != nil {
		eta = env.EtaAt.UTC().Format("2006-01-02T15:04:05Z07:00")
	}
	return EnvInjection{
		ExecutionID:    string(env.ExecutionID),
		TaskID:         string(env.TaskID),
		ProjectID:      env.ProjectID,
		ConversationID: env.ConversationID,
		WorkspaceMode:  string(env.WorkspaceMode),
		CWD:            cwd,
		Priority:       env.Priority,
		EtaAt:          eta,
		ShimToken:      shimToken,
	}
}
