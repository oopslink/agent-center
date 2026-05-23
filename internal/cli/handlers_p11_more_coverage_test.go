package cli

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/oopslink/agent-center/internal/observability"
	"github.com/oopslink/agent-center/internal/secretmgmt"
	"github.com/oopslink/agent-center/internal/taskruntime"
	"github.com/oopslink/agent-center/internal/taskruntime/execution"
	"github.com/oopslink/agent-center/internal/taskruntime/inputrequest"
	trservice "github.com/oopslink/agent-center/internal/taskruntime/service"
)

// ============================================================================
// projection helpers — irToMap / secretToMap / valid kind / trim
// ============================================================================

func TestIRToMap_Responded(t *testing.T) {
	now := time.Now().UTC()
	ir, err := inputrequest.New(inputrequest.NewInput{
		ID: "IR-1", TaskExecutionID: "E-1",
		Question: "q?", Options: []string{"yes", "no"},
		Urgency: inputrequest.UrgencyNormal, Now: now,
	})
	if err != nil {
		t.Fatal(err)
	}
	_ = ir.Respond(inputrequest.InputResponse{
		Answer: "yes", DecidedBy: "user:hayang", DecidedAt: now,
	})
	m := irToMap(ir)
	if m["answer"] != "yes" {
		t.Fatalf("answer: %v", m["answer"])
	}
	if m["decided_by"] != "user:hayang" {
		t.Fatalf("decided_by: %v", m["decided_by"])
	}
}

func TestIRToMap_Pending(t *testing.T) {
	now := time.Now().UTC()
	ir, _ := inputrequest.New(inputrequest.NewInput{
		ID: "IR-2", TaskExecutionID: "E-2",
		Question: "q?", Urgency: inputrequest.UrgencyNormal, Now: now,
	})
	m := irToMap(ir)
	if _, ok := m["answer"]; ok {
		t.Fatal("pending IR should not carry answer in projection")
	}
}

func TestSecretToMap_AllFields(t *testing.T) {
	now := time.Now().UTC()
	sec, err := secretmgmt.NewUserSecret(secretmgmt.NewUserSecretInput{
		ID: "S-1", Name: "x", Kind: secretmgmt.UserSecretKindOther,
		Ciphertext: []byte{1, 2, 3}, Nonce: []byte{4, 5, 6},
		CreatedAt: now, CreatedBy: "user:hayang",
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := sec.Revoke(now, "user:hayang", secretmgmt.UserSecretRevokedReasonManual, "done"); err != nil {
		t.Fatal(err)
	}
	m := secretToMap(sec)
	if m["state"] != "revoked" || m["revoked_by"] != "user:hayang" {
		t.Fatalf("got %v", m)
	}
}

func TestSecretToMap_Active(t *testing.T) {
	now := time.Now().UTC()
	sec, _ := secretmgmt.NewUserSecret(secretmgmt.NewUserSecretInput{
		ID: "S-2", Name: "y", Kind: secretmgmt.UserSecretKindMCP,
		Ciphertext: []byte{1}, Nonce: []byte{2},
		CreatedAt: now, CreatedBy: "user:hayang",
	})
	m := secretToMap(sec)
	if _, ok := m["revoked_at"]; ok {
		t.Fatalf("active secret should not show revoked_at: %v", m)
	}
}

func TestValidSecretKind(t *testing.T) {
	for _, k := range []secretmgmt.UserSecretKind{
		secretmgmt.UserSecretKindMCP, secretmgmt.UserSecretKindCloudCredential,
		secretmgmt.UserSecretKindRepoDeployKey, secretmgmt.UserSecretKindOther,
	} {
		if !validSecretKind(k) {
			t.Fatalf("%s should be valid", k)
		}
	}
	if validSecretKind("weird") {
		t.Fatal()
	}
}

func TestTrimTrailingNewline(t *testing.T) {
	if string(trimTrailingNewline([]byte("hello\n"))) != "hello" {
		t.Fatal()
	}
	if string(trimTrailingNewline([]byte("hello\n\n\n"))) != "hello" {
		t.Fatal()
	}
	if string(trimTrailingNewline([]byte("no-newline"))) != "no-newline" {
		t.Fatal()
	}
}

// ============================================================================
// resolveSecretInput / resolveAnswerInput — file paths
// ============================================================================

func TestResolveSecretInput_FromFile(t *testing.T) {
	tmp := writeTempFile(t, "deadbeef\n")
	got, err := resolveSecretInput(tmp)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != "deadbeef" {
		t.Fatalf("got %q", got)
	}
}

func TestResolveSecretInput_EmptyFile(t *testing.T) {
	tmp := writeTempFile(t, "")
	_, err := resolveSecretInput(tmp)
	if err == nil {
		t.Fatal()
	}
}

func TestResolveSecretInput_FileNotFound(t *testing.T) {
	_, err := resolveSecretInput("/no/such/file/asdf")
	if err == nil {
		t.Fatal()
	}
}

func TestResolveAnswerInput_FromFile(t *testing.T) {
	tmp := writeTempFile(t, "yes\n")
	got, err := resolveAnswerInput("", tmp)
	if err != nil {
		t.Fatal(err)
	}
	if got != "yes" {
		t.Fatalf("got %q", got)
	}
}

func TestResolveAnswerInput_FromFlag(t *testing.T) {
	got, err := resolveAnswerInput("the answer", "")
	if err != nil {
		t.Fatal(err)
	}
	if got != "the answer" {
		t.Fatalf("got %q", got)
	}
}

func TestResolveAnswerInput_FileNotFound(t *testing.T) {
	_, err := resolveAnswerInput("", "/no/such/file/qwer")
	if err == nil {
		t.Fatal()
	}
}

func TestResolveAnswerInput_EmptyFile(t *testing.T) {
	tmp := writeTempFile(t, "")
	_, err := resolveAnswerInput("", tmp)
	if err == nil {
		t.Fatal()
	}
}

// ============================================================================
// convShowHandler — happy path
// ============================================================================

func TestCLI_ConvShow_Happy(t *testing.T) {
	app := newTestApp(t)
	_, _, _ = runOn(t, app, "channel", "create", []string{"--name=showme"})
	out, _, _ := runOn(t, app, "channel", "show", []string{"showme", "--format=json"})
	var m map[string]any
	_ = json.Unmarshal([]byte(strings.TrimSpace(out)), &m)
	cid := m["conversation_id"].(string)
	out2, _, code := runOn(t, app, "conversation", "show", []string{cid})
	if code != ExitOK {
		t.Fatalf("code %d", code)
	}
	if !strings.Contains(out2, "showme") {
		t.Fatalf("got %s", out2)
	}
}

func TestCLI_ConvShow_JSON(t *testing.T) {
	app := newTestApp(t)
	_, _, _ = runOn(t, app, "channel", "create", []string{"--name=showmejson"})
	out, _, _ := runOn(t, app, "channel", "show", []string{"showmejson", "--format=json"})
	var m map[string]any
	_ = json.Unmarshal([]byte(strings.TrimSpace(out)), &m)
	cid := m["conversation_id"].(string)
	out2, _, _ := runOn(t, app, "conversation", "show", []string{cid, "--format=json"})
	var m2 map[string]any
	_ = json.Unmarshal([]byte(strings.TrimSpace(out2)), &m2)
	if m2["name"] != "showmejson" {
		t.Fatalf("got %v", m2)
	}
}

// ============================================================================
// irShow — happy path via seeded IR
// ============================================================================

func TestCLI_IRShow_Happy(t *testing.T) {
	app := newTestApp(t)
	now := app.Clock.Now()
	exec, _ := execution.New(execution.NewInput{
		ID: taskruntime.TaskExecutionID("E-IR1"), TaskID: "T-1",
		WorkerID: "W-1", AgentCLI: "claudecode",
		WorkspaceMode: execution.WorkspaceWorktree, Now: now,
	})
	_ = app.ExecRepo.Save(context.Background(), exec)
	ir, _ := inputrequest.New(inputrequest.NewInput{
		ID: "IR-S1", TaskExecutionID: "E-IR1",
		Question: "go?", Options: []string{"yes", "no"},
		Urgency: inputrequest.UrgencyNormal, Now: now,
	})
	_ = app.IRRepo.Save(context.Background(), ir)
	out, _, code := runOn(t, app, "input-request", "show", []string{"IR-S1"})
	if code != ExitOK {
		t.Fatalf("code %d out=%s", code, out)
	}
	if !strings.Contains(out, "go?") {
		t.Fatalf("got %s", out)
	}
}

func TestCLI_IRShow_JSON_Responded(t *testing.T) {
	app := newTestApp(t)
	now := app.Clock.Now()
	exec, _ := execution.New(execution.NewInput{
		ID: "E-IR2", TaskID: "T-1", WorkerID: "W-1", AgentCLI: "claudecode",
		WorkspaceMode: execution.WorkspaceWorktree, Now: now,
	})
	_ = exec.StartWorking("/tmp/wt", now)
	_ = app.ExecRepo.Save(context.Background(), exec)
	ir, _ := inputrequest.New(inputrequest.NewInput{
		ID: "IR-S2", TaskExecutionID: "E-IR2",
		Question: "q?", Urgency: inputrequest.UrgencyNormal, Now: now,
	})
	_ = app.IRRepo.Save(context.Background(), ir)
	_ = exec.EnterInputRequired(ir.ID(), now)
	_ = app.ExecRepo.Update(context.Background(), exec)
	_ = app.IRSvc.Respond(context.Background(), trservice.RespondInput{
		InputRequestID: "IR-S2",
		Answer:         "yes",
		DecidedBy:      "user:hayang",
		Actor:          observability.Actor("user:hayang"),
	})
	out, _, code := runOn(t, app, "input-request", "show", []string{"IR-S2", "--format=json"})
	if code != ExitOK {
		t.Fatalf("code %d out=%s", code, out)
	}
	var m map[string]any
	_ = json.Unmarshal([]byte(strings.TrimSpace(out)), &m)
	if m["answer"] != "yes" {
		t.Fatalf("got %v", m)
	}
}

// ============================================================================
// webconsole_wiring buildWebConsoleHandler
// ============================================================================

func TestBuildWebConsoleHandler_Happy(t *testing.T) {
	app := newTestApp(t)
	h := buildWebConsoleHandler(app, nil)
	if h == nil {
		t.Fatal()
	}
}

func TestBuildWebConsoleHandler_NilApp(t *testing.T) {
	if buildWebConsoleHandler(nil, nil) != nil {
		t.Fatal()
	}
}

// ============================================================================
// GlobalConfigPath roundtrip
// ============================================================================

func TestGlobalConfigPath_RoundTrip(t *testing.T) {
	SetGlobalConfigPath("/tmp/x")
	defer SetGlobalConfigPath("")
	if GlobalConfigPath() != "/tmp/x" {
		t.Fatal()
	}
}
