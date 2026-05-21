package main

import (
	"bytes"
	"strings"
	"testing"
)

func TestRun_HappyPath(t *testing.T) {
	script := `{"type":"start","text":"hi"}
{"type":"progress","milestone":"step_1"}
{"type":"done","exit_code":0}
`
	var out bytes.Buffer
	if err := run(strings.NewReader(script), &out); err != nil {
		t.Fatalf("err: %v", err)
	}
	if !strings.Contains(out.String(), "step_1") || !strings.Contains(out.String(), "done") {
		t.Errorf("out: %s", out.String())
	}
}

func TestRun_BlankLinesIgnored(t *testing.T) {
	script := `{"type":"start"}

{"type":"done"}`
	var out bytes.Buffer
	if err := run(strings.NewReader(script), &out); err != nil {
		t.Fatal(err)
	}
	if strings.Count(out.String(), "\n") != 2 {
		t.Errorf("expected 2 lines, got %d", strings.Count(out.String(), "\n"))
	}
}

func TestRun_FailpointWithEnv(t *testing.T) {
	t.Setenv("FAKEAGENT_FAIL_AT", "step_3")
	script := `{"type":"progress","milestone":"step_1"}
{"type":"progress","milestone":"step_2"}
{"type":"progress","milestone":"step_3"}
{"type":"progress","milestone":"step_4"}
`
	var out bytes.Buffer
	if err := run(strings.NewReader(script), &out); err != nil {
		t.Fatalf("err: %v", err)
	}
	if !strings.Contains(out.String(), "failed") {
		t.Errorf("expected failed line, got: %s", out.String())
	}
	if strings.Contains(out.String(), "step_4") {
		t.Errorf("step_4 should not have been emitted: %s", out.String())
	}
}
