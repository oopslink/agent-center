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
	return setupEvidenceOnlyAt(t, f, id, task, failed, false)
}

func setupEvidenceOnlyAt(t *testing.T, f *finalizeGateFixture, id, task string, failed, recordedWorkspace bool) (string, string) {
	t.Helper()
	bare := t.TempDir()
	runGitIn(t, f.git, bare, "init", "-q", "--bare")
	runGitIn(t, f.git, f.repo, "remote", "add", "origin", bare)
	if _, err := f.fx.Provision(id); err != nil {
		t.Fatal(err)
	}
	ws, _ := f.fx.Layout().WorkspaceDir(id)
	if recordedWorkspace {
		ws = filepath.Join(t.TempDir(), "runtime-worktrees", id)
	}
	branch := "ac-exec/" + task + "/" + id
	if err := f.prov.AddNewBranch(context.Background(), ws, branch, "main"); err != nil {
		t.Fatal(err)
	}
	must(t, f.tr.Write(Record{ExecutorID: id, PID: 1234, SpawnedAt: testNow, BaseRef: "main", RunnerCmd: []string{"codex", "exec", "--json", "run executor"}, WorkspacePath: ws}))
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

func TestEvidenceOnly_UsesRecordedWorkspacePath(t *testing.T) {
	f := newFinalizeGateFixture(t)
	id, task := "exec-evidence-recorded", "task-evidence-recorded"
	ws, bare := setupEvidenceOnlyAt(t, f, id, task, false, true)
	writeEvidenceCommandEvent(t, f.fx, id, "cmd-recorded", "go test ./...", 0)
	must(t, f.mon.Finalize(context.Background(), Completion{ExecutorID: id, Kind: OutcomeSucceeded, Output: okOutput(id), Status: doneStatus(id)}))

	got := f.wb.reports[0]
	if got.Kind != OutcomeSucceeded || got.Evidence == nil || got.Git == nil || !got.Git.Pushed {
		t.Fatalf("recorded-workspace evidence delivery=%+v", got)
	}
	if _, err := os.Stat(filepath.Join(ws, filepath.FromSlash(got.Evidence.Path))); err != nil {
		t.Fatalf("evidence artifact not written to recorded workspace: %v", err)
	}
	layoutWS, _ := f.fx.Layout().WorkspaceDir(id)
	if _, err := os.Stat(filepath.Join(layoutWS, filepath.FromSlash(got.Evidence.Path))); !os.IsNotExist(err) {
		t.Fatalf("evidence must not be written to exchange workspace, stat err=%v", err)
	}
	if !gitRefExists(f.git, bare, "refs/heads/ac-exec/"+task+"/"+id) {
		t.Fatal("recorded-workspace evidence branch not pushed")
	}
}

func TestEvidenceOnly_MissingGitAndFailedPersistenceAreExplicit(t *testing.T) {
	f := newFinalizeGateFixture(t)
	id, task := "exec-evidence-no-git", "task-evidence-no-git"
	if _, err := f.fx.Provision(id); err != nil {
		t.Fatal(err)
	}
	in := inputWithTaskRef(id, task)
	in.DeliveryContract = DeliveryContractEvidenceOnly
	mustWriteInput(t, f.fx, in)
	got := f.mon.materializeEvidence(context.Background(), Completion{ExecutorID: id, Kind: OutcomeSucceeded})
	if got.Kind != OutcomeCrashed || got.Error == nil || got.Error.Kind != "non_delivery" || got.Evidence == nil || got.Evidence.Error == "" {
		t.Fatalf("missing git evidence result=%+v", got)
	}

	failed := f.mon.evidenceNonDelivery(Completion{Kind: OutcomeFailed, Evidence: &EvidenceArtifact{}}, "disk full")
	if failed.Kind != OutcomeFailed || failed.Error == nil || failed.Error.Kind != "evidence_persistence" || failed.Evidence.Error != "disk full" {
		t.Fatalf("failed verdict without prior error=%+v", failed)
	}
	failed = f.mon.evidenceNonDelivery(Completion{Kind: OutcomeFailed, Error: &ErrorDetail{Kind: "test_failed", Message: "red"}}, "push failed")
	if failed.Error == nil || !strings.Contains(failed.Error.Message, "red — evidence persistence: push failed") {
		t.Fatalf("failed verdict must retain both errors: %+v", failed)
	}

	commands, available, reason, path, digest := f.mon.evidenceCommands("exec-without-events")
	if len(commands) != 0 || available || reason == "" || path == "" || digest == "" {
		t.Fatalf("missing command evidence must be explicit: commands=%v available=%v reason=%q path=%q digest=%q", commands, available, reason, path, digest)
	}
	if commands, available, reason := evidenceCommandsFromEvents(nil); len(commands) != 0 || available || reason == "" {
		t.Fatalf("empty event stream must be unavailable: commands=%v available=%v reason=%q", commands, available, reason)
	}
}

func TestEvidenceCommands_IncompleteCapturesFailClosed(t *testing.T) {
	zero := 0
	cases := []struct {
		name   string
		events []CommandExecutionEvent
		want   string
	}{
		{
			name: "finished without command",
			events: []CommandExecutionEvent{{
				Type: commandEventFinished, ToolUseID: "cmd-missing", ExitStatus: &zero, ExitStatusAvailable: true,
			}},
			want: "missing command",
		},
		{
			name: "finished without exit status",
			events: []CommandExecutionEvent{{
				Type: commandEventStarted, ToolUseID: "cmd-no-exit", Command: "go test ./...", Source: commandEventSourceCodex,
			}, {
				Type: commandEventFinished, ToolUseID: "cmd-no-exit",
			}},
			want: "missing exit_status",
		},
		{
			name: "started without completion",
			events: []CommandExecutionEvent{{
				Type: commandEventStarted, ToolUseID: "cmd-open",
			}},
			want: "missing completion",
		},
		{
			name:   "no command events",
			events: []CommandExecutionEvent{{Type: "ignored"}},
			want:   "no completed command executions",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			commands, available, reason := evidenceCommandsFromEvents(tc.events)
			if available || len(commands) != 0 || !strings.Contains(reason, tc.want) {
				t.Fatalf("commands=%v available=%v reason=%q, want %q", commands, available, reason, tc.want)
			}
		})
	}
}

func writeEvidenceCommandEvent(t *testing.T, fx *FileExchange, id, toolUseID, command string, exitStatus int) {
	t.Helper()
	must(t, fx.AppendCommandEvent(id, CommandExecutionEvent{
		Type:      commandEventStarted,
		Source:    commandEventSourceCodex,
		ToolUseID: toolUseID,
		ToolName:  "shell",
		Command:   command,
	}))
	status := exitStatus
	must(t, fx.AppendCommandEvent(id, CommandExecutionEvent{
		Type:                commandEventFinished,
		Source:              commandEventSourceCodex,
		ToolUseID:           toolUseID,
		ToolName:            "shell",
		Command:             command,
		ExitStatus:          &status,
		ExitStatusAvailable: true,
	}))
}

func TestEvidenceOnly_ZeroSourceDiffCreatesDurableArtifact(t *testing.T) {
	f := newFinalizeGateFixture(t)
	id, task := "exec-evidence-green", "task-green"
	ws, bare := setupEvidenceOnly(t, f, id, task, false)
	writeEvidenceCommandEvent(t, f.fx, id, "cmd-green", "go test ./...", 0)
	must(t, f.mon.Finalize(context.Background(), Completion{ExecutorID: id, Kind: OutcomeSucceeded, Output: okOutput(id), Status: doneStatus(id)}))
	if len(f.wb.reports) != 1 || f.wb.reports[0].Kind != OutcomeSucceeded {
		t.Fatalf("reports=%+v", f.wb.reports)
	}
	ev := f.wb.reports[0].Evidence
	if ev == nil || !ev.CommandsAvailable || !strings.HasPrefix(ev.Digest, "sha256:") || !strings.HasPrefix(ev.CommandEventDigest, "sha256:") {
		t.Fatalf("evidence=%+v", ev)
	}
	if f.wb.reports[0].Git == nil || !f.wb.reports[0].Git.Pushed {
		t.Fatalf("evidence branch push not reflected in git status: %+v", f.wb.reports[0].Git)
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
	if disk.ReviewedSHA == "" || disk.BaseSHA != "main" || disk.ExitStatus != 0 || disk.Verdict != "pass" || len(disk.Commands) != 1 {
		t.Fatalf("disk=%+v", disk)
	}
	if disk.Commands[0].Command != "go test ./..." || disk.Commands[0].ExitStatus != 0 {
		t.Fatalf("commands must be real verification events, not RunnerCmd: %+v", disk.Commands)
	}
	if strings.Contains(string(b), `"pushed"`) {
		t.Fatalf("artifact JSON must not persist a self-contradictory pushed field: %s", b)
	}
	remote := evidenceGitOut(t, f.git, bare, "show", "refs/heads/ac-exec/"+task+"/"+id+":"+ev.Path)
	if strings.Contains(remote, `"pushed"`) {
		t.Fatalf("remote artifact must not claim pushed=false/true inside JSON: %s", remote)
	}
}

func TestEvidenceOnly_RedVerdictStillDurable(t *testing.T) {
	f := newFinalizeGateFixture(t)
	id, task := "exec-evidence-red", "task-red"
	_, bare := setupEvidenceOnly(t, f, id, task, true)
	writeEvidenceCommandEvent(t, f.fx, id, "cmd-red", "go test -race ./...", 1)
	err := &ErrorDetail{Kind: "test_failed", Message: "red test"}
	st := doneStatus(id)
	st.State = StateFailed
	st.Error = err
	st.Summary = "go test failed"
	must(t, f.mon.Finalize(context.Background(), Completion{ExecutorID: id, Kind: OutcomeFailed, Error: err, Status: st}))
	got := f.wb.reports[0]
	if got.Kind != OutcomeFailed || got.Evidence == nil || !got.Evidence.CommandsAvailable || got.Evidence.Verdict != "fail" {
		t.Fatalf("completion=%+v", got)
	}
	if got.Git == nil || !got.Git.Pushed {
		t.Fatalf("red evidence branch push not reflected in git status: %+v", got.Git)
	}
	if len(got.Evidence.Commands) != 1 || got.Evidence.Commands[0].ExitStatus != 1 {
		t.Fatalf("red evidence must carry per-command exit status: %+v", got.Evidence.Commands)
	}
	if !gitRefExists(f.git, bare, "refs/heads/ac-exec/"+task+"/"+id) {
		t.Fatal("red evidence branch not pushed")
	}
}

func TestEvidenceOnlyPushFailureIsNonDelivery(t *testing.T) {
	f := newFinalizeGateFixture(t)
	id, task := "exec-evidence-refused", "task-refused"
	ws, _ := setupEvidenceOnly(t, f, id, task, false)
	writeEvidenceCommandEvent(t, f.fx, id, "cmd-refused", "go test ./...", 0)
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

func TestEvidenceOnlyCommandEventsUnavailableIsExplicitNonDelivery(t *testing.T) {
	f := newFinalizeGateFixture(t)
	id, task := "exec-evidence-unavailable", "task-unavailable"
	ws, bare := setupEvidenceOnly(t, f, id, task, false)
	progress := ProgressEntry{At: testNow, Phase: phaseTool, Message: `Bash({"command":"go test ./..."})`, Tools: []string{"Bash", "go", "test"}}
	must(t, f.fx.AppendProgress(id, progress))
	must(t, f.mon.Finalize(context.Background(), Completion{ExecutorID: id, Kind: OutcomeSucceeded, Output: okOutput(id), Status: doneStatus(id)}))
	got := f.wb.reports[0]
	if got.Kind != OutcomeCrashed || !got.Retryable || got.Error == nil || got.Error.Kind != "non_delivery" {
		t.Fatalf("missing command events must be non-delivery, got %+v", got)
	}
	if got.Evidence == nil || got.Evidence.CommandsAvailable || got.Evidence.CommandsUnavailableReason == "" || got.Evidence.CommandEventDigest == "" {
		t.Fatalf("artifact must explicitly mark commands unavailable with digest: %+v", got.Evidence)
	}
	if len(got.Evidence.Commands) != 0 {
		t.Fatalf("must not fall back to RunnerCmd as commands: %+v", got.Evidence.Commands)
	}
	b, err := os.ReadFile(filepath.Join(ws, filepath.FromSlash(got.Evidence.Path)))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(b), "codex exec") || strings.Contains(string(b), `"pushed"`) {
		t.Fatalf("artifact must not persist runner argv or pushed field: %s", b)
	}
	if !gitRefExists(f.git, bare, "refs/heads/ac-exec/"+task+"/"+id) {
		t.Fatal("unavailable evidence artifact should still be pushed for audit")
	}
}

func TestEvidenceOnlyFinalizeRetryDoesNotDuplicateCommit(t *testing.T) {
	f := newFinalizeGateFixture(t)
	id, task := "exec-evidence-retry", "task-retry"
	ws, _ := setupEvidenceOnly(t, f, id, task, false)
	writeEvidenceCommandEvent(t, f.fx, id, "cmd-retry", "go test ./...", 0)
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
