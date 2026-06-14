package api

import (
	"strings"
	"testing"
	"time"

	agentbc "github.com/oopslink/agent-center/internal/agent"
)

// TestActivityPreviewText_NoRawJSONLeak is the class-guard for E2E finding F-5:
// an activity event with no human-meaningful top-level field (a system /
// commands_changed line whose only keys are type/subtype/raw) must NOT leak its
// raw stream-json payload ({"raw":{...}}) into the agents-list "Last activity"
// cell — the preview returns empty so the row renders its empty state.
func TestActivityPreviewText_NoRawJSONLeak(t *testing.T) {
	mk := func(eventType, payload string) *agentbc.AgentActivityEvent {
		ev, err := agentbc.NewActivityEvent(agentbc.NewActivityEventInput{
			ID: "ev-f5", AgentID: "agent-f5", EventType: eventType,
			Payload: payload, OccurredAt: time.Unix(1, 0).UTC(),
		})
		if err != nil {
			t.Fatalf("NewActivityEvent(%s): %v", eventType, err)
		}
		return ev
	}

	cases := []struct {
		name      string
		eventType string
		payload   string
		want      string // exact expected preview ("" = empty / no leak)
		wantLeak  bool   // if true, the bug would surface raw JSON
	}{
		{
			name:      "system commands_changed → empty (no raw leak)",
			eventType: "system",
			payload:   `{"type":"system","subtype":"commands_changed","raw":{"type":"system","subtype":"commands_changed","commands":[{"name":"bytedcli","description":"x"}]}}`,
			want:      "",
		},
		{
			name:      "assistant_text → the text",
			eventType: agentbc.EventTypeAssistantText,
			payload:   `{"text":"Done. pwd is /tmp","raw":{"type":"text","text":"Done. pwd is /tmp"}}`,
			want:      "Done. pwd is /tmp",
		},
		{
			name:      "non-json payload → empty (never echo raw string)",
			eventType: "system",
			payload:   `not-json-at-all {raw blob`,
			want:      "",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := activityPreviewText(mk(tc.eventType, tc.payload))
			if got != tc.want {
				t.Errorf("activityPreviewText = %q, want %q", got, tc.want)
			}
			// Defense in depth: regardless of the exact value, the preview must never
			// contain the raw-payload marker `{"raw":` that the F-5 bug leaked.
			if strings.Contains(got, `"raw":`) {
				t.Errorf("raw stream-json leaked into preview: %q", got)
			}
		})
	}
}
