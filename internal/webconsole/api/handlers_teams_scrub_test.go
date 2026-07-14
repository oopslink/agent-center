package api

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/oopslink/agent-center/internal/team"
)

// seedTemplateServer builds the facade over deps and seeds one stored template
// (with its seed-memory experiences) directly into the in-process template store,
// returning the running test server and the template id. This is the only way to
// hand the scrub endpoint a template that carries experiences — the create
// endpoint authors roles-only templates.
func seedTemplateServer(t *testing.T, deps HandlerDeps, orgID string, exps []team.Experience) (*httptest.Server, string) {
	t.Helper()
	srv := NewServer("127.0.0.1:0", Deps{})
	ts := httptest.NewServer(WithDeps(deps)(srv.Handler()))
	tmpl, err := team.NewTemplate(team.NewTemplateInput{
		ID:          "teamtmpl-scrub-fixture",
		OrgID:       orgID,
		Name:        "Backend Squad",
		Roles:       []team.RoleSlot{{Config: implRole[0], Count: 1}},
		Experiences: exps,
		CreatedAt:   time.Now().UTC(),
	})
	if err != nil {
		t.Fatalf("NewTemplate: %v", err)
	}
	srv.teamTemplates.add(orgID, &storedTemplate{tmpl: tmpl, source: "manual", sourceKind: "manual"})
	return ts, tmpl.ID
}

// TestTemplateScrub_FindingsFromExperiences locks that the endpoint runs the real
// curation-assist scrub over the template's seed-memory experiences and returns
// truthful findings (only {experience_slug, kind, token} — no display-layer
// enrichment leaks from the backend). A code-name ticket token in the experience
// body must surface.
func TestTemplateScrub_FindingsFromExperiences(t *testing.T) {
	deps, _, sess := setupTeamsAPI(t)
	ts, tid := seedTemplateServer(t, deps, sess.OrgID, []team.Experience{
		{
			Slug:  "barrier-fix",
			Title: "Barrier fix",
			Body:  "Resolved ticket PROJ-42 cleanly.",
			Scope: team.ExpScopeTeam,
		},
	})
	defer ts.Close()

	resp := orgScopedGet(t, ts.URL+"/api/team-templates/"+tid+"/scrub", sess)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("scrub = %d, want 200", resp.StatusCode)
	}
	body := decodeBody(t, resp)
	findings, ok := body["scrub_findings"].([]any)
	if !ok || len(findings) == 0 {
		t.Fatalf("scrub_findings = %v, want at least one finding", body["scrub_findings"])
	}
	var found bool
	for _, raw := range findings {
		f, ok := raw.(map[string]any)
		if !ok {
			t.Fatalf("finding is not an object: %v", raw)
		}
		// Truthful-token contract: exactly the three backend keys, no display
		// enrichment (risk/reason/default_action are FE-side only).
		if len(f) != 3 {
			t.Errorf("finding has %d keys %v, want 3 (experience_slug,kind,token)", len(f), f)
		}
		if _, bad := f["risk"]; bad {
			t.Errorf("finding leaked display enrichment: %v", f)
		}
		if f["experience_slug"] == "barrier-fix" && f["kind"] == string(team.ScrubCodeName) && f["token"] == "PROJ-42" {
			found = true
		}
	}
	if !found {
		t.Errorf("expected a code_name PROJ-42 finding for barrier-fix, got %v", findings)
	}
}

// TestTemplateScrub_EmptyWhenNoExperiences locks the honest empty-state: a
// template with no seed memory returns an empty (never fixture) findings array,
// so the Curation pane shows nothing rather than fabricated data.
func TestTemplateScrub_EmptyWhenNoExperiences(t *testing.T) {
	deps, _, sess := setupTeamsAPI(t)
	ts, tid := seedTemplateServer(t, deps, sess.OrgID, nil)
	defer ts.Close()

	resp := orgScopedGet(t, ts.URL+"/api/team-templates/"+tid+"/scrub", sess)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("scrub = %d, want 200", resp.StatusCode)
	}
	body := decodeBody(t, resp)
	findings, ok := body["scrub_findings"].([]any)
	if !ok || len(findings) != 0 {
		t.Errorf("scrub_findings = %v, want [] (no experiences → honest empty)", body["scrub_findings"])
	}
}

// TestTemplateScrub_NotFound locks the 404 for an unknown template id.
func TestTemplateScrub_NotFound(t *testing.T) {
	deps, _, sess := setupTeamsAPI(t)
	ts := newTestServer(t, deps)
	defer ts.Close()

	resp := orgScopedGet(t, ts.URL+"/api/team-templates/ghost/scrub", sess)
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusNotFound {
		t.Fatalf("scrub unknown template = %d, want 404", resp.StatusCode)
	}
}
