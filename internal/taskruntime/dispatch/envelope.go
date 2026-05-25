// Package dispatch hosts the DispatchService domain service + dispatch
// protocol VOs (DispatchEnvelope / DispatchAck / DispatchNack) and the
// IssueConcludeSpawn stub (00-overview § 3.1 / § 3.4 + ADR-0011).
package dispatch

import (
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/oopslink/agent-center/internal/taskruntime"
	"github.com/oopslink/agent-center/internal/taskruntime/execution"
)

// EnvelopeVersionV1 is the v1 DispatchEnvelope schema version (with AgentCLI).
const EnvelopeVersionV1 = "v1"

// EnvelopeVersionV2 is the v2 DispatchEnvelope schema version (with
// AgentInstanceID per ADR-0024 / 0027). Worker daemon joins AgentInstance
// at spawn time to resolve agent_cli + home_dir.
const EnvelopeVersionV2 = "v2"

// DispatchEnvelope is the Center → Worker dispatch payload (02-task-
// execution § 5.2). v2 (per ADR-0024) introduces AgentInstanceID and
// retains AgentCLI as a denormalised convenience field for worker daemon
// spawn (filled from AgentInstance.AgentCLI by DispatchService).
type DispatchEnvelope struct {
	EnvelopeVersion          string                      `json:"envelope_version"`
	ExecutionID              taskruntime.TaskExecutionID `json:"execution_id"`
	TaskID                   taskruntime.TaskID          `json:"task_id"`
	WorkerID                 string                      `json:"worker_id"`
	ProjectID                string                      `json:"project_id"`
	ConversationID           string                      `json:"conversation_id,omitempty"`
	// AgentInstanceID is the v2 strong-binding to the AgentInstance AR
	// (per ADR-0024). Required in v2 envelopes; optional in v1.
	AgentInstanceID          string                      `json:"agent_instance_id,omitempty"`
	// AgentCLI is the resolved agent CLI kind ("claude-code", "codex", etc).
	// In v1 envelopes this is the user-supplied value; in v2 envelopes the
	// DispatchService fills it from AgentInstance.AgentCLI before send.
	AgentCLI                 string                      `json:"agent_cli"`
	WorkspaceMode            execution.WorkspaceMode     `json:"workspace_mode"`
	BaseBranch               string                      `json:"base_branch,omitempty"`
	TaskTitle                string                      `json:"task_title"`
	TaskDescription          string                      `json:"task_description,omitempty"`
	TaskDescriptionBlobRef   string                      `json:"task_description_blob_ref,omitempty"`
	FromIssueID              string                      `json:"from_issue_id,omitempty"`
	ParentTaskID             taskruntime.TaskID          `json:"parent_task_id,omitempty"`
	DependsOnTaskIDs         []taskruntime.TaskID        `json:"depends_on_task_ids,omitempty"`
	Priority                 string                      `json:"priority"`
	EtaAt                    *time.Time                  `json:"eta_at,omitempty"`
	ExecutionTimeoutOverride *int64                      `json:"execution_timeout_override_seconds,omitempty"`
	ExtraSkillFiles          []string                    `json:"extra_skill_files,omitempty"`
	// HomeDir is the agent_instance home directory on the worker host
	// (per ADR-0024 § 5 + ADR-0029 § 3). v2 envelopes fill this from
	// AgentResolution; v1 envelopes leave it empty. The worker daemon
	// reads `<HomeDir>/instructions.md` + `<HomeDir>/mcp_config.json`
	// (see internal/workerdaemon/prompt_assembly.go + mcp_injection.go).
	HomeDir string `json:"home_dir,omitempty"`
}

// Validate checks the envelope has required fields and a supported version.
// v2 envelopes additionally require AgentInstanceID.
func (e DispatchEnvelope) Validate() error {
	switch e.EnvelopeVersion {
	case EnvelopeVersionV1, EnvelopeVersionV2:
	default:
		return fmt.Errorf("dispatch envelope: unsupported version %q (require %q or %q)", e.EnvelopeVersion, EnvelopeVersionV1, EnvelopeVersionV2)
	}
	if strings.TrimSpace(string(e.ExecutionID)) == "" {
		return errors.New("dispatch envelope: execution_id required")
	}
	if strings.TrimSpace(string(e.TaskID)) == "" {
		return errors.New("dispatch envelope: task_id required")
	}
	if strings.TrimSpace(e.WorkerID) == "" {
		return errors.New("dispatch envelope: worker_id required")
	}
	if strings.TrimSpace(e.ProjectID) == "" {
		return errors.New("dispatch envelope: project_id required")
	}
	if strings.TrimSpace(e.AgentCLI) == "" {
		return errors.New("dispatch envelope: agent_cli required")
	}
	if e.EnvelopeVersion == EnvelopeVersionV2 && strings.TrimSpace(e.AgentInstanceID) == "" {
		return errors.New("dispatch envelope: agent_instance_id required for v2 envelopes")
	}
	if !e.WorkspaceMode.IsValid() {
		return fmt.Errorf("dispatch envelope: invalid workspace_mode %q", e.WorkspaceMode)
	}
	if strings.TrimSpace(e.TaskTitle) == "" {
		return errors.New("dispatch envelope: task_title required")
	}
	if strings.TrimSpace(e.Priority) == "" {
		return errors.New("dispatch envelope: priority required")
	}
	return nil
}

// MarshalJSON marshals the envelope.
func (e DispatchEnvelope) MarshalJSON() ([]byte, error) {
	type alias DispatchEnvelope
	return json.Marshal(alias(e))
}
