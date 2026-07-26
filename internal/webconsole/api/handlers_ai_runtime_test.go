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
	"gopkg.in/yaml.v3"
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

	resp := orgScopedGet(t, server.URL+"/api/ai-runtime/export?format=json", member)
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

	payload, _ := json.Marshal(airuntime.PreviewRequest{
		Strategy: airuntime.StrategyCreate, Document: doc,
	})
	resp = orgScopedPost(t, server.URL+"/api/ai-runtime/import/preview", string(payload), member)
	if resp.StatusCode != http.StatusForbidden {
		t.Fatalf("member preview status=%d want 403", resp.StatusCode)
	}
	resp.Body.Close()
	resp = orgScopedPost(t, server.URL+"/api/ai-runtime/import/preview", string(payload), owner)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("owner preview status=%d", resp.StatusCode)
	}
	resp.Body.Close()

	doc.Runtime.CLIs = doc.Runtime.CLIs[:1]
	payload, _ = json.Marshal(airuntime.PreviewRequest{
		Strategy: airuntime.StrategyReplace, Document: doc,
	})
	resp = orgScopedPost(t, server.URL+"/api/ai-runtime/import/preview", string(payload), owner)
	if resp.StatusCode != http.StatusOK {
		var failure any
		_ = json.NewDecoder(resp.Body).Decode(&failure)
		t.Fatalf("owner replace preview status=%d body=%+v", resp.StatusCode, failure)
	}
	var replacePreview airuntime.PreviewResponse
	if err := json.NewDecoder(resp.Body).Decode(&replacePreview); err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	payload, _ = json.Marshal(airuntime.ApplyRequest{
		Strategy: airuntime.StrategyReplace, Document: doc, ValidationToken: replacePreview.ValidationToken,
	})
	resp = orgScopedPost(t, server.URL+"/api/ai-runtime/import/apply", string(payload), owner)
	if resp.StatusCode != http.StatusOK {
		var failure any
		_ = json.NewDecoder(resp.Body).Decode(&failure)
		t.Fatalf("owner replace apply status=%d body=%+v", resp.StatusCode, failure)
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
	req, _ := http.NewRequest(http.MethodGet, server.URL+"/api/orgs/"+other.Slug()+"/ai-runtime/export?format=json", nil)
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

func TestAIRuntimePreviewApplyAndExportFormatsHTTP(t *testing.T) {
	deps, db := setupAPIWithAuth(t)
	deps.RuntimeCatalog = airuntime.NewServiceWithValidationKey(
		airuntimesql.NewRepository(db),
		func() string { return "runtime-http-contract" },
		[]byte("0123456789abcdef0123456789abcdef"),
	)
	owner := setupTestSession(t, db, deps)
	server := newTestServer(t, deps)
	defer server.Close()

	resp := orgScopedGet(t, server.URL+"/api/ai-runtime/export", owner)
	if resp.StatusCode != http.StatusOK || !strings.HasPrefix(resp.Header.Get("Content-Type"), "application/yaml") {
		t.Fatalf("default export status=%d content-type=%q", resp.StatusCode, resp.Header.Get("Content-Type"))
	}
	var yamlDoc map[string]any
	if err := yaml.NewDecoder(resp.Body).Decode(&yamlDoc); err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if yamlDoc["schema_version"] != 1 {
		t.Fatalf("yaml export=%+v", yamlDoc)
	}

	resp = orgScopedGet(t, server.URL+"/api/ai-runtime/export?format=json&scope=cli&cli_keys=codex", owner)
	if resp.StatusCode != http.StatusOK || !strings.HasPrefix(resp.Header.Get("Content-Type"), "application/json") {
		t.Fatalf("json export status=%d content-type=%q", resp.StatusCode, resp.Header.Get("Content-Type"))
	}
	var doc airuntime.ExportDocument
	if err := json.NewDecoder(resp.Body).Decode(&doc); err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if len(doc.Runtime.CLIs) != 1 || doc.Runtime.CLIs[0].Key != "codex" || len(doc.Runtime.Models) != 0 {
		t.Fatalf("scoped export=%+v", doc.Runtime)
	}

	docJSON, _ := json.Marshal(doc)
	var yamlDocument any
	_ = json.Unmarshal(docJSON, &yamlDocument)
	previewPayload, err := yaml.Marshal(map[string]any{
		"strategy": "merge",
		"document": yamlDocument,
	})
	if err != nil {
		t.Fatal(err)
	}
	previewURL := orgScopedURL(server.URL+"/api/ai-runtime/import/preview", owner.OrgSlug)
	req, _ := http.NewRequest(http.MethodPost, previewURL, strings.NewReader(string(previewPayload)))
	req.Header.Set("Content-Type", "application/yaml")
	req.AddCookie(owner.Cookie)
	resp, err = http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	if resp.StatusCode != http.StatusOK {
		var body any
		_ = json.NewDecoder(resp.Body).Decode(&body)
		t.Fatalf("preview status=%d body=%+v", resp.StatusCode, body)
	}
	var preview airuntime.PreviewResponse
	if err := json.NewDecoder(resp.Body).Decode(&preview); err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if preview.ValidationToken == "" {
		t.Fatal("preview omitted validation_token")
	}
	resp = orgScopedPost(t, server.URL+"/api/ai-runtime/import", `{}`, owner)
	if resp.StatusCode != http.StatusNotFound {
		t.Fatalf("combined import endpoint remains public: status=%d", resp.StatusCode)
	}
	resp.Body.Close()

	applyPayload, _ := json.Marshal(airuntime.ApplyRequest{
		Strategy: airuntime.StrategyMerge, Document: doc, ValidationToken: preview.ValidationToken,
	})
	resp = orgScopedPost(t, server.URL+"/api/ai-runtime/import/apply", string(applyPayload), owner)
	if resp.StatusCode != http.StatusOK {
		var body any
		_ = json.NewDecoder(resp.Body).Decode(&body)
		t.Fatalf("apply status=%d body=%+v", resp.StatusCode, body)
	}
	resp.Body.Close()

	var apply map[string]any
	_ = json.Unmarshal(applyPayload, &apply)
	apply["validation_token"] = preview.ValidationToken + "tampered"
	tampered, _ := json.Marshal(apply)
	resp = orgScopedPost(t, server.URL+"/api/ai-runtime/import/apply", string(tampered), owner)
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("tampered apply status=%d", resp.StatusCode)
	}
	resp.Body.Close()
}
