package taskruntime

import "testing"

func TestIDsStringRoundtrip(t *testing.T) {
	if TaskID("T-1").String() != "T-1" {
		t.Fatal("task")
	}
	if TaskExecutionID("E-1").String() != "E-1" {
		t.Fatal("execution")
	}
	if InputRequestID("IR-1").String() != "IR-1" {
		t.Fatal("input_request")
	}
	if ArtifactID("A-1").String() != "A-1" {
		t.Fatal("artifact")
	}
}
