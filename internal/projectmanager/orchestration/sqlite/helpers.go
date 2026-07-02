package sqlite

import (
	"encoding/json"
	"strings"
	"time"

	orch "github.com/oopslink/agent-center/internal/projectmanager/orchestration"
)

func ts(t time.Time) string { return t.UTC().Format(time.RFC3339Nano) }

func parseTime(s string) time.Time {
	t, _ := time.Parse(time.RFC3339Nano, s)
	return t
}

func isUnique(err error) bool {
	return err != nil && strings.Contains(strings.ToLower(err.Error()), "unique")
}

func unmarshalMetadata(s string) map[string]any {
	if strings.TrimSpace(s) == "" {
		return map[string]any{}
	}
	var m map[string]any
	if err := json.Unmarshal([]byte(s), &m); err != nil {
		return map[string]any{}
	}
	return m
}

func unmarshalActionLogs(s string) []orch.ActionLog {
	if strings.TrimSpace(s) == "" {
		return nil
	}
	var logs []orch.ActionLog
	if err := json.Unmarshal([]byte(s), &logs); err != nil {
		return nil
	}
	return logs
}
