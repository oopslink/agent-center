package api

import (
	"bytes"
	"context"
	"net/http"
	"testing"
	"time"

	authz "github.com/oopslink/agent-center/internal/authorization"
	"github.com/oopslink/agent-center/internal/conversation"
	"github.com/oopslink/agent-center/internal/files"
	filesservice "github.com/oopslink/agent-center/internal/files/service"
	pm "github.com/oopslink/agent-center/internal/projectmanager"
	pmservice "github.com/oopslink/agent-center/internal/projectmanager/service"
)

func seedTeamRAMProjectRead(t *testing.T, f *writeToolsFixture, projectID pm.ProjectID) {
	t.Helper()
	now := time.Now().UTC().Format(time.RFC3339Nano)
	if _, err := f.db.Exec(`
		INSERT INTO authorization_roles (id, org_id, kind, name, description, created_by, created_at, updated_at, version)
		VALUES ('role-t1416-project-reader', ?, 'custom', 'T1416 project reader', '', 'system', ?, ?, 1)`,
		atTestOrg, now, now); err != nil {
		t.Fatal(err)
	}
	if _, err := f.db.Exec(`
		INSERT INTO authorization_role_permissions (role_id, permission_key, resource_kind, delegatable, created_at)
		VALUES ('role-t1416-project-reader', 'project.read', 'project', 0, ?)`, now); err != nil {
		t.Fatal(err)
	}
	if _, err := f.db.Exec(`
		INSERT INTO teams (id, org_id, name, description, created_at, updated_at, version)
		VALUES ('team-t1416-read', ?, 'T1416 read team', '', ?, ?, 1)`, atTestOrg, now, now); err != nil {
		t.Fatal(err)
	}
	if _, err := f.db.Exec(`
		INSERT INTO team_roles (team_id, role, cli, model, capability_tags, max_concurrency, created_at, access_requirements)
		VALUES ('team-t1416-read', 'reader', '', '', '[]', 1, ?, '["project.read"]')`, now); err != nil {
		t.Fatal(err)
	}
	if _, err := f.db.Exec(`
		INSERT INTO team_members (team_id, member_ref, member_kind, role, created_at)
		VALUES ('team-t1416-read', ?, 'agent', 'reader', ?)`, "agent:"+atAgent1, now); err != nil {
		t.Fatal(err)
	}
	if _, err := f.db.Exec(`
		INSERT INTO team_projects (team_id, project_id, created_at)
		VALUES ('team-t1416-read', ?, ?)`, string(projectID), now); err != nil {
		t.Fatal(err)
	}
	if _, err := f.db.Exec(`
		INSERT INTO team_role_ram_role_mappings (team_id, team_role, ram_role_id, created_at, created_by)
		VALUES ('team-t1416-read', 'reader', 'role-t1416-project-reader', ?, 'system')`, now); err != nil {
		t.Fatal(err)
	}
	if _, err := f.db.Exec(`
		INSERT INTO team_role_ram_role_versions (team_id, team_role, version, updated_at, updated_by)
		VALUES ('team-t1416-read', 'reader', 1, ?, 'system')`, now); err != nil {
		t.Fatal(err)
	}
}

func seedUnassignedTaskConversation(t *testing.T, f *writeToolsFixture) (pm.ProjectID, string, string) {
	t.Helper()
	ctx := context.Background()
	pid, err := f.pmSvc.CreateProject(ctx, pmservice.CreateProjectCommand{
		OrganizationID: atTestOrg, Name: "T1416 Project", CreatedBy: pm.IdentityRef("user:owner"),
	})
	if err != nil {
		t.Fatal(err)
	}
	tid, err := f.pmSvc.CreateTask(ctx, pmservice.CreateTaskCommand{
		ProjectID: pid, Title: "T1416 task", CreatedBy: pm.IdentityRef("user:owner"),
	})
	if err != nil {
		t.Fatal(err)
	}
	f.drain(t)
	conv, err := f.convRepo.FindByOwnerRef(ctx, conversation.NewTaskOwnerRef(string(tid)))
	if err != nil {
		t.Fatal(err)
	}
	return pid, string(tid), string(conv.ID())
}

func TestT1416ListMessages_EnforceAllowsTeamRAMProjectRead(t *testing.T) {
	f := newWriteToolsFixture(t)
	f.addWorkerToken(t, "acat_w1", atWorker1)
	projectID, _, convID := seedUnassignedTaskConversation(t, f)
	seedTeamRAMProjectRead(t, f, projectID)
	appendMsg(t, f, convID, "user:owner", "RAM visible")
	f.deps.Authorizer = authz.New(authz.Deps{DB: f.db, Mode: authz.EnforcementEnforce})
	srv := f.server(t)

	status, body := postBearer(t, srv.URL, "/admin/agent-tools/list_messages", "acat_w1",
		map[string]any{"agent_id": atAgent1, "conversation_id": convID})
	if status != http.StatusOK {
		t.Fatalf("list_messages status=%d body=%v, want RAM-authorized read", status, body)
	}
	msgs, _ := body["messages"].([]any)
	if len(msgs) != 1 || msgs[0].(map[string]any)["content"] != "RAM visible" {
		t.Fatalf("messages=%v", body["messages"])
	}
}

func TestT1416FileDownload_EnforceAllowsTeamRAMConversationRead(t *testing.T) {
	f := newWriteToolsFixture(t)
	f.addWorkerToken(t, "acat_w1", atWorker1)
	projectID, _, convID := seedUnassignedTaskConversation(t, f)
	seedTeamRAMProjectRead(t, f, projectID)
	f.attachAgentFilesSvc(t)
	f.deps.Authorizer = authz.New(authz.Deps{DB: f.db, Mode: authz.EnforcementEnforce})
	srv := f.filesServer(t)

	content := []byte("RAM file content")
	ulid := uploadViaAgent(t, srv.URL, "acat_w1", atAgent1, "", "", content)
	uri, _ := files.NewFileURI(ulid)
	if _, err := f.deps.FilesSvc.AddReference(context.Background(), filesservice.AddReferenceCmd{
		FileURI: uri, Scope: files.ScopeConversation, ScopeID: convID,
		MimeType: "text/plain", SizeBytes: int64(len(content)), CreatedBy: "user:owner",
	}); err != nil {
		t.Fatal(err)
	}

	status, _, body := getRawBearer(t, srv.URL, "/admin/files/"+ulid+"?agent_id="+atAgent1, "acat_w1")
	if status != http.StatusOK {
		t.Fatalf("download status=%d body=%s, want RAM-authorized file read", status, body)
	}
	if !bytes.Equal(body, content) {
		t.Fatalf("download body=%q want %q", body, content)
	}
}
