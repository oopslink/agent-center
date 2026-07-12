package centergit

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// agentHeader is the request header the test resolver reads to identify the
// calling agent (stands in for the admin API's bearer-token → agent resolution).
const agentHeader = "X-Agent-Id"

func headerResolver(r *http.Request) (string, bool) {
	id := r.Header.Get(agentHeader)
	return id, id != ""
}

// newTestBackend provisions a host with agent-1 + team-alpha + global repos,
// grants agent-1 → team-alpha, and returns a live httptest server mounting the
// handler at /git/.
func newTestBackend(t *testing.T) (srvURL, root, home string) {
	t.Helper()
	ctx := context.Background()
	root = t.TempDir()
	home = t.TempDir()
	host := NewHost(root, nil)
	for _, ref := range []RepoRef{AgentRepo("agent-1"), TeamRepo("team-alpha"), GlobalRepo()} {
		if err := host.EnsureRepo(ctx, ref); err != nil {
			t.Fatalf("provision %s: %v", ref, err)
		}
	}
	mem := NewMapMembership()
	mem.Grant("agent-1", "team-alpha")
	h, err := NewHandler(host, NewAuthorizer(mem), headerResolver, WithMountPrefix("/git"))
	if err != nil {
		t.Fatalf("NewHandler: %v", err)
	}
	mux := http.NewServeMux()
	mux.Handle("/git/", h)
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	return srv.URL, root, home
}

// gitClient runs the git client with an injected agent-id header (when agentID
// is non-empty), returning combined output + error.
func gitClient(home, dir, agentID string, args ...string) (string, error) {
	full := args
	if agentID != "" {
		full = append([]string{"-c", "http.extraHeader=" + agentHeader + ": " + agentID}, args...)
	}
	return tryGit(home, dir, full...)
}

func TestHandlerRoundTripAgentRepo(t *testing.T) {
	srvURL, _, home := newTestBackend(t)
	repoURL := srvURL + "/git/agent/agent-1.git"

	// Clone (empty repo → warning, exit 0), write, commit, push as agent-1.
	work := t.TempDir()
	if out, err := gitClient(home, work, "agent-1", "clone", repoURL, "wc"); err != nil {
		t.Fatalf("clone: %v\n%s", err, out)
	}
	wc := filepath.Join(work, "wc")
	if err := os.WriteFile(filepath.Join(wc, "hello.md"), []byte("hi\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	runGit(t, home, wc, "add", "-A")
	runGit(t, home, wc, "-c", "commit.gpgsign=false", "commit", "-m", "add hello")
	if out, err := gitClient(home, wc, "agent-1", "push", "origin", "HEAD:main"); err != nil {
		t.Fatalf("push: %v\n%s", err, out)
	}

	// Fresh clone must observe the pushed content (proves smart-HTTP read+write).
	verify := t.TempDir()
	if out, err := gitClient(home, verify, "agent-1", "clone", repoURL, "check"); err != nil {
		t.Fatalf("verify clone: %v\n%s", err, out)
	}
	got, err := os.ReadFile(filepath.Join(verify, "check", "hello.md"))
	if err != nil || string(got) != "hi\n" {
		t.Fatalf("round-trip content mismatch: got %q err %v", got, err)
	}
}

func TestHandlerTeamMemberReadNonMemberDenied(t *testing.T) {
	srvURL, root, home := newTestBackend(t)
	// Seed the team repo directly on disk so there is something to clone.
	host := NewHost(root, nil)
	teamBare, _ := host.RepoDir(TeamRepo("team-alpha"))
	seedBare(t, home, teamBare)
	teamURL := srvURL + "/git/team/team-alpha.git"

	// Member (agent-1) may clone.
	memberDir := t.TempDir()
	if out, err := gitClient(home, memberDir, "agent-1", "clone", teamURL, "wc"); err != nil {
		t.Fatalf("member clone should succeed: %v\n%s", err, out)
	}
	// Non-member (agent-2) is rejected by access control (HTTP 403 → clone fails).
	strangerDir := t.TempDir()
	out, err := gitClient(home, strangerDir, "agent-2", "clone", teamURL, "wc")
	if err == nil {
		t.Fatalf("non-member clone should FAIL, but succeeded:\n%s", out)
	}
}

// recorderRequest drives the handler directly for precise status-code checks.
func recorderRequest(t *testing.T, method, path, agentID string) int {
	t.Helper()
	ctx := context.Background()
	root := t.TempDir()
	host := NewHost(root, nil)
	// Provision the standard set for authz-positive paths.
	for _, ref := range []RepoRef{AgentRepo("agent-1"), TeamRepo("team-alpha"), GlobalRepo()} {
		if err := host.EnsureRepo(ctx, ref); err != nil {
			t.Fatal(err)
		}
	}
	mem := NewMapMembership()
	mem.Grant("agent-1", "team-alpha")
	h, err := NewHandler(host, NewAuthorizer(mem), headerResolver, WithMountPrefix("/git"))
	if err != nil {
		t.Fatal(err)
	}
	req := httptest.NewRequest(method, path, nil)
	if agentID != "" {
		req.Header.Set(agentHeader, agentID)
	}
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	return rec.Code
}

func TestHandlerStatusMapping(t *testing.T) {
	tests := []struct {
		name    string
		method  string
		path    string
		agent   string
		want    int
	}{
		{"unauthenticated", "GET", "/git/agent/agent-1.git/info/refs?service=git-upload-pack", "", http.StatusUnauthorized},
		{"forbidden other agent", "GET", "/git/agent/agent-2.git/info/refs?service=git-upload-pack", "agent-1", http.StatusForbidden},
		{"forbidden global write", "GET", "/git/global.git/info/refs?service=git-receive-pack", "agent-1", http.StatusForbidden},
		{"forbidden non-member team", "GET", "/git/team/team-alpha.git/info/refs?service=git-upload-pack", "stranger", http.StatusForbidden},
		{"not found own unprovisioned", "GET", "/git/agent/agent-9.git/info/refs?service=git-upload-pack", "agent-9", http.StatusNotFound},
		{"bad path", "GET", "/git/nonsense", "agent-1", http.StatusNotFound},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := recorderRequest(t, tc.method, tc.path, tc.agent); got != tc.want {
				t.Fatalf("status=%d want %d", got, tc.want)
			}
		})
	}
}

func TestOperationForClassification(t *testing.T) {
	cases := []struct {
		sub, svc string
		want     Operation
	}{
		{"git-receive-pack", "", OpWrite},
		{"info/refs", "git-receive-pack", OpWrite},
		{"info/refs", "git-upload-pack", OpRead},
		{"git-upload-pack", "", OpRead},
		{"objects/info/packs", "", OpRead},
	}
	for _, c := range cases {
		if got := operationFor(c.sub, c.svc); got != c.want {
			t.Errorf("operationFor(%q,%q)=%v want %v", c.sub, c.svc, got, c.want)
		}
	}
}

func TestNewHandlerRequiresDeps(t *testing.T) {
	if _, err := NewHandler(nil, nil, nil); err == nil {
		t.Fatal("expected error for nil deps")
	}
}

func TestNewHandlerWithOverrides(t *testing.T) {
	host := NewHost(t.TempDir(), nil)
	h, err := NewHandler(host, NewAuthorizer(NewMapMembership()), headerResolver,
		WithHTTPBackend("/nonexistent/git-http-backend"),
		WithExtraEnv("FOO=bar"))
	if err != nil {
		t.Fatalf("NewHandler with overrides: %v", err)
	}
	if !strings.Contains(h.httpBackend, "git-http-backend") {
		t.Fatalf("override not applied: %q", h.httpBackend)
	}
}
