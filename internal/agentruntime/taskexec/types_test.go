package taskexec

import "testing"

func TestTaskExecutionStatus_IsValid(t *testing.T) {
	valid := []TaskExecutionStatus{StatusPending, StatusRunning, StatusPaused, StatusFailed, StatusDone}
	for _, s := range valid {
		if !s.IsValid() {
			t.Errorf("%q should be valid", s)
		}
	}
	if TaskExecutionStatus("bogus").IsValid() {
		t.Error("bogus should be invalid")
	}
}

func TestTaskExecutionStatus_IsTerminal(t *testing.T) {
	if !StatusFailed.IsTerminal() {
		t.Error("failed should be terminal")
	}
	if !StatusDone.IsTerminal() {
		t.Error("done should be terminal")
	}
	if StatusRunning.IsTerminal() {
		t.Error("running should not be terminal")
	}
}

func TestTaskExecutionMeta_Validate(t *testing.T) {
	m := TaskExecutionMeta{TaskID: "t-1", Status: StatusPending}
	if err := m.Validate(); err != nil {
		t.Errorf("valid meta: %v", err)
	}
	m.TaskID = ""
	if err := m.Validate(); err == nil {
		t.Error("empty TaskID should fail validation")
	}
	m.TaskID = "t-1"
	m.Status = "bogus"
	if err := m.Validate(); err == nil {
		t.Error("invalid status should fail validation")
	}
}
