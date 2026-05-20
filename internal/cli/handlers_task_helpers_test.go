package cli

import (
	"testing"

	"github.com/oopslink/agent-center/internal/taskruntime/execution"
	"github.com/oopslink/agent-center/internal/taskruntime/inputrequest"
)

func TestKillReasonFromString(t *testing.T) {
	if killReasonFromString("user_request") != execution.KilledUserRequest {
		t.Fatal("user_request")
	}
	if killReasonFromString("supervisor_request") != execution.KilledSupervisorRequest {
		t.Fatal("supervisor_request")
	}
	if killReasonFromString("garbage") != execution.KilledUserRequest {
		t.Fatal("default on unknown")
	}
}

func TestParseUrgency(t *testing.T) {
	u, err := parseUrgency("")
	if err != nil || u != inputrequest.UrgencyNormal {
		t.Fatalf("default: %v / %s", err, u)
	}
	u, err = parseUrgency("urgent")
	if err != nil || u != inputrequest.UrgencyUrgent {
		t.Fatalf("urgent: %v / %s", err, u)
	}
	if _, err := parseUrgency("garbage"); err == nil {
		t.Fatal("expected error")
	}
}
