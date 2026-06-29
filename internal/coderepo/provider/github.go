package provider

import (
	"context"
	"fmt"
	"strings"

	"github.com/google/go-github/v66/github"
)

// GitHub is the go-github REST adapter (v2.18.4 BE-2): rich commit/branch metadata
// over the GitHub API, no clone. A private repo's credential is a PAT/token applied
// per call (go-github's WithAuthToken); an empty credential reads anonymously
// (public repos / rate-limited).
type GitHub struct {
	// clientFor builds a *github.Client for the optional token. A seam so tests
	// point the client at a mock server (overriding BaseURL); production uses the
	// real api.github.com client.
	clientFor func(token string) *github.Client
}

// NewGitHub builds the production go-github adapter.
func NewGitHub() *GitHub {
	return &GitHub{clientFor: defaultGitHubClient}
}

// defaultGitHubClient returns a real api.github.com client, token-authenticated
// when a credential is present.
func defaultGitHubClient(token string) *github.Client {
	c := github.NewClient(nil)
	if strings.TrimSpace(token) != "" {
		c = c.WithAuthToken(strings.TrimSpace(token))
	}
	return c
}

// ListCommits returns up to `limit` recent commits on branch (empty → default).
func (g *GitHub) ListCommits(ctx context.Context, t Target, branch string, limit int) ([]Commit, error) {
	owner, repo, err := parseGitHubURL(t.URL)
	if err != nil {
		return nil, err
	}
	if strings.TrimSpace(branch) == "" {
		branch = t.DefaultBranch
	}
	n := ClampLimit(limit)
	cl := g.clientFor(t.Credential)
	rcs, _, err := cl.Repositories.ListCommits(ctx, owner, repo, &github.CommitsListOptions{
		SHA:         strings.TrimSpace(branch),
		ListOptions: github.ListOptions{PerPage: n},
	})
	if err != nil {
		return nil, fmt.Errorf("provider/github: list commits %s/%s: %w", owner, repo, err)
	}
	out := make([]Commit, 0, len(rcs))
	for _, rc := range rcs {
		if len(out) >= n {
			break
		}
		out = append(out, mapGitHubCommit(rc))
	}
	return out, nil
}

// ListBranches returns the repo's branches, marking the default from the target.
func (g *GitHub) ListBranches(ctx context.Context, t Target) ([]Branch, error) {
	owner, repo, err := parseGitHubURL(t.URL)
	if err != nil {
		return nil, err
	}
	cl := g.clientFor(t.Credential)
	bs, _, err := cl.Repositories.ListBranches(ctx, owner, repo, &github.BranchListOptions{
		ListOptions: github.ListOptions{PerPage: 100},
	})
	if err != nil {
		return nil, fmt.Errorf("provider/github: list branches %s/%s: %w", owner, repo, err)
	}
	out := make([]Branch, 0, len(bs))
	for _, b := range bs {
		out = append(out, Branch{
			Name:      b.GetName(),
			CommitSHA: b.GetCommit().GetSHA(),
			IsDefault: b.GetName() == t.DefaultBranch,
		})
	}
	return out, nil
}

// mapGitHubCommit maps a go-github RepositoryCommit to the provider-agnostic shape.
func mapGitHubCommit(rc *github.RepositoryCommit) Commit {
	c := Commit{SHA: rc.GetSHA(), URL: rc.GetHTMLURL()}
	if gc := rc.GetCommit(); gc != nil {
		c.Message = gc.GetMessage()
		if a := gc.GetAuthor(); a != nil {
			c.Author = a.GetName()
			c.AuthorEmail = a.GetEmail()
			c.CommittedAt = a.GetDate().Time
		}
	}
	return c
}

// parseGitHubURL extracts owner/repo from the common GitHub URL shapes:
//
//	https://github.com/owner/repo(.git)
//	http://github.com/owner/repo
//	git@github.com:owner/repo.git
//	github.com/owner/repo
//
// The host is not validated against github.com (GitHub Enterprise uses other
// hosts); only the trailing owner/repo path matters for the REST call.
func parseGitHubURL(raw string) (owner, repo string, err error) {
	s := strings.TrimSpace(raw)
	if s == "" {
		return "", "", fmt.Errorf("provider/github: empty repo url")
	}
	// Strip scheme.
	if i := strings.Index(s, "://"); i >= 0 {
		s = s[i+3:]
	}
	// scp-like ssh: git@host:owner/repo → host/owner/repo
	if at := strings.Index(s, "@"); at >= 0 {
		s = s[at+1:]
	}
	s = strings.Replace(s, ":", "/", 1) // host:owner/repo → host/owner/repo
	s = strings.TrimSuffix(s, "/")
	s = strings.TrimSuffix(s, ".git")
	parts := strings.Split(s, "/")
	if len(parts) < 3 {
		return "", "", fmt.Errorf("provider/github: cannot parse owner/repo from %q", raw)
	}
	owner = parts[len(parts)-2]
	repo = parts[len(parts)-1]
	if owner == "" || repo == "" {
		return "", "", fmt.Errorf("provider/github: cannot parse owner/repo from %q", raw)
	}
	return owner, repo, nil
}
