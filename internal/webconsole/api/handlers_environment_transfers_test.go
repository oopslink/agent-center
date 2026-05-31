package api

import (
	"context"
	"database/sql"
	"encoding/json"
	"net/http"
	"testing"
	"time"

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
