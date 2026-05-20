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

// EnvelopeVersionV1 is the current DispatchEnvelope schema version.
const EnvelopeVersionV1 = "v1"

// DispatchEnvelope is the Center → Worker dispatch payload (02-task-
// execution § 5.2).
type DispatchEnvelope struct {
	EnvelopeVersion          string                    `json:"envelope_version"`
	ExecutionID              taskruntime.TaskExecutionID `json:"execution_id"`
	TaskID                   taskruntime.TaskID         `json:"task_id"`
	WorkerID                 string                    `json:"worker_id"`
	ProjectID                string                    `json:"project_id"`
	ConversationID           string                    `json:"conversation_id,omitempty"`
	AgentCLI                 string                    `json:"agent_cli"`
	WorkspaceMode            execution.WorkspaceMode   `json:"workspace_mode"`
	BaseBranch               string                    `json:"base_branch,omitempty"`
	TaskTitle                string                    `json:"task_title"`
	TaskDescription          string                    `json:"task_description,omitempty"`
	TaskDescriptionBlobRef   string                    `json:"task_description_blob_ref,omitempty"`
	FromIssueID              string                    `json:"from_issue_id,omitempty"`
	ParentTaskID             taskruntime.TaskID         `json:"parent_task_id,omitempty"`
	DependsOnTaskIDs         []taskruntime.TaskID       `json:"depends_on_task_ids,omitempty"`
	Priority                 string                    `json:"priority"`
	EtaAt                    *time.Time                `json:"eta_at,omitempty"`
	ExecutionTimeoutOverride *int64                    `json:"execution_timeout_override_seconds,omitempty"`
	ExtraSkillFiles          []string                  `json:"extra_skill_files,omitempty"`
}

// Validate checks the envelope has required fields and a supported version.
func (e DispatchEnvelope) Validate() error {
	if e.EnvelopeVersion != EnvelopeVersionV1 {
		return fmt.Errorf("dispatch envelope: unsupported version %q (require %q)", e.EnvelopeVersion, EnvelopeVersionV1)
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
