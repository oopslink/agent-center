package provider

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"time"
)

// Git is the generic fallback adapter for any non-github provider (gitlab,
// self-hosted, "git", or unknown): branches via `git ls-remote --heads` and
// commits via a throwaway SHALLOW fetch + `git log` — never a full/working clone
// (issue-f980c8de: "不 clone"). All git invocations go through the gitRunner seam
// so tests fake them without a network/binary.
type Git struct {
	runner gitRunner
}

// NewGit builds the production git fallback (real `git` over os/exec + a temp dir).
func NewGit() *Git {
	return &Git{runner: execGitRunner{}}
}

// gitRunner is the seam over the two git operations the fallback needs.
type gitRunner interface {
	// LsRemoteHeads runs `git ls-remote --heads <authURL>` and returns its stdout.
	LsRemoteHeads(ctx context.Context, authURL string) (string, error)
	// ShallowLog shallow-fetches `branch` (depth=limit) into a throwaway dir and
	// returns `git log` stdout in the unit-separated format gitLogFormat expects.
	ShallowLog(ctx context.Context, authURL, branch string, limit int) (string, error)
}

// ListBranches parses `git ls-remote --heads`. Each line is "<sha>\trefs/heads/<name>".
func (g *Git) ListBranches(ctx context.Context, t Target) ([]Branch, error) {
	out, err := g.runner.LsRemoteHeads(ctx, injectCredential(t.URL, t.Credential))
	if err != nil {
		return nil, fmt.Errorf("provider/git: ls-remote %s: %w", t.URL, err)
	}
	var branches []Branch
	for _, line := range strings.Split(out, "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		fields := strings.Fields(line)
		if len(fields) != 2 {
			continue
		}
		sha, ref := fields[0], fields[1]
		name := strings.TrimPrefix(ref, "refs/heads/")
		if name == ref { // not a head ref — skip
			continue
		}
		branches = append(branches, Branch{
			Name:      name,
			CommitSHA: sha,
			IsDefault: name == t.DefaultBranch,
		})
	}
	return branches, nil
}

// ListCommits shallow-fetches the branch and parses `git log`.
func (g *Git) ListCommits(ctx context.Context, t Target, branch string, limit int) ([]Commit, error) {
	if strings.TrimSpace(branch) == "" {
		branch = t.DefaultBranch
	}
	branch = strings.TrimSpace(branch)
	if branch == "" {
		return nil, fmt.Errorf("provider/git: no branch and no default branch to fetch commits")
	}
	n := ClampLimit(limit)
	out, err := g.runner.ShallowLog(ctx, injectCredential(t.URL, t.Credential), branch, n)
	if err != nil {
		return nil, fmt.Errorf("provider/git: shallow log %s@%s: %w", t.URL, branch, err)
	}
	return parseGitLog(out, n), nil
}

// gitLogFormat is the `git log --format` string: fields unit-separated (\x1f),
// records newline-separated. Order: sha, author name, author email, committer
// ISO-8601 date, subject.
const gitLogFormat = "%H%x1f%an%x1f%ae%x1f%cI%x1f%s"

// parseGitLog parses gitLogFormat output into Commits (capped at limit).
func parseGitLog(out string, limit int) []Commit {
	var commits []Commit
	for _, line := range strings.Split(out, "\n") {
		if strings.TrimSpace(line) == "" {
			continue
		}
		if len(commits) >= limit {
			break
		}
		f := strings.Split(line, "\x1f")
		if len(f) != 5 {
			continue
		}
		c := Commit{SHA: f[0], Author: f[1], AuthorEmail: f[2], Message: f[4]}
		if ts, err := time.Parse(time.RFC3339, f[3]); err == nil {
			c.CommittedAt = ts
		}
		commits = append(commits, c)
	}
	return commits
}

// injectCredential embeds a token into an https URL as a basic-auth userinfo so a
// non-interactive `git` can read a private repo. For non-https URLs (ssh / scp) or
// an empty credential it returns the url unchanged (ssh auth rides the agent/keys).
func injectCredential(rawURL, credential string) string {
	cred := strings.TrimSpace(credential)
	if cred == "" {
		return rawURL
	}
	const httpsPrefix = "https://"
	if !strings.HasPrefix(rawURL, httpsPrefix) {
		return rawURL
	}
	rest := strings.TrimPrefix(rawURL, httpsPrefix)
	if strings.Contains(rest, "@") { // url already carries userinfo — don't double it
		return rawURL
	}
	// x-access-token is the conventional username for a token-as-password (GitHub/
	// GitLab both accept it); the token is the password.
	return httpsPrefix + "x-access-token:" + cred + "@" + rest
}

// execGitRunner is the production gitRunner over real `git`.
type execGitRunner struct{}

func (execGitRunner) LsRemoteHeads(ctx context.Context, authURL string) (string, error) {
	cmd := exec.CommandContext(ctx, "git", "ls-remote", "--heads", authURL)
	cmd.Env = gitNonInteractiveEnv()
	out, err := cmd.Output()
	if err != nil {
		return "", err
	}
	return string(out), nil
}

func (execGitRunner) ShallowLog(ctx context.Context, authURL, branch string, limit int) (string, error) {
	dir, err := os.MkdirTemp("", "coderepo-shallow-*")
	if err != nil {
		return "", err
	}
	defer os.RemoveAll(dir) // throwaway: no working clone survives this call

	steps := [][]string{
		{"init", "-q", dir},
		{"-C", dir, "remote", "add", "origin", authURL},
		{"-C", dir, "fetch", "-q", "--depth", fmt.Sprint(limit), "origin", branch},
	}
	for _, args := range steps {
		cmd := exec.CommandContext(ctx, "git", args...)
		cmd.Env = gitNonInteractiveEnv()
		if out, err := cmd.CombinedOutput(); err != nil {
			return "", fmt.Errorf("git %s: %w: %s", strings.Join(args, " "), err, strings.TrimSpace(string(out)))
		}
	}
	logCmd := exec.CommandContext(ctx, "git", "-C", dir, "log",
		"--format="+gitLogFormat, "--max-count", fmt.Sprint(limit), "FETCH_HEAD")
	logCmd.Env = gitNonInteractiveEnv()
	out, err := logCmd.Output()
	if err != nil {
		return "", err
	}
	return string(out), nil
}

// gitNonInteractiveEnv disables credential/SSH prompts so a fetch fails fast
// instead of hanging on a missing credential.
func gitNonInteractiveEnv() []string {
	return append(os.Environ(),
		"GIT_TERMINAL_PROMPT=0",
		"GIT_SSH_COMMAND=ssh -oBatchMode=yes",
	)
}
