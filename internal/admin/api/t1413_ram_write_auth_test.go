package api

import (
	"net/http"
	"testing"

	authz "github.com/oopslink/agent-center/internal/authorization"
)

func TestT1413AgentCreateTaskEnforceUsesSharedAuthorization(t *testing.T) {
	f := newWriteToolsFixture(t)
	f.addWorkerToken(t, "acat_w1", atWorker1)
	projectID, _ := f.seedMemberProject(t)
	f.deps.Authorizer = authz.New(authz.Deps{DB: f.db, Mode: authz.EnforcementEnforce})
	_, err := f.db.Exec(`DELETE FROM authorization_role_permissions WHERE role_id='sys-project-member' AND permission_key='project.write'`)
	if err != nil {
		t.Fatal(err)
	}
	srv := f.server(t)

	status, body := postBearer(t, srv.URL, "/admin/agent-tools/create_task", "acat_w1", map[string]any{
		"agent_id":   atAgent1,
		"project_id": string(projectID),
		"title":      "blocked by RAM",
	})
	if status != http.StatusForbidden || body["error"] != "permission_denied" {
		t.Fatalf("create_task status=%d body=%v, want shared authz 403", status, body)
	}
}
