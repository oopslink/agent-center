package provider

import (
	"context"
	"strings"
	"testing"
)

// fakeGitRunner returns canned ls-remote / log output and records the auth url it
// was handed (to assert credential injection without a network or git binary).
type fakeGitRunner struct {
	lsRemoteOut string
	logOut      string
	gotLsURL    string
	gotLogURL   string
	gotBranch   string
	gotLimit    int
	err         error
}

func (f *fakeGitRunner) LsRemoteHeads(_ context.Context, authURL string) (string, error) {
	f.gotLsURL = authURL
	return f.lsRemoteOut, f.err
}

func (f *fakeGitRunner) ShallowLog(_ context.Context, authURL, branch string, limit int) (string, error) {
	f.gotLogURL, f.gotBranch, f.gotLimit = authURL, branch, limit
	return f.logOut, f.err
}

func TestGit_ListBranches(t *testing.T) {
	fr := &fakeGitRunner{lsRemoteOut: "" +
		"abc123\trefs/heads/main\n" +
		"def456\trefs/heads/dev\n" +
		"999\trefs/tags/v1\n" + // non-head ref → skipped
		"\n"}
	g := &Git{runner: fr}
	branches, err := g.ListBranches(context.Background(), Target{URL: "https://gitlab.com/acme/widget", Provider: "git", DefaultBranch: "main"})
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
}

func TestGit_ListCommits(t *testing.T) {
	// gitLogFormat fields: sha \x1f author \x1f email \x1f isoDate \x1f subject
	logOut := strings.Join([]string{
		strings.Join([]string{"abc123", "Ada", "ada@x.io", "2026-06-01T10:00:00Z", "fix bug"}, "\x1f"),
		strings.Join([]string{"def456", "Bo", "bo@x.io", "2026-06-02T11:00:00Z", "add feature"}, "\x1f"),
	}, "\n")
	fr := &fakeGitRunner{logOut: logOut}
	g := &Git{runner: fr}
	commits, err := g.ListCommits(context.Background(), Target{URL: "https://gitlab.com/acme/widget", DefaultBranch: "main"}, "", 5)
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
	// Empty branch → fell back to the default branch for the fetch.
	if fr.gotBranch != "main" {
		t.Errorf("fetched branch = %q, want default main", fr.gotBranch)
	}
	if fr.gotLimit != 5 {
		t.Errorf("fetch depth = %d, want 5", fr.gotLimit)
	}
}

func TestGit_ListCommits_NoBranch(t *testing.T) {
	g := &Git{runner: &fakeGitRunner{}}
	if _, err := g.ListCommits(context.Background(), Target{URL: "https://x/y", DefaultBranch: ""}, "", 5); err == nil {
		t.Error("expected error when no branch and no default branch")
	}
}

func TestInjectCredential(t *testing.T) {
	cases := []struct {
		name, url, cred, want string
	}{
		{"https with token", "https://github.com/a/b", "tok", "https://x-access-token:tok@github.com/a/b"},
		{"empty cred unchanged", "https://github.com/a/b", "", "https://github.com/a/b"},
		{"ssh unchanged", "git@github.com:a/b.git", "tok", "git@github.com:a/b.git"},
		{"already has userinfo", "https://user@github.com/a/b", "tok", "https://user@github.com/a/b"},
	}
	for _, c := range cases {
		if got := injectCredential(c.url, c.cred); got != c.want {
			t.Errorf("%s: injectCredential(%q,%q) = %q, want %q", c.name, c.url, c.cred, got, c.want)
		}
	}
}

func TestGit_CredentialInjectedIntoAuthURL(t *testing.T) {
	fr := &fakeGitRunner{lsRemoteOut: "abc\trefs/heads/main\n"}
	g := &Git{runner: fr}
	_, err := g.ListBranches(context.Background(), Target{URL: "https://gitlab.com/a/b", Provider: "git", Credential: "secret-tok", DefaultBranch: "main"})
	if err != nil {
		t.Fatalf("ListBranches: %v", err)
	}
	if !strings.Contains(fr.gotLsURL, "x-access-token:secret-tok@") {
		t.Errorf("ls-remote authURL = %q, want embedded credential", fr.gotLsURL)
	}
}
