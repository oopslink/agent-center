package api

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"testing"

	"github.com/oopslink/agent-center/internal/airuntime"
	airuntimesql "github.com/oopslink/agent-center/internal/airuntime/sqlite"
	"github.com/oopslink/agent-center/internal/identity"
)

func TestAIRuntimeCatalogHTTPFlowAndPermissions(t *testing.T) {
	deps, db := setupAPIWithAuth(t)
	n := 0
	deps.RuntimeCatalog = airuntime.NewService(airuntimesql.NewRepository(db), func() string { n++; return fmt.Sprintf("runtime-%d", n) })
	owner := setupTestSession(t, db, deps)
	server := newTestServer(t, deps)
	defer server.Close()

	resp := orgScopedPost(t, server.URL+"/api/ai-runtime/models", `{"expected_revision":0,"value":{"key":"gpt-5","model_key":"gpt-5","display_name":"GPT-5","compatible_cli_keys":["codex"],"default_parameters":{},"enabled":true}}`, owner)
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("create model status=%d", resp.StatusCode)
	}
	var modelResult struct {
		Revision int64                     `json:"revision"`
		Entry    airuntime.ModelDefinition `json:"entry"`
	}
	_ = json.NewDecoder(resp.Body).Decode(&modelResult)
	resp.Body.Close()

	resp = orgScopedPost(t, server.URL+"/api/ai-runtime/profiles", fmt.Sprintf(`{"expected_revision":%d,"value":{"key":"default-coding","name":"Default coding","cli_key":"codex","model_key":"%s","parameters":{},"enabled":true}}`, modelResult.Revision, modelResult.Entry.Key), owner)
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("create profile status=%d", resp.StatusCode)
	}
	var profileResult struct {
		Revision int64                    `json:"revision"`
		Entry    airuntime.RuntimeProfile `json:"entry"`
	}
	_ = json.NewDecoder(resp.Body).Decode(&profileResult)
	resp.Body.Close()

	url := orgScopedURL(server.URL+"/api/ai-runtime/default-profile", owner.OrgSlug)
	req, _ := http.NewRequest(http.MethodPut, url, strings.NewReader(fmt.Sprintf(`{"expected_revision":%d,"profile_id":"%s"}`, profileResult.Revision, profileResult.Entry.ID)))
	req.Header.Set("Content-Type", "application/json")
	req.AddCookie(owner.Cookie)
	resp, _ = http.DefaultClient.Do(req)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("set default status=%d", resp.StatusCode)
	}
	resp.Body.Close()

	member := memberSessionInOrg(t, db, owner.OrgID, owner.OrgSlug)
	resp = orgScopedGet(t, server.URL+"/api/ai-runtime", member)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("member read status=%d", resp.StatusCode)
	}
	resp.Body.Close()
	resp = orgScopedPost(t, server.URL+"/api/ai-runtime/clis", `{"expected_revision":3,"value":{"key":"custom","display_name":"Custom","executable":"custom","enabled":true}}`, member)
	if resp.StatusCode != http.StatusForbidden {
		t.Fatalf("member write status=%d want 403", resp.StatusCode)
	}
	resp.Body.Close()
}

func TestAIRuntimeBulkHTTPAuthorizationAndOrgIsolation(t *testing.T) {
	deps, db := setupAPIWithAuth(t)
	n := 0
	deps.RuntimeCatalog = airuntime.NewService(airuntimesql.NewRepository(db), func() string {
		n++
		return fmt.Sprintf("runtime-bulk-%d", n)
	})
	owner := setupTestSession(t, db, deps)
	member := memberSessionInOrg(t, db, owner.OrgID, owner.OrgSlug)
	server := newTestServer(t, deps)
	defer server.Close()

	resp := orgScopedGet(t, server.URL+"/api/ai-runtime/export", member)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("member export status=%d", resp.StatusCode)
	}
	var doc airuntime.ExportDocument
	if err := json.NewDecoder(resp.Body).Decode(&doc); err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if doc.Kind != "agent-center-ai-runtime" || doc.SchemaVersion != 1 || doc.ExportedAt.IsZero() {
		t.Fatalf("export contract = %+v", doc)
	}

	payload, _ := json.Marshal(airuntime.ImportRequest{
		ExpectedRevision: 0, DryRun: true, Strategy: airuntime.StrategyCreate, Document: doc,
	})
	resp = orgScopedPost(t, server.URL+"/api/ai-runtime/import", string(payload), member)
	if resp.StatusCode != http.StatusForbidden {
		t.Fatalf("member import status=%d want 403", resp.StatusCode)
	}
	resp.Body.Close()
	resp = orgScopedPost(t, server.URL+"/api/ai-runtime/import", string(payload), owner)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("owner dry-run import status=%d", resp.StatusCode)
	}
	resp.Body.Close()

	doc.Runtime.CLIs = doc.Runtime.CLIs[:1]
	payload, _ = json.Marshal(airuntime.ImportRequest{
		ExpectedRevision: 0, Strategy: airuntime.StrategyReplace, Document: doc,
	})
	resp = orgScopedPost(t, server.URL+"/api/ai-runtime/import", string(payload), owner)
	if resp.StatusCode != http.StatusOK {
		var failure any
		_ = json.NewDecoder(resp.Body).Decode(&failure)
		t.Fatalf("owner replace import status=%d body=%+v", resp.StatusCode, failure)
	}
	var replaceReport airuntime.ImportReport
	if err := json.NewDecoder(resp.Body).Decode(&replaceReport); err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if !replaceReport.Applied {
		t.Fatalf("replace report=%+v", replaceReport)
	}
	ownerCatalog, err := deps.RuntimeCatalog.Catalog(context.Background(), owner.OrgID)
	if err != nil {
		t.Fatal(err)
	}
	enabled := 0
	for _, cli := range ownerCatalog.CLIs {
		if cli.Enabled {
			enabled++
		}
	}
	if enabled != 1 {
		t.Fatalf("replace did not disable omitted CLI: %+v", ownerCatalog.CLIs)
	}

	other, err := identity.OrganizationFactory{}.New("runtime-other", "Runtime Other", owner.IdentityID)
	if err != nil {
		t.Fatal(err)
	}
	if err := identity.NewSQLiteOrganizationRepo(db).Save(context.Background(), other); err != nil {
		t.Fatal(err)
	}
	req, _ := http.NewRequest(http.MethodGet, server.URL+"/api/orgs/"+other.Slug()+"/ai-runtime/export", nil)
	req.AddCookie(owner.Cookie)
	resp, err = http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	if resp.StatusCode != http.StatusForbidden {
		t.Fatalf("cross-org export status=%d want 403", resp.StatusCode)
	}
	resp.Body.Close()
	otherCatalog, err := deps.RuntimeCatalog.Catalog(context.Background(), string(other.ID()))
	if err != nil {
		t.Fatal(err)
	}
	for _, cli := range otherCatalog.CLIs {
		if !cli.Enabled {
			t.Fatalf("replace crossed org boundary: %+v", otherCatalog.CLIs)
		}
	}
}
