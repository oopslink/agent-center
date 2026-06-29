package provider

import (
	"context"
	"testing"
)

// stubProvider records that it was called and returns canned data.
type stubProvider struct {
	name           string
	commitsCalled  bool
	branchesCalled bool
	lastTargetURL  string
}

func (s *stubProvider) ListCommits(_ context.Context, t Target, _ string, _ int) ([]Commit, error) {
	s.commitsCalled = true
	s.lastTargetURL = t.URL
	return []Commit{{SHA: s.name}}, nil
}

func (s *stubProvider) ListBranches(_ context.Context, t Target) ([]Branch, error) {
	s.branchesCalled = true
	s.lastTargetURL = t.URL
	return []Branch{{Name: s.name}}, nil
}

func TestFactory_DispatchesByProvider(t *testing.T) {
	gh := &stubProvider{name: "gh"}
	git := &stubProvider{name: "git"}
	f := NewFactory(gh, git)

	// "github" → go-github adapter.
	if got := f.For("github"); got != Provider(gh) {
		t.Error("For(github) should return the github adapter")
	}
	// everything else → git fallback (the cross-provider catch-all).
	for _, p := range []string{"gitlab", "git", "bitbucket", "", "unknown"} {
		if got := f.For(p); got != Provider(git) {
			t.Errorf("For(%q) should return the git fallback", p)
		}
	}
}

func TestFactory_ListCommits_RoutesByTarget(t *testing.T) {
	gh := &stubProvider{name: "gh"}
	git := &stubProvider{name: "git"}
	f := NewFactory(gh, git)

	cs, err := f.ListCommits(context.Background(), Target{URL: "u1", Provider: "github"}, "", 0)
	if err != nil || len(cs) != 1 || cs[0].SHA != "gh" {
		t.Fatalf("github route: %+v %v", cs, err)
	}
	if !gh.commitsCalled || git.commitsCalled {
		t.Error("only the github adapter should have been called")
	}

	bs, err := f.ListBranches(context.Background(), Target{URL: "u2", Provider: "gitlab"})
	if err != nil || len(bs) != 1 || bs[0].Name != "git" {
		t.Fatalf("git fallback route: %+v %v", bs, err)
	}
	if !git.branchesCalled {
		t.Error("the git fallback should have been called for gitlab")
	}
}

func TestFactory_GithubNil_FallsBackToGit(t *testing.T) {
	git := &stubProvider{name: "git"}
	f := NewFactory(nil, git)
	if got := f.For("github"); got != Provider(git) {
		t.Error("For(github) with nil github adapter should fall back to git")
	}
}

func TestClampLimit(t *testing.T) {
	cases := map[int]int{0: DefaultCommitLimit, -5: DefaultCommitLimit, 1: 1, 50: 50, MaxCommitLimit: MaxCommitLimit, MaxCommitLimit + 1: MaxCommitLimit, 9999: MaxCommitLimit}
	for in, want := range cases {
		if got := ClampLimit(in); got != want {
			t.Errorf("ClampLimit(%d) = %d, want %d", in, got, want)
		}
	}
}
