package provider

import (
	"context"
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"

	"github.com/google/go-github/v66/github"
)

// newMockGitHub builds a GitHub adapter pointed at a test server, capturing the
// Authorization header the last request carried (to assert token auth).
func newMockGitHub(t *testing.T, handler http.Handler) (*GitHub, *string) {
	t.Helper()
	var lastAuth string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		lastAuth = r.Header.Get("Authorization")
		handler.ServeHTTP(w, r)
	}))
	t.Cleanup(srv.Close)
	base, _ := url.Parse(srv.URL + "/")
	g := &GitHub{clientFor: func(token string) *github.Client {
		c := github.NewClient(nil)
		if token != "" {
			c = c.WithAuthToken(token)
		}
		c.BaseURL = base
		return c
	}}
	return g, &lastAuth
}

func TestGitHub_ListCommits(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/repos/acme/widget/commits", func(w http.ResponseWriter, r *http.Request) {
		if got := r.URL.Query().Get("sha"); got != "main" {
			t.Errorf("sha = %q, want main", got)
		}
		w.Write([]byte(`[
		  {"sha":"abc123","html_url":"https://github.com/acme/widget/commit/abc123",
		   "commit":{"message":"fix bug","author":{"name":"Ada","email":"ada@x.io","date":"2026-06-01T10:00:00Z"}}},
		  {"sha":"def456","commit":{"message":"add feature","author":{"name":"Bo","email":"bo@x.io","date":"2026-06-02T11:00:00Z"}}}
		]`))
	})
	g, lastAuth := newMockGitHub(t, mux)

	commits, err := g.ListCommits(context.Background(), Target{URL: "https://github.com/acme/widget", Provider: "github", DefaultBranch: "main", Credential: "test-token"}, "", 10)
	if err != nil {
		t.Fatalf("ListCommits: %v", err)
	}
	if len(commits) != 2 {
		t.Fatalf("got %d commits, want 2", len(commits))
	}
	if commits[0].SHA != "abc123" || commits[0].Message != "fix bug" || commits[0].Author != "Ada" || commits[0].AuthorEmail != "ada@x.io" {
		t.Errorf("commit[0] = %+v", commits[0])
	}
	if commits[0].URL != "https://github.com/acme/widget/commit/abc123" {
		t.Errorf("commit[0].URL = %q", commits[0].URL)
	}
	if commits[0].CommittedAt.IsZero() {
		t.Error("commit[0].CommittedAt not parsed")
	}
	// Private-repo credential → bearer token auth on the wire.
	if *lastAuth != "Bearer test-token" {
		t.Errorf("Authorization = %q, want Bearer test-token", *lastAuth)
	}
}

func TestGitHub_ListCommits_LimitCaps(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/repos/acme/widget/commits", func(w http.ResponseWriter, r *http.Request) {
		// Return 3 even though limit=2; the adapter must cap.
		w.Write([]byte(`[{"sha":"a"},{"sha":"b"},{"sha":"c"}]`))
	})
	g, _ := newMockGitHub(t, mux)
	commits, err := g.ListCommits(context.Background(), Target{URL: "https://github.com/acme/widget", DefaultBranch: "main"}, "main", 2)
	if err != nil {
		t.Fatalf("ListCommits: %v", err)
	}
	if len(commits) != 2 {
		t.Errorf("got %d commits, want capped at 2", len(commits))
	}
}

func TestGitHub_ListBranches(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/repos/acme/widget/branches", func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(`[
		  {"name":"main","commit":{"sha":"abc123"}},
		  {"name":"dev","commit":{"sha":"def456"}}
		]`))
	})
	g, lastAuth := newMockGitHub(t, mux)
	branches, err := g.ListBranches(context.Background(), Target{URL: "https://github.com/acme/widget", DefaultBranch: "main"})
	if err != nil {
		t.Fatalf("ListBranches: %v", err)
	}
	if len(branches) != 2 {
		t.Fatalf("got %d branches, want 2", len(branches))
	}
	if branches[0].Name != "main" || branches[0].CommitSHA != "abc123" || !branches[0].IsDefault {
		t.Errorf("branch[0] = %+v, want main/abc123/default", branches[0])
	}
	if branches[1].IsDefault {
		t.Errorf("branch[1] (dev) must not be default")
	}
	// No credential → anonymous (no Authorization header).
	if *lastAuth != "" {
		t.Errorf("Authorization = %q, want empty (anonymous)", *lastAuth)
	}
}

func TestParseGitHubURL(t *testing.T) {
	cases := map[string]struct{ owner, repo string }{
		"https://github.com/acme/widget":     {"acme", "widget"},
		"https://github.com/acme/widget.git": {"acme", "widget"},
		"http://github.com/acme/widget/":     {"acme", "widget"},
		"git@github.com:acme/widget.git":     {"acme", "widget"},
		"github.com/acme/widget":             {"acme", "widget"},
		"https://ghe.corp.com/acme/widget":   {"acme", "widget"},
	}
	for in, want := range cases {
		owner, repo, err := parseGitHubURL(in)
		if err != nil {
			t.Errorf("%q: unexpected err %v", in, err)
			continue
		}
		if owner != want.owner || repo != want.repo {
			t.Errorf("%q → %s/%s, want %s/%s", in, owner, repo, want.owner, want.repo)
		}
	}
	for _, bad := range []string{"", "  ", "https://github.com/acme"} {
		if _, _, err := parseGitHubURL(bad); err == nil {
			t.Errorf("%q: expected parse error", bad)
		}
	}
}
