package executor

import (
	"context"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func evidenceGitOut(t *testing.T, gitBin, dir string, args ...string) string {
	t.Helper()
	cmd := exec.Command(gitBin, args...)
	cmd.Dir, cmd.Env = dir, gitEnv()
	b, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git %v: %v: %s", args, err, b)
	}
	return string(b)
}

func setupEvidenceOnly(t *testing.T, f *finalizeGateFixture, id, task string, failed bool) (string, string) {
	t.Helper()
	bare := t.TempDir()
	runGitIn(t, f.git, bare, "init", "-q", "--bare")
	runGitIn(t, f.git, f.repo, "remote", "add", "origin", bare)
	if _, err := f.fx.Provision(id); err != nil {
		t.Fatal(err)
	}
	ws, _ := f.fx.Layout().WorkspaceDir(id)
	branch := "ac-exec/" + task + "/" + id
	if err := f.prov.AddNewBranch(context.Background(), ws, branch, "main"); err != nil {
		t.Fatal(err)
	}
	must(t, f.tr.Write(Record{ExecutorID: id, PID: 1234, SpawnedAt: testNow, BaseRef: "main", RunnerCmd: []string{"go", "test", "./..."}}))
	in := inputWithTaskRef(id, task)
	in.DeliveryContract = DeliveryContractEvidenceOnly
	mustWriteInput(t, f.fx, in)
	if failed {
		err := &ErrorDetail{Kind: "test_failed", Message: "red test"}
		must(t, f.fx.WriteOutput(Output{ExecutorID: id, Success: false, Error: err, FinishedAt: testNow}))
		st := doneStatus(id)
		st.State = StateFailed
		st.Error = err
		st.Summary = "go test failed"
		must(t, f.fx.WriteStatus(*st))
	} else {
		must(t, f.fx.WriteOutput(*okOutput(id)))
		must(t, f.fx.WriteStatus(*doneStatus(id)))
	}
	return ws, bare
}

func TestEvidenceOnly_ZeroSourceDiffCreatesDurableArtifact(t *testing.T) {
	f := newFinalizeGateFixture(t)
	id, task := "exec-evidence-green", "task-green"
	ws, bare := setupEvidenceOnly(t, f, id, task, false)
	must(t, f.mon.Finalize(context.Background(), Completion{ExecutorID: id, Kind: OutcomeSucceeded, Output: okOutput(id), Status: doneStatus(id)}))
	if len(f.wb.reports) != 1 || f.wb.reports[0].Kind != OutcomeSucceeded {
		t.Fatalf("reports=%+v", f.wb.reports)
	}
	ev := f.wb.reports[0].Evidence
	if ev == nil || !ev.Pushed || !strings.HasPrefix(ev.Digest, "sha256:") {
		t.Fatalf("evidence=%+v", ev)
	}
	if !gitRefExists(f.git, bare, "refs/heads/ac-exec/"+task+"/"+id) {
		t.Fatal("evidence branch not pushed")
	}
	b, err := os.ReadFile(filepath.Join(ws, filepath.FromSlash(ev.Path)))
	if err != nil {
		t.Fatal(err)
	}
	var disk EvidenceArtifact
	if err := json.Unmarshal(b, &disk); err != nil {
		t.Fatal(err)
	}
	if disk.ReviewedSHA == "" || disk.BaseSHA != "main" || disk.ExitStatus != 0 || disk.Verdict != "pass" || len(disk.Commands) == 0 {
		t.Fatalf("disk=%+v", disk)
	}
}

func TestEvidenceOnly_RedVerdictStillDurable(t *testing.T) {
	f := newFinalizeGateFixture(t)
	id, task := "exec-evidence-red", "task-red"
	_, bare := setupEvidenceOnly(t, f, id, task, true)
	err := &ErrorDetail{Kind: "test_failed", Message: "red test"}
	st := doneStatus(id)
	st.State = StateFailed
	st.Error = err
	st.Summary = "go test failed"
	must(t, f.mon.Finalize(context.Background(), Completion{ExecutorID: id, Kind: OutcomeFailed, Error: err, Status: st}))
	got := f.wb.reports[0]
	if got.Kind != OutcomeFailed || got.Evidence == nil || !got.Evidence.Pushed || got.Evidence.Verdict != "fail" {
		t.Fatalf("completion=%+v", got)
	}
	if !gitRefExists(f.git, bare, "refs/heads/ac-exec/"+task+"/"+id) {
		t.Fatal("red evidence branch not pushed")
	}
}

func TestEvidenceOnlyPushFailureIsNonDelivery(t *testing.T) {
	f := newFinalizeGateFixture(t)
	id, task := "exec-evidence-refused", "task-refused"
	ws, _ := setupEvidenceOnly(t, f, id, task, false)
	// Violate the unique ac-exec branch guardrail: runtime must fail closed.
	runGitIn(t, f.git, ws, "checkout", "-q", "-b", "unexpected")
	must(t, f.mon.Finalize(context.Background(), Completion{ExecutorID: id, Kind: OutcomeSucceeded, Output: okOutput(id), Status: doneStatus(id)}))
	got := f.wb.reports[0]
	if got.Kind != OutcomeCrashed || !got.Retryable || got.Error == nil || got.Error.Kind != "non_delivery" {
		t.Fatalf("completion=%+v", got)
	}
	if got.Evidence == nil || got.Evidence.Error == "" {
		t.Fatalf("evidence failure ref lost: %+v", got.Evidence)
	}
}

func TestEvidenceOnlyFinalizeRetryDoesNotDuplicateCommit(t *testing.T) {
	f := newFinalizeGateFixture(t)
	id, task := "exec-evidence-retry", "task-retry"
	ws, _ := setupEvidenceOnly(t, f, id, task, false)
	c := Completion{ExecutorID: id, Kind: OutcomeSucceeded, Output: okOutput(id), Status: doneStatus(id)}
	must(t, f.mon.Finalize(context.Background(), c))
	before := strings.TrimSpace(evidenceGitOut(t, f.git, ws, "rev-list", "--count", "main..HEAD"))
	// Simulate a finalize/writeback retry while retained state still exists.
	must(t, f.mon.Finalize(context.Background(), c))
	after := strings.TrimSpace(evidenceGitOut(t, f.git, ws, "rev-list", "--count", "main..HEAD"))
	if before != "1" || after != before {
		t.Fatalf("evidence commits before=%s after=%s", before, after)
	}
}

func TestInputDeliveryContractUnknownFailsClosed(t *testing.T) {
	in := Input{ExecutorID: "exec-x", Goal: Goal{Title: "x"}, Model: "m", CreatedAt: testNow, DeliveryContract: "future"}
	if err := in.Validate(); err == nil || !strings.Contains(err.Error(), "unknown delivery_contract") {
		t.Fatalf("err=%v", err)
	}
	in.DeliveryContract = ""
	if err := in.Validate(); err != nil {
		t.Fatalf("legacy empty must remain code_change: %v", err)
	}
}
