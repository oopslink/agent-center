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

func TestAIRuntimeCleanupPreflightFailsClosedAndAllowsCompleteEvidence(t *testing.T) {
	deps, db := setupAPIWithAuth(t)
	deps.RuntimeCatalog = airuntime.NewService(airuntimesql.NewRepository(db), func() string { return "runtime" })
	owner := setupTestSession(t, db, deps)
	server := newTestServer(t, deps)
	defer server.Close()

	resp := orgScopedPost(t, server.URL+"/api/ai-runtime/cleanup/preflight", `{}`, owner)
	if resp.StatusCode != http.StatusConflict {
		t.Fatalf("empty evidence status=%d want 409", resp.StatusCode)
	}
	var blocked airuntime.CleanupGateResult
	_ = json.NewDecoder(resp.Body).Decode(&blocked)
	resp.Body.Close()
	if blocked.Allowed || len(blocked.Blockers) == 0 {
		t.Fatalf("empty evidence must fail closed: %+v", blocked)
	}

	body := `{
		"baseline_sha":"36676f14",
		"window_started_at":"2026-08-01T00:00:00Z",
		"window_ended_at":"2026-08-08T00:00:00Z",
		"fallback_samples":[{"object_type":"agent","observed_at":"2026-08-01T00:00:00Z","count":0,"source":"prometheus://release/agent"}],
		"migration_report":{"plan_sha256":"migration-sha","unmapped":[],"summary":{}},
		"migration_report_sha256":"migration-artifact-sha",
		"consumer_audit":{"environment":"production","observed_at":"2026-08-08T00:00:00Z","inventory_source":"gateway-and-tool-audit","legacy_consumer_count":0,"new_path_probe":"GET /api/orgs/acme/ai-runtime/catalog","new_path_reachable":true,"report_sha256":"consumer-report-sha"},
		"isolated_acceptance":{"deployment_id":"isolated-stage6","process_fingerprint":"binary/config","retry":true,"resume":true,"reassign":true,"cancel":true,"historical_execution_readable":true,"snapshot_stable":true,"secret_plaintext_free":true},
		"rollback":{"artifact_sha":"rollback-sha","tested_at":"2026-08-08T01:00:00Z","succeeded":true},
		"owner_confirmed_at":"2026-08-08T02:00:00Z"
	}`
	resp = orgScopedPost(t, server.URL+"/api/ai-runtime/cleanup/preflight", body, owner)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("complete evidence status=%d want 200", resp.StatusCode)
	}
	var allowed airuntime.CleanupGateResult
	_ = json.NewDecoder(resp.Body).Decode(&allowed)
	resp.Body.Close()
	if !allowed.Allowed || allowed.EvidenceSHA256 == "" {
		t.Fatalf("complete evidence must pass: %+v", allowed)
	}
}

func TestAIRuntimeStage4EntrypointsAndImpactPreview(t *testing.T) {
	deps, db, owner := setupTeamsAPI(t)
	n := 0
	deps.RuntimeCatalog = airuntime.NewService(airuntimesql.NewRepository(db), func() string {
		n++
		return fmt.Sprintf("runtime-stage4-%d", n)
	})
	server := newTestServer(t, deps)
	defer server.Close()

	profile := seedRuntimeProfile(t, deps.RuntimeCatalog, owner.OrgID, "user:"+owner.IdentityID)

	resp := orgScopedPost(t, server.URL+"/api/teams", fmt.Sprintf(`{
		"name":"Runtime Team",
		"description":"runtime selection team",
		"visibility":"org-private",
		"roles":[{
			"role":"coder",
			"runtime_selection":{"mode":"profile","profile_id":"%s"},
			"max_concurrency":1,
			"count":1,
			"tags":"go"
		}]
	}`, profile.ID), owner)
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("create team status=%d", resp.StatusCode)
	}
	var teamBody map[string]any
	_ = json.NewDecoder(resp.Body).Decode(&teamBody)
	resp.Body.Close()
	roles, _ := teamBody["roles"].([]any)
	if len(roles) != 1 {
		t.Fatalf("team roles = %v", teamBody["roles"])
	}
	role, _ := roles[0].(map[string]any)
	selection, _ := role["runtime_selection"].(map[string]any)
	if selection["mode"] != airuntime.SelectionProfile || selection["profile_id"] != profile.ID {
		t.Fatalf("team runtime_selection = %+v", selection)
	}
	if role["cli"] != "codex" || role["model"] != "gpt-stage4" {
		t.Fatalf("team runtime mirror = cli %v model %v", role["cli"], role["model"])
	}

	saveWorkerInOrg(t, db, owner.OrgID, "w-stage4")
	resp = orgScopedPost(t, server.URL+"/api/members/agent", fmt.Sprintf(`{
		"display_name":"runtime-coder",
		"model":"gpt-stage4",
		"cli":"codex",
		"worker_id":"w-stage4",
		"max_concurrent_tasks":2,
		"allowed_executors":[{"runtime_selection":{"mode":"profile","profile_id":"%s"}}]
	}`, profile.ID), owner)
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("create agent status=%d", resp.StatusCode)
	}
	var createdAgent map[string]any
	_ = json.NewDecoder(resp.Body).Decode(&createdAgent)
	resp.Body.Close()
	agentID, _ := createdAgent["identity_id"].(string)
	if agentID == "" {
		t.Fatalf("created agent missing identity_id: %+v", createdAgent)
	}
	resp = orgScopedGet(t, server.URL+"/api/agents/"+agentID, owner)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("get agent status=%d", resp.StatusCode)
	}
	var agentBody map[string]any
	_ = json.NewDecoder(resp.Body).Decode(&agentBody)
	resp.Body.Close()
	executors, _ := agentBody["allowed_executors"].([]any)
	if len(executors) != 1 {
		t.Fatalf("allowed_executors = %v", agentBody["allowed_executors"])
	}
	execProfile, _ := executors[0].(map[string]any)
	execSelection, _ := execProfile["runtime_selection"].(map[string]any)
	if execSelection["mode"] != airuntime.SelectionProfile || execSelection["profile_id"] != profile.ID {
		t.Fatalf("executor runtime_selection = %+v", execSelection)
	}
	if execProfile["cli"] != "codex" || execProfile["model"] != "gpt-stage4" {
		t.Fatalf("executor runtime mirror = cli %v model %v", execProfile["cli"], execProfile["model"])
	}

	catalog, err := deps.RuntimeCatalog.Catalog(context.Background(), owner.OrgID)
	if err != nil {
		t.Fatalf("catalog: %v", err)
	}
	url := orgScopedURL(server.URL+"/api/ai-runtime/default-profile", owner.OrgSlug)
	req, _ := http.NewRequest(http.MethodPut, url, strings.NewReader(fmt.Sprintf(`{"expected_revision":%d,"profile_id":"%s"}`, catalog.Revision, profile.ID)))
	req.Header.Set("Content-Type", "application/json")
	req.AddCookie(owner.Cookie)
	resp, _ = http.DefaultClient.Do(req)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("set default status=%d", resp.StatusCode)
	}
	var defaultBody struct {
		Impact airuntime.RuntimeImpactPreview `json:"impact_preview"`
	}
	_ = json.NewDecoder(resp.Body).Decode(&defaultBody)
	resp.Body.Close()
	if defaultBody.Impact.ReferenceCounts.DefaultProfile != 1 ||
		defaultBody.Impact.ReferenceCounts.TeamRoleProfileSelections != 1 ||
		defaultBody.Impact.ReferenceCounts.ExecutorProfileSelections != 1 {
		t.Fatalf("impact counts = %+v", defaultBody.Impact.ReferenceCounts)
	}
	if defaultBody.Impact.HistoricalNote == "" || defaultBody.Impact.ReferenceCounts.HistoricalExecutionSnapshot != 0 {
		t.Fatalf("historical snapshot impact = %+v", defaultBody.Impact)
	}
}

func seedRuntimeProfile(t *testing.T, svc *airuntime.Service, orgID, actor string) airuntime.RuntimeProfile {
	t.Helper()
	model, rev, err := svc.CreateModel(context.Background(), orgID, actor, 0, airuntime.ModelDefinition{
		Key: "gpt-stage4", ModelKey: "gpt-stage4", DisplayName: "GPT Stage 4",
		CompatibleCLIKeys: []string{"codex"}, DefaultParameters: map[string]any{}, Enabled: true,
	})
	if err != nil {
		t.Fatalf("create runtime model: %v", err)
	}
	profile, rev, err := svc.CreateProfile(context.Background(), orgID, actor, rev, airuntime.RuntimeProfile{
		Key: "stage4-coding", Name: "Stage 4 coding", CLIKey: "codex", ModelKey: model.Key,
		Parameters: map[string]any{}, Enabled: true,
	})
	if err != nil {
		t.Fatalf("create runtime profile: %v", err)
	}
	if _, err := svc.SetDefaultProfile(context.Background(), orgID, actor, profile.ID, rev); err != nil {
		t.Fatalf("set runtime default: %v", err)
	}
	return profile
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

func TestAIRuntimePreviewWarnsOnUnknownJSONAndYAMLFields(t *testing.T) {
	for _, contentType := range []string{"application/json", "application/yaml"} {
		t.Run(contentType, func(t *testing.T) {
			deps, db := setupAPIWithAuth(t)
			deps.RuntimeCatalog = airuntime.NewServiceWithValidationKey(
				airuntimesql.NewRepository(db),
				func() string { return "runtime-warning" },
				[]byte("0123456789abcdef0123456789abcdef"),
			)
			owner := setupTestSession(t, db, deps)
			server := newTestServer(t, deps)
			defer server.Close()

			payload := map[string]any{
				"strategy":     "merge",
				"unknown_root": true,
				"document": map[string]any{
					"schema_version": 1,
					"kind":           airuntime.ExportKind,
					"runtime": map[string]any{
						"unknown_runtime": true,
						"clis": []any{map[string]any{
							"key": "custom", "display_name": "Custom", "executable": "custom",
							"parameter_schema": map[string]any{"type": "object"},
							"enabled":          true,
							"unknown_cli":      true,
						}},
						"models":   []any{},
						"profiles": []any{},
					},
				},
			}
			var body []byte
			var err error
			if contentType == "application/yaml" {
				body, err = yaml.Marshal(payload)
			} else {
				body, err = json.Marshal(payload)
			}
			if err != nil {
				t.Fatal(err)
			}
			req, _ := http.NewRequest(http.MethodPost, orgScopedURL(server.URL+"/api/ai-runtime/import/preview", owner.OrgSlug), strings.NewReader(string(body)))
			req.Header.Set("Content-Type", contentType)
			req.AddCookie(owner.Cookie)
			resp, err := http.DefaultClient.Do(req)
			if err != nil {
				t.Fatal(err)
			}
			defer resp.Body.Close()
			if resp.StatusCode != http.StatusOK {
				t.Fatalf("status=%d", resp.StatusCode)
			}
			var preview airuntime.PreviewResponse
			if err := json.NewDecoder(resp.Body).Decode(&preview); err != nil {
				t.Fatal(err)
			}
			var paths []string
			for _, diagnostic := range preview.Report.Diagnostics {
				if diagnostic.Code == airuntime.ReasonImportUnknownField && diagnostic.Severity == "warning" {
					paths = append(paths, diagnostic.Path)
				}
			}
			want := []string{"$.document.runtime.clis[0].unknown_cli", "$.document.runtime.unknown_runtime", "$.unknown_root"}
			if fmt.Sprint(paths) != fmt.Sprint(want) {
				t.Fatalf("warning paths=%v want=%v", paths, want)
			}
		})
	}
}
