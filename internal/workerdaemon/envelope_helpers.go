package workerdaemon

import (
	"encoding/json"

	"github.com/oopslink/agent-center/internal/taskruntime/execution"
)

// parseWorkspaceModeFromEnvelope is a best-effort decode of `workspace_
// mode` from an envelope.json blob.
func parseWorkspaceModeFromEnvelope(b []byte) (execution.WorkspaceMode, bool) {
	var v struct {
		WorkspaceMode string `json:"workspace_mode"`
	}
	if err := json.Unmarshal(b, &v); err != nil {
		return "", false
	}
	m := execution.WorkspaceMode(v.WorkspaceMode)
	if !m.IsValid() {
		return "", false
	}
	return m, true
}

// parseProjectIDFromEnvelope decodes the project_id.
func parseProjectIDFromEnvelope(b []byte) (string, bool) {
	var v struct {
		ProjectID string `json:"project_id"`
	}
	if err := json.Unmarshal(b, &v); err != nil {
		return "", false
	}
	if v.ProjectID == "" {
		return "", false
	}
	return v.ProjectID, true
}
