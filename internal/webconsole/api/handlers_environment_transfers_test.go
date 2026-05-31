package api

import (
	"context"
	"database/sql"
	"encoding/json"
	"net/http"
	"testing"
	"time"

	agentbc "github.com/oopslink/agent-center/internal/agent"
	agentsql "github.com/oopslink/agent-center/internal/agent/sqlite"
	"github.com/oopslink/agent-center/internal/files"
	filessql "github.com/oopslink/agent-center/internal/files/sqlite"
	"github.com/oopslink/agent-center/internal/idgen"
	pm "github.com/oopslink/agent-center/internal/projectmanager"
	pmservice "github.com/oopslink/agent-center/internal/projectmanager/service"
)

// saveTransferSession seeds an OPEN upload session with the given scope/scope_id,
// created at `at` with `ttl` (so expiresAt = at+ttl controls liveness).
func saveTransferSession(t *testing.T, db *sql.DB, scope files.FileScope, scopeID string, at time.Time, ttl time.Duration) string {
	t.Helper()
	s, err := files.NewUploadSession(files.NewUploadInput{
		FileULID:    idgen.MustNewULID(),
		SessionULID: idgen.MustNewULID(),
		ContentType: "text/plain",
		Size:        10,
		Scope:       scope,
		ScopeID:     scopeID,
		CreatedBy:   "user:x",
		CreatedAt:   at,
		TTL:         ttl,
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := filessql.NewFileTransferSessionRepo(db).Save(context.Background(), s); err != nil {
		t.Fatal(err)
	}
	return s.ID()
}

func seedPMProject(t *testing.T, deps HandlerDeps, orgID, name string) pm.ProjectID {
	t.Helper()
	pid, err := deps.PM.CreateProject(context.Background(), pmservice.CreateProjectCommand{
		OrganizationID: orgID, Name: name, CreatedBy: pm.IdentityRef("user:hayang"),
	})
	if err != nil {
		t.Fatal(err)
	}
	return pid
}

// TestAPI_Transfers_OrgScopedFailClosed: GET /api/files/transfers returns ONLY the
// caller org's in-flight sessions, resolving each session's scope→org fail-closed.
// Cross-org / tmp / expired / unknown-scope sessions are all EXCLUDED (no leak).
func TestAPI_Transfers_OrgScopedFailClosed(t *testing.T) {
	deps, db := setupAPIWithAuth(t)
	deps.FileTransferRepo = filessql.NewFileTransferSessionRepo(db)
	sess := setupTestSession(t, db, deps)
	s := newTestServer(t, deps)
	defer s.Close()

	now := time.Now().UTC()
	pidMine := seedPMProject(t, deps, sess.OrgID, "mine")
	pidOther := seedPMProject(t, deps, "org-other", "other")

	// (1) project-scoped, my org, live → INCLUDED
	mine := saveTransferSession(t, db, files.ScopeProject, string(pidMine), now, time.Hour)
	// (2) project-scoped, OTHER org, live → excluded (cross-org)
	saveTransferSession(t, db, files.ScopeProject, string(pidOther), now, time.Hour)
	// (3) tmp-scoped → excluded (not org-resolvable)
	saveTransferSession(t, db, files.ScopeTmp, "", now, time.Hour)
	// (4) project-scoped, my org, EXPIRED → excluded (ListOpen drops expired)
	saveTransferSession(t, db, files.ScopeProject, string(pidMine), now.Add(-2*time.Hour), time.Hour)
	// (5) project-scoped, UNKNOWN project id → excluded (fail-closed)
	saveTransferSession(t, db, files.ScopeProject, "proj-nonexistent", now, time.Hour)

	resp := orgScopedGet(t, s.URL+"/api/files/transfers", sess)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("list: got %d", resp.StatusCode)
	}
	var out struct {
		TransferSessions []map[string]any `json:"transfer_sessions"`
	}
	_ = json.NewDecoder(resp.Body).Decode(&out)
	if len(out.TransferSessions) != 1 {
		t.Fatalf("got %d sessions, want 1 (only my-org live project session); cross-org/tmp/expired/unknown must be excluded: %+v", len(out.TransferSessions), out.TransferSessions)
	}
	if out.TransferSessions[0]["id"] != mine {
		t.Fatalf("got session %v, want %s", out.TransferSessions[0]["id"], mine)
	}
	if out.TransferSessions[0]["scope"] != "project" || out.TransferSessions[0]["status"] != "open" {
		t.Fatalf("session shape wrong: %+v", out.TransferSessions[0])
	}
}

// TestAPI_Transfers_ConversationScope: a conversation-scoped session resolves org
// via the conversation (org match → included; the harness conversation is in the
// caller org).
func TestAPI_Transfers_ConversationScope(t *testing.T) {
	deps, db := setupAPIWithAuth(t)
	deps.FileTransferRepo = filessql.NewFileTransferSessionRepo(db)
	sess := setupTestSession(t, db, deps)
	s := newTestServer(t, deps)
	defer s.Close()

	convID := seedOrgChannel(t, deps, sess.OrgID, "transfers-conv")
	now := time.Now().UTC()
	mine := saveTransferSession(t, db, files.ScopeConversation, convID, now, time.Hour)

	resp := orgScopedGet(t, s.URL+"/api/files/transfers", sess)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("list: got %d", resp.StatusCode)
	}
	var out struct {
		TransferSessions []map[string]any `json:"transfer_sessions"`
	}
	_ = json.NewDecoder(resp.Body).Decode(&out)
	if len(out.TransferSessions) != 1 || out.TransferSessions[0]["id"] != mine {
		t.Fatalf("conversation-scoped session not resolved to caller org: %+v", out.TransferSessions)
	}
}

// TestAPI_Transfers_NotWired: 501 when FileTransferRepo is not wired.
func TestAPI_Transfers_NotWired(t *testing.T) {
	deps, db := setupAPIWithAuth(t)
	sess := setupTestSession(t, db, deps)
	s := newTestServer(t, deps)
	defer s.Close()

	resp := orgScopedGet(t, s.URL+"/api/files/transfers", sess)
	if resp.StatusCode != http.StatusNotImplemented {
		t.Fatalf("not-wired: got %d, want 501", resp.StatusCode)
	}
}

// saveAgentInOrg seeds an agent.Agent in orgID directly via the sqlite repo so the
// agent-scope transfer-session resolution (AgentSvc.GetAgent→OrganizationID) has a
// target. Bypasses CreateAgent's worker-in-org check (not under test here).
func saveAgentInOrg(t *testing.T, db *sql.DB, orgID, agentID string) {
	t.Helper()
	a, err := agentbc.NewAgent(agentbc.NewAgentInput{
		ID:             agentbc.AgentID(agentID),
		OrganizationID: orgID,
		Profile:        agentbc.Profile{Name: agentID, Model: "claude", CLI: "claudecode"},
		WorkerID:       "w-x",
		CreatedBy:      agentbc.IdentityRef("user:hayang"),
		CreatedAt:      time.Date(2026, 5, 24, 0, 0, 0, 0, time.UTC),
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := agentsql.NewAgentRepo(db).Save(context.Background(), a); err != nil {
		t.Fatal(err)
	}
}

// TestAPI_Transfers_AgentTaskIssueScope_OrgScoped gives EXECUTED fail-closed
// assertions for the agent / task / issue scopes (PD REQUIRED follow-up — the
// agent scope is the security boundary). For each, an own-org session is INCLUDED
// and a cross-org (or unknown) one is EXCLUDED, exercising transferSessionOrg's
// agent→org, task→project→org, issue→project→org paths.
func TestAPI_Transfers_AgentTaskIssueScope_OrgScoped(t *testing.T) {
	deps, db := setupAPIWithAuth(t)
	deps.FileTransferRepo = filessql.NewFileTransferSessionRepo(db)
	sess := setupTestSession(t, db, deps)
	s := newTestServer(t, deps)
	defer s.Close()

	now := time.Now().UTC()
	ctx := context.Background()
	caller := pm.IdentityRef("user:hayang")

	// agent scope: own-org agent (include), other-org agent (exclude), unknown (exclude).
	saveAgentInOrg(t, db, sess.OrgID, "agent-mine")
	saveAgentInOrg(t, db, "org-other", "agent-other")
	mineAgent := saveTransferSession(t, db, files.ScopeAgent, "agent-mine", now, time.Hour)
	saveTransferSession(t, db, files.ScopeAgent, "agent-other", now, time.Hour)
	saveTransferSession(t, db, files.ScopeAgent, "agent-unknown", now, time.Hour)

	// task scope: task in my-org project (include) vs other-org project (exclude).
	pidMine := seedPMProject(t, deps, sess.OrgID, "mine-proj")
	pidOther := seedPMProject(t, deps, "org-other", "other-proj")
	taskMine, err := deps.PM.CreateTask(ctx, pmservice.CreateTaskCommand{ProjectID: pidMine, Title: "tm", CreatedBy: caller})
	if err != nil {
		t.Fatal(err)
	}
	taskOther, err := deps.PM.CreateTask(ctx, pmservice.CreateTaskCommand{ProjectID: pidOther, Title: "to", CreatedBy: caller})
	if err != nil {
		t.Fatal(err)
	}
	mineTask := saveTransferSession(t, db, files.ScopeTask, string(taskMine), now, time.Hour)
	saveTransferSession(t, db, files.ScopeTask, string(taskOther), now, time.Hour)

	// issue scope: issue in my-org project (include) vs other-org project (exclude).
	issueMine, err := deps.PM.CreateIssue(ctx, pmservice.CreateIssueCommand{ProjectID: pidMine, Title: "im", CreatedBy: caller})
	if err != nil {
		t.Fatal(err)
	}
	issueOther, err := deps.PM.CreateIssue(ctx, pmservice.CreateIssueCommand{ProjectID: pidOther, Title: "io", CreatedBy: caller})
	if err != nil {
		t.Fatal(err)
	}
	mineIssue := saveTransferSession(t, db, files.ScopeIssue, string(issueMine), now, time.Hour)
	saveTransferSession(t, db, files.ScopeIssue, string(issueOther), now, time.Hour)

	resp := orgScopedGet(t, s.URL+"/api/files/transfers", sess)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("list: got %d", resp.StatusCode)
	}
	var out struct {
		TransferSessions []map[string]any `json:"transfer_sessions"`
	}
	_ = json.NewDecoder(resp.Body).Decode(&out)

	got := map[string]bool{}
	for _, ts := range out.TransferSessions {
		got[ts["id"].(string)] = true
	}
	// Own-org agent/task/issue sessions INCLUDED.
	for _, want := range []string{mineAgent, mineTask, mineIssue} {
		if !got[want] {
			t.Fatalf("expected own-org session %s included; got %+v", want, out.TransferSessions)
		}
	}
	// Exactly 3 — every cross-org and unknown session excluded (fail-closed).
	if len(out.TransferSessions) != 3 {
		t.Fatalf("got %d sessions, want exactly 3 own-org (agent/task/issue cross-org+unknown must be excluded): %+v", len(out.TransferSessions), out.TransferSessions)
	}
}
