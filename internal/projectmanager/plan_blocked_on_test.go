package projectmanager

import "testing"

// TestWaitType_IsValid pins the 7 I103 §1 wait-type enum members and rejects an
// unknown value.
func TestWaitType_IsValid(t *testing.T) {
	valid := []WaitType{
		WaitUpstreamCompletion, WaitAcceptanceVerdict, WaitStageBarrier,
		WaitHumanDecision, WaitExternalEvent, WaitExecutorLiveness, WaitTimeoutOnly,
	}
	if len(valid) != 7 {
		t.Fatalf("expected 7 wait types, got %d", len(valid))
	}
	for _, w := range valid {
		if !w.IsValid() {
			t.Errorf("%q should be valid", w)
		}
	}
	for _, bad := range []WaitType{"", "bogus", "upstream"} {
		if bad.IsValid() {
			t.Errorf("%q should be invalid", bad)
		}
	}
}
