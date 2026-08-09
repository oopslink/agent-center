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
	pmsql "github.com/oopslink/agent-center/internal/projectmanager/sqlite"
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

	retired := []struct {
		method string
		path   string
		body   string
	}{
		{http.MethodGet, "/api/ai-runtime/profiles", ""},
		{http.MethodPost, "/api/ai-runtime/profiles", fmt.Sprintf(`{"expected_revision":%d,"value":{"key":"default-coding","name":"Default coding","cli_key":"codex","model_key":"%s","parameters":{},"enabled":true}}`, modelResult.Revision, modelResult.Entry.Key)},
		{http.MethodPatch, "/api/ai-runtime/profiles/runtime-profile-default", `{"expected_revision":1,"value":{"key":"default-coding"}}`},
		{http.MethodPut, "/api/ai-runtime/default-profile", `{"expected_revision":1,"profile_id":"runtime-profile-default"}`},
	}
	for _, tc := range retired {
		url := orgScopedURL(server.URL+tc.path, owner.OrgSlug)
		req, _ := http.NewRequest(tc.method, url, strings.NewReader(tc.body))
		req.Header.Set("Content-Type", "application/json")
		req.AddCookie(owner.Cookie)
		resp, _ = http.DefaultClient.Do(req)
		if resp.StatusCode != http.StatusNotFound {
			t.Fatalf("retired %s %s status=%d want 404", tc.method, tc.path, resp.StatusCode)
		}
		resp.Body.Close()
	}

	member := memberSessionInOrg(t, db, owner.OrgID, owner.OrgSlug)
	resp = orgScopedGet(t, server.URL+"/api/ai-runtime", member)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("member read status=%d", resp.StatusCode)
	}
	resp.Body.Close()
	resp = orgScopedPost(t, server.URL+"/api/ai-runtime/clis", `{"expected_revision":1,"value":{"key":"custom","display_name":"Custom","executable":"custom","enabled":true}}`, member)
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

func TestAIRuntimeModelsOnlyImportPreservesCLIsHTTP(t *testing.T) {
	deps, db := setupAPIWithAuth(t)
	nextID := 0
	deps.RuntimeCatalog = airuntime.NewServiceWithValidationKey(
		airuntimesql.NewRepository(db),
		func() string {
			nextID++
			return fmt.Sprintf("models-only-%d", nextID)
		},
		[]byte("0123456789abcdef0123456789abcdef"),
	)
	owner := setupTestSession(t, db, deps)
	server := newTestServer(t, deps)
	defer server.Close()

	ctx := context.Background()
	actor := "user:" + owner.IdentityID
	if _, _, err := deps.RuntimeCatalog.CreateModel(ctx, owner.OrgID, actor, 0, airuntime.ModelDefinition{
		Key: "gpt-5", ModelKey: "gpt-5", DisplayName: "GPT-5",
		CompatibleCLIKeys: []string{"codex"}, DefaultParameters: map[string]any{}, Enabled: true,
		ContextWindow: 400000, InputCost: 1.25, OutputCost: 10, Tier: "frontier",
	}); err != nil {
		t.Fatal(err)
	}
	before, err := deps.RuntimeCatalog.Catalog(ctx, owner.OrgID)
	if err != nil {
		t.Fatal(err)
	}
	beforeCLIState := map[string]bool{}
	for _, cli := range before.CLIs {
		beforeCLIState[cli.Key] = cli.Enabled
	}

	resp := orgScopedGet(t, server.URL+"/api/ai-runtime/export?format=json", owner)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("export status=%d", resp.StatusCode)
	}
	var doc airuntime.ExportDocument
	if err := json.NewDecoder(resp.Body).Decode(&doc); err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	doc.Runtime.Models = append(doc.Runtime.Models, airuntime.ExportModel{
		Key: "gpt-5-mini", ModelKey: "gpt-5-mini", DisplayName: "GPT-5 mini",
		CompatibleCLIKeys: []string{"codex"}, DefaultParameters: map[string]any{}, Enabled: true,
		ContextWindow: 128000, InputCost: 0.15, OutputCost: 0.6, Tier: "standard",
	})

	payload, _ := json.Marshal(airuntime.PreviewRequest{Strategy: airuntime.StrategyMerge, Document: doc})
	resp = orgScopedPost(t, server.URL+"/api/ai-runtime/import/preview", string(payload), owner)
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
	if !hasImportItem(preview.Report.Items, "model", "gpt-5-mini", "create") {
		t.Fatalf("preview did not show model create: %+v", preview.Report.Items)
	}
	if !hasImportItem(preview.Report.Items, "cli", "codex", "unchanged") {
		t.Fatalf("preview did not make preserved CLI explicit: %+v", preview.Report.Items)
	}

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

	after, err := deps.RuntimeCatalog.Catalog(ctx, owner.OrgID)
	if err != nil {
		t.Fatal(err)
	}
	if len(after.CLIs) != len(before.CLIs) {
		t.Fatalf("CLI count changed: before=%d after=%d", len(before.CLIs), len(after.CLIs))
	}
	for _, cli := range after.CLIs {
		if beforeCLIState[cli.Key] != cli.Enabled {
			t.Fatalf("CLI state changed for %s: before=%v after=%v", cli.Key, beforeCLIState[cli.Key], cli.Enabled)
		}
	}
	var imported *airuntime.ModelDefinition
	for i := range after.Models {
		if after.Models[i].Key == "gpt-5-mini" {
			imported = &after.Models[i]
			break
		}
	}
	if imported == nil || !imported.Enabled || imported.ContextWindow != 128000 {
		t.Fatalf("imported model missing or malformed: %+v", after.Models)
	}

	retiredDoc := doc
	retiredPayload, _ := json.Marshal(map[string]any{
		"strategy": "merge",
		"document": map[string]any{
			"schema_version": retiredDoc.SchemaVersion,
			"kind":           retiredDoc.Kind,
			"exported_at":    retiredDoc.ExportedAt,
			"runtime": map[string]any{
				"clis":                retiredDoc.Runtime.CLIs,
				"models":              retiredDoc.Runtime.Models,
				"default_profile_key": "default-coding",
			},
		},
	})
	resp = orgScopedPost(t, server.URL+"/api/ai-runtime/import/preview", string(retiredPayload), owner)
	if resp.StatusCode != http.StatusBadRequest {
		var body any
		_ = json.NewDecoder(resp.Body).Decode(&body)
		t.Fatalf("retired default_profile_key preview status=%d body=%+v", resp.StatusCode, body)
	}
	resp.Body.Close()

	retiredPayload, _ = json.Marshal(map[string]any{
		"strategy": "merge",
		"document": map[string]any{
			"schema_version": retiredDoc.SchemaVersion,
			"kind":           retiredDoc.Kind,
			"exported_at":    retiredDoc.ExportedAt,
			"runtime": map[string]any{
				"clis":     retiredDoc.Runtime.CLIs,
				"models":   retiredDoc.Runtime.Models,
				"profiles": []any{},
			},
		},
	})
	resp = orgScopedPost(t, server.URL+"/api/ai-runtime/import/preview", string(retiredPayload), owner)
	if resp.StatusCode != http.StatusBadRequest {
		var body any
		_ = json.NewDecoder(resp.Body).Decode(&body)
		t.Fatalf("retired profiles preview status=%d body=%+v", resp.StatusCode, body)
	}
	resp.Body.Close()
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

func hasImportItem(items []airuntime.ImportItem, entityType, key, action string) bool {
	for _, item := range items {
		if item.EntityType == entityType && item.Key == key && item.Action == action {
			return true
		}
	}
	return false
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
						"models": []any{},
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

func TestLegacyModelCatalogImportUsesRuntimePreviewApplyAdapter(t *testing.T) {
	deps, db := setupAPIWithAuth(t)
	deps.ModelCatalogRepo = pmsql.NewModelCatalogRepo(db)
	nextID := 0
	deps.RuntimeCatalog = airuntime.NewServiceWithValidationKey(
		airuntimesql.NewRepository(db),
		func() string {
			nextID++
			return fmt.Sprintf("legacy-adapter-%d", nextID)
		},
		[]byte("0123456789abcdef0123456789abcdef"),
	)
	owner := setupTestSession(t, db, deps)
	server := newTestServer(t, deps)
	defer server.Close()
	if _, _, err := deps.RuntimeCatalog.CreateModel(context.Background(), owner.OrgID, "user:"+owner.IdentityID, 0, airuntime.ModelDefinition{
		Key: "unrelated-runtime", ModelKey: "unrelated-runtime", DisplayName: "Unrelated Runtime",
		CompatibleCLIKeys: []string{"codex"}, DefaultParameters: map[string]any{}, Enabled: true,
	}); err != nil {
		t.Fatal(err)
	}

	resp := orgScopedPost(t, server.URL+"/api/model-catalog/import", `{
		"mode":"replace",
		"entries":[{
			"model_id":"gpt-legacy",
			"display_name":"GPT Legacy",
			"input_cost":1.25,
			"output_cost":2.5,
			"context_window":128000,
			"tier":"standard"
		}]
	}`, owner)
	if resp.StatusCode != http.StatusOK {
		var body any
		_ = json.NewDecoder(resp.Body).Decode(&body)
		t.Fatalf("legacy import status=%d body=%+v", resp.StatusCode, body)
	}
	resp.Body.Close()

	catalog, err := deps.RuntimeCatalog.Catalog(context.Background(), owner.OrgID)
	if err != nil {
		t.Fatal(err)
	}
	var imported *airuntime.ModelDefinition
	for i := range catalog.Models {
		if catalog.Models[i].Key == "gpt-legacy" {
			imported = &catalog.Models[i]
			break
		}
	}
	if imported == nil || imported.ContextWindow != 128000 || imported.InputCost != 1.25 || imported.OutputCost != 2.5 {
		t.Fatalf("legacy import did not mutate Runtime Catalog: %+v", catalog.Models)
	}
	for _, model := range catalog.Models {
		if model.Key == "unrelated-runtime" && !model.Enabled {
			t.Fatal("legacy replace disabled an unrelated Runtime Catalog model")
		}
	}
	resp = orgScopedGet(t, server.URL+"/api/model-catalog", owner)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("legacy list status=%d", resp.StatusCode)
	}
	var legacyList struct {
		Entries []map[string]any `json:"entries"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&legacyList); err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	listedImported := false
	listedRuntime := false
	for _, entry := range legacyList.Entries {
		listedImported = listedImported || entry["model_id"] == "gpt-legacy"
		listedRuntime = listedRuntime || entry["model_id"] == "unrelated-runtime"
	}
	if !listedImported || !listedRuntime {
		t.Fatalf("legacy compatibility projection should come from Runtime Catalog: %+v", legacyList.Entries)
	}
}
