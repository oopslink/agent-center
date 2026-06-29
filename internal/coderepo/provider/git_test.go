package provider

import (
	"context"
	"errors"
	"strings"
	"testing"
)

// fakeGitRunner returns canned ls-remote / log output and records the (clean) url +
// token it was handed separately (so the test can assert the token is NEVER folded
// into the url/argv).
type fakeGitRunner struct {
	lsRemoteOut string
	logOut      string
	gotLsURL    string
	gotLogURL   string
	gotToken    string
	gotBranch   string
	gotLimit    int
	err         error
}

func (f *fakeGitRunner) LsRemoteHeads(_ context.Context, url, token string) (string, error) {
	f.gotLsURL, f.gotToken = url, token
	return f.lsRemoteOut, f.err
}

func (f *fakeGitRunner) ShallowLog(_ context.Context, url, token, branch string, limit int) (string, error) {
	f.gotLogURL, f.gotToken, f.gotBranch, f.gotLimit = url, token, branch, limit
	return f.logOut, f.err
}

func TestGit_ListBranches(t *testing.T) {
	fr := &fakeGitRunner{lsRemoteOut: "" +
		"abc123\trefs/heads/main\n" +
		"def456\trefs/heads/dev\n" +
		"999\trefs/tags/v1\n" + // non-head ref → skipped
		"\n"}
	g := &Git{runner: fr}
	branches, err := g.ListBranches(context.Background(), Target{URL: "https://gitlab.com/acme/widget", Provider: "git", Credential: "tok", DefaultBranch: "main"})
	if err != nil {
		t.Fatalf("ListBranches: %v", err)
	}
	if len(branches) != 2 {
		t.Fatalf("got %d branches, want 2 (tag skipped)", len(branches))
	}
	if branches[0].Name != "main" || branches[0].CommitSHA != "abc123" || !branches[0].IsDefault {
		t.Errorf("branch[0] = %+v", branches[0])
	}
	if branches[1].Name != "dev" || branches[1].IsDefault {
		t.Errorf("branch[1] = %+v", branches[1])
	}
	// The token is passed SEPARATELY; the url stays clean (no userinfo).
	if fr.gotLsURL != "https://gitlab.com/acme/widget" || strings.Contains(fr.gotLsURL, "tok") {
		t.Errorf("ls-remote url = %q, want clean url without token", fr.gotLsURL)
	}
	if fr.gotToken != "tok" {
		t.Errorf("token = %q, want passed separately", fr.gotToken)
	}
}

func TestGit_ListCommits(t *testing.T) {
	// gitLogFormat fields: sha \x1f author \x1f email \x1f isoDate \x1f subject
	logOut := strings.Join([]string{
		strings.Join([]string{"abc123", "Ada", "ada@x.io", "2026-06-01T10:00:00Z", "fix bug"}, "\x1f"),
		strings.Join([]string{"def456", "Bo", "bo@x.io", "2026-06-02T11:00:00Z", "add feature"}, "\x1f"),
	}, "\n")
	fr := &fakeGitRunner{logOut: logOut}
	g := &Git{runner: fr}
	commits, err := g.ListCommits(context.Background(), Target{URL: "https://gitlab.com/acme/widget", Credential: "tok", DefaultBranch: "main"}, "", 5)
	if err != nil {
		t.Fatalf("ListCommits: %v", err)
	}
	if len(commits) != 2 {
		t.Fatalf("got %d commits, want 2", len(commits))
	}
	if commits[0].SHA != "abc123" || commits[0].Author != "Ada" || commits[0].AuthorEmail != "ada@x.io" || commits[0].Message != "fix bug" {
		t.Errorf("commit[0] = %+v", commits[0])
	}
	if commits[0].CommittedAt.IsZero() {
		t.Error("commit[0].CommittedAt not parsed")
	}
	if fr.gotBranch != "main" { // empty branch → default
		t.Errorf("fetched branch = %q, want default main", fr.gotBranch)
	}
	if fr.gotLimit != 5 {
		t.Errorf("fetch depth = %d, want 5", fr.gotLimit)
	}
	// Clean url, token separate.
	if strings.Contains(fr.gotLogURL, "tok") || fr.gotToken != "tok" {
		t.Errorf("log url = %q token = %q; token must not be in the url", fr.gotLogURL, fr.gotToken)
	}
}

func TestGit_ListCommits_NoBranch(t *testing.T) {
	g := &Git{runner: &fakeGitRunner{}}
	if _, err := g.ListCommits(context.Background(), Target{URL: "https://x/y", DefaultBranch: ""}, "", 5); err == nil {
		t.Error("expected error when no branch and no default branch")
	}
}

// SECURITY: the argv builders take a url only — the token is structurally impossible
// to pass, so it can never land in argv (/proc/<pid>/cmdline, world-readable).
func TestGitArgs_NeverCarryToken(t *testing.T) {
	ls := gitLsRemoteArgs("https://gitlab.com/a/b")
	fetch := gitFetchArgs("/tmp/x", "https://gitlab.com/a/b", "main", 10)
	for _, args := range [][]string{ls, fetch} {
		joined := strings.Join(args, " ")
		if strings.Contains(joined, "x-access-token") || strings.Contains(joined, "Authorization") || strings.Contains(joined, "@gitlab.com") {
			t.Errorf("argv %q must not carry any credential", joined)
		}
	}
}

// SECURITY: the token rides the child ENV as an http.extraHeader, not argv.
func TestGitAuthEnv(t *testing.T) {
	// With a token: an Authorization: Basic header is injected via GIT_CONFIG_*.
	env := gitAuthEnv("sekret-tok")
	var foundHeader, foundCount bool
	for _, e := range env {
		if strings.HasPrefix(e, "GIT_CONFIG_VALUE_0=Authorization: Basic ") {
			foundHeader = true
			// The raw token must NOT appear verbatim (it's base64'd inside the header).
			if strings.Contains(e, "sekret-tok") {
				t.Errorf("raw token leaked into env value: %q", e)
			}
		}
		if e == "GIT_CONFIG_COUNT=1" {
			foundCount = true
		}
		if e == "GIT_TERMINAL_PROMPT=0" {
			// non-interactive — good
			_ = e
		}
	}
	if !foundHeader || !foundCount {
		t.Errorf("token env missing GIT_CONFIG header/count: header=%v count=%v", foundHeader, foundCount)
	}
	// Without a token: no GIT_CONFIG auth injected (anonymous).
	for _, e := range gitAuthEnv("") {
		if strings.HasPrefix(e, "GIT_CONFIG_COUNT") || strings.Contains(e, "Authorization") {
			t.Errorf("empty token must not inject auth config, got %q", e)
		}
	}
}

func TestRedactSecrets(t *testing.T) {
	cases := map[string]string{
		"fatal: could not read from https://x-access-token:SEKRET@github.com/a/b": "SEKRET",
		"remote: Authorization: Basic eHRva2VuOnNla3JldA== rejected":              "eHRva2VuOnNla3JldA==",
	}
	for in, secret := range cases {
		out := redactSecrets(in)
		if strings.Contains(out, secret) {
			t.Errorf("redactSecrets(%q) = %q, still contains secret %q", in, out, secret)
		}
		if !strings.Contains(out, "***") {
			t.Errorf("redactSecrets(%q) = %q, expected a *** marker", in, out)
		}
	}
}

// SECURITY (error path): a fetch failure whose git stderr echoes a credential must
// be REDACTED before the error propagates to the API/agent surface.
func TestGit_ErrorPath_RedactsLeakedToken(t *testing.T) {
	fr := &fakeGitRunner{err: errors.New("fatal: unable to access 'https://x-access-token:LEAKED@github.com/a/b': 403")}
	g := &Git{runner: fr}

	_, err := g.ListBranches(context.Background(), Target{URL: "https://github.com/a/b", Provider: "git", Credential: "LEAKED", DefaultBranch: "main"})
	if err == nil {
		t.Fatal("expected an error")
	}
	if strings.Contains(err.Error(), "LEAKED") {
		t.Errorf("ls-remote error leaked the token: %q", err.Error())
	}

	_, err = g.ListCommits(context.Background(), Target{URL: "https://github.com/a/b", Credential: "LEAKED", DefaultBranch: "main"}, "main", 5)
	if err == nil {
		t.Fatal("expected an error")
	}
	if strings.Contains(err.Error(), "LEAKED") {
		t.Errorf("shallow-log error leaked the token: %q", err.Error())
	}
}
