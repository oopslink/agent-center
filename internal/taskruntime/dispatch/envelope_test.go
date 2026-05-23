package dispatch

import (
	"encoding/json"
	"errors"
	"testing"
	"time"

	"github.com/oopslink/agent-center/internal/taskruntime"
	"github.com/oopslink/agent-center/internal/taskruntime/execution"
	"github.com/oopslink/agent-center/internal/taskruntime/task"
)

var ref = time.Date(2026, 5, 21, 12, 0, 0, 0, time.UTC)

func mkEnvelope() DispatchEnvelope {
	return DispatchEnvelope{
		EnvelopeVersion: EnvelopeVersionV1,
		ExecutionID:     "E-1",
		TaskID:          "T-1",
		WorkerID:        "W-1",
		ProjectID:       "P-1",
		AgentCLI:        "claude-code",
		WorkspaceMode:   execution.WorkspaceWorktree,
		BaseBranch:      "main",
		TaskTitle:       "do thing",
		Priority:        "medium",
	}
}

func TestEnvelope_ValidateHappy(t *testing.T) {
	if err := mkEnvelope().Validate(); err != nil {
		t.Fatalf("validate: %v", err)
	}
}

func TestEnvelope_ValidateRejects(t *testing.T) {
	cases := []struct {
		name string
		mut  func(*DispatchEnvelope)
	}{
		{"version", func(e *DispatchEnvelope) { e.EnvelopeVersion = "vBogus" }},
		{"exec id", func(e *DispatchEnvelope) { e.ExecutionID = "" }},
		{"task id", func(e *DispatchEnvelope) { e.TaskID = "" }},
		{"worker id", func(e *DispatchEnvelope) { e.WorkerID = "" }},
		{"project id", func(e *DispatchEnvelope) { e.ProjectID = "" }},
		{"agent cli", func(e *DispatchEnvelope) { e.AgentCLI = "" }},
		{"workspace mode", func(e *DispatchEnvelope) { e.WorkspaceMode = "LOL" }},
		{"title", func(e *DispatchEnvelope) { e.TaskTitle = "" }},
		{"priority", func(e *DispatchEnvelope) { e.Priority = "" }},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			e := mkEnvelope()
			c.mut(&e)
			if err := e.Validate(); err == nil {
				t.Fatal("expected error")
			}
		})
	}
}

// V2 envelopes require AgentInstanceID; missing it should fail Validate.
func TestEnvelope_V2RequiresAgentInstanceID(t *testing.T) {
	e := mkEnvelope()
	e.EnvelopeVersion = EnvelopeVersionV2
	// AgentInstanceID intentionally not set
	if err := e.Validate(); err == nil {
		t.Fatal("expected v2 envelope without agent_instance_id to fail")
	}
	e.AgentInstanceID = "01HAI"
	if err := e.Validate(); err != nil {
		t.Fatalf("expected v2 envelope with agent_instance_id to validate, got %v", err)
	}
}

// V2 envelopes JSON roundtrip preserves AgentInstanceID.
func TestEnvelope_V2RoundTrip(t *testing.T) {
	in := mkEnvelope()
	in.EnvelopeVersion = EnvelopeVersionV2
	in.AgentInstanceID = "01HAI-COOL"
	data, err := json.Marshal(in)
	if err != nil {
		t.Fatal(err)
	}
	var out DispatchEnvelope
	if err := json.Unmarshal(data, &out); err != nil {
		t.Fatal(err)
	}
	if out.AgentInstanceID != "01HAI-COOL" {
		t.Fatalf("roundtrip agent_instance_id: %s", out.AgentInstanceID)
	}
	if out.EnvelopeVersion != EnvelopeVersionV2 {
		t.Fatalf("envelope_version: %s", out.EnvelopeVersion)
	}
}

func TestEnvelope_JSONRoundTrip(t *testing.T) {
	in := mkEnvelope()
	in.DependsOnTaskIDs = []taskruntime.TaskID{"T-2"}
	in.TaskDescription = "do it"
	data, err := json.Marshal(in)
	if err != nil {
		t.Fatal(err)
	}
	var out DispatchEnvelope
	if err := json.Unmarshal(data, &out); err != nil {
		t.Fatal(err)
	}
	if out.ExecutionID != in.ExecutionID || out.AgentCLI != in.AgentCLI {
		t.Fatalf("roundtrip mismatch: %+v", out)
	}
	if len(out.DependsOnTaskIDs) != 1 || out.DependsOnTaskIDs[0] != "T-2" {
		t.Fatalf("deps: %+v", out.DependsOnTaskIDs)
	}
}

func TestAck_ValidateAndParse(t *testing.T) {
	a := DispatchAck{
		ExecutionID: "E-1",
		Accepted:    true,
		AckedAt:     ref,
	}
	if err := a.Validate(); err != nil {
		t.Fatal(err)
	}
	data, _ := json.Marshal(a)
	parsed, err := ParseAck(data)
	if err != nil {
		t.Fatal(err)
	}
	if parsed.ExecutionID != "E-1" {
		t.Fatalf("parse: %+v", parsed)
	}
	if err := (DispatchAck{}).Validate(); err == nil {
		t.Fatal("expected error")
	}
	if err := (DispatchAck{ExecutionID: "E", Accepted: false, AckedAt: ref}).Validate(); err == nil {
		t.Fatal("expected accepted error")
	}
	if err := (DispatchAck{ExecutionID: "E", Accepted: true}).Validate(); err == nil {
		t.Fatal("expected ackedAt error")
	}
	if _, err := ParseAck([]byte("not-json")); err == nil {
		t.Fatal("expected parse error")
	}
}

func TestNack_ValidateAndParse(t *testing.T) {
	n := DispatchNack{
		ExecutionID: "E-1",
		Accepted:    false,
		Reason:      execution.NackWorkerAtCapacity,
		Message:     "too many",
		AckedAt:     ref,
	}
	if err := n.Validate(); err != nil {
		t.Fatal(err)
	}
	if n.FailedReason() != execution.DispatchNack(execution.NackWorkerAtCapacity) {
		t.Fatalf("failed reason: %s", n.FailedReason())
	}
	data, _ := json.Marshal(n)
	parsed, err := ParseNack(data)
	if err != nil {
		t.Fatal(err)
	}
	if parsed.Reason != execution.NackWorkerAtCapacity {
		t.Fatalf("parse: %+v", parsed)
	}
	if err := (DispatchNack{ExecutionID: "E", Accepted: true, AckedAt: ref, Reason: execution.NackWorkerAtCapacity, Message: "x"}).Validate(); err == nil {
		t.Fatal("expected accepted error")
	}
	if err := (DispatchNack{ExecutionID: "E", Accepted: false, AckedAt: ref, Reason: "BOGUS", Message: "x"}).Validate(); !errors.Is(err, execution.ErrUnknownReason) {
		t.Fatal("expected unknown reason")
	}
	if err := (DispatchNack{ExecutionID: "E", Accepted: false, AckedAt: ref, Reason: execution.NackWorkerAtCapacity, Message: ""}).Validate(); err == nil {
		t.Fatal("expected message required")
	}
	if err := (DispatchNack{Accepted: false, Reason: execution.NackWorkerAtCapacity, Message: "x", AckedAt: ref}).Validate(); err == nil {
		t.Fatal("expected exec id error")
	}
	if err := (DispatchNack{ExecutionID: "E", Accepted: false, Reason: execution.NackWorkerAtCapacity, Message: "x"}).Validate(); err == nil {
		t.Fatal("expected ackedAt error")
	}
	if _, err := ParseNack([]byte("not-json")); err == nil {
		t.Fatal("expected parse error")
	}
}

func TestIssueConcludeSpec_ValidateHappyAndCycles(t *testing.T) {
	spec := IssueConcludeSpec{
		IssueID:    "ISS-1",
		ProjectID:  "P-1",
		Resolution: "done",
		ActorID:    "user:hayang",
		Tasks: []IssueConcludeTaskSpec{
			{LocalID: "a", Title: "alpha", Priority: task.PriorityHigh},
			{LocalID: "b", Title: "beta", DependsOnLocalIDs: []string{"a"}},
		},
	}
	if err := spec.Validate(); err != nil {
		t.Fatalf("happy: %v", err)
	}

	// Cycle a → b → a
	bad := spec
	bad.Tasks = []IssueConcludeTaskSpec{
		{LocalID: "a", Title: "alpha", DependsOnLocalIDs: []string{"b"}},
		{LocalID: "b", Title: "beta", DependsOnLocalIDs: []string{"a"}},
	}
	if err := bad.Validate(); err == nil {
		t.Fatal("expected cycle")
	}

	// Unknown dep ref
	bad = spec
	bad.Tasks = []IssueConcludeTaskSpec{
		{LocalID: "a", Title: "alpha", DependsOnLocalIDs: []string{"c"}},
	}
	if err := bad.Validate(); err == nil {
		t.Fatal("expected unknown dep")
	}

	// Missing fields
	if err := (IssueConcludeSpec{}).Validate(); err == nil {
		t.Fatal("expected error")
	}
	if err := (IssueConcludeSpec{IssueID: "x"}).Validate(); err == nil {
		t.Fatal("expected error")
	}
	if err := (IssueConcludeSpec{IssueID: "x", ProjectID: "p"}).Validate(); err == nil {
		t.Fatal("expected error")
	}
	if err := (IssueConcludeSpec{IssueID: "x", ProjectID: "p", Resolution: "r"}).Validate(); err == nil {
		t.Fatal("expected error")
	}
	if err := (IssueConcludeSpec{IssueID: "x", ProjectID: "p", Resolution: "r", ActorID: "", Tasks: []IssueConcludeTaskSpec{{LocalID: "a", Title: "t"}}}).Validate(); err == nil {
		t.Fatal("expected actor error")
	}
	// duplicate local id
	dup := IssueConcludeSpec{
		IssueID: "i", ProjectID: "p", Resolution: "r", ActorID: "u",
		Tasks: []IssueConcludeTaskSpec{
			{LocalID: "a", Title: "alpha"},
			{LocalID: "a", Title: "beta"},
		},
	}
	if err := dup.Validate(); err == nil {
		t.Fatal("expected duplicate")
	}
	// missing title
	noTitle := IssueConcludeSpec{
		IssueID: "i", ProjectID: "p", Resolution: "r", ActorID: "u",
		Tasks: []IssueConcludeTaskSpec{{LocalID: "a"}},
	}
	if err := noTitle.Validate(); err == nil {
		t.Fatal("expected title")
	}
	// missing local id
	noLocal := IssueConcludeSpec{
		IssueID: "i", ProjectID: "p", Resolution: "r", ActorID: "u",
		Tasks: []IssueConcludeTaskSpec{{Title: "alpha"}},
	}
	if err := noLocal.Validate(); err == nil {
		t.Fatal("expected local_id")
	}
	// bad priority
	badPri := IssueConcludeSpec{
		IssueID: "i", ProjectID: "p", Resolution: "r", ActorID: "u",
		Tasks: []IssueConcludeTaskSpec{{LocalID: "a", Title: "t", Priority: "garbage"}},
	}
	if err := badPri.Validate(); err == nil {
		t.Fatal("expected priority")
	}
}
