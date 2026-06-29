package provider

import (
	"context"
	"encoding/base64"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"regexp"
	"strings"
	"time"
)

// Git is the generic fallback adapter for any non-github provider (gitlab,
// self-hosted, "git", or unknown): branches via `git ls-remote --heads` and
// commits via a throwaway SHALLOW fetch + `git log` — never a full/working clone
// (issue-f980c8de: "不 clone"). All git invocations go through the gitRunner seam
// so tests fake them without a network/binary.
//
// SECURITY (v2.18.4 BE-2 review): a private-repo token must NEVER appear in process
// argv (/proc/<pid>/cmdline is world-readable — a forked executor could read it) nor
// in any returned error (errors propagate to the API/agent surface). So the clean
// URL goes in argv and the token rides an env-injected http.extraHeader
// (GIT_CONFIG_*), and every error returned from this adapter is run through
// redactSecrets as a defense-in-depth backstop against git echoing a credential.
type Git struct {
	runner gitRunner
}

// NewGit builds the production git fallback (real `git` over os/exec + a temp dir).
func NewGit() *Git {
	return &Git{runner: execGitRunner{}}
}

// gitRunner is the seam over the two git operations the fallback needs. The token
// is passed SEPARATELY from the (clean) url so the runner can keep it out of argv.
type gitRunner interface {
	// LsRemoteHeads runs `git ls-remote --heads <url>` (token via env) → stdout.
	LsRemoteHeads(ctx context.Context, url, token string) (string, error)
	// ShallowLog shallow-fetches `branch` (depth=limit) into a throwaway dir (token
	// via env) and returns `git log` stdout in the gitLogFormat layout.
	ShallowLog(ctx context.Context, url, token, branch string, limit int) (string, error)
}

// ListBranches parses `git ls-remote --heads`. Each line is "<sha>\trefs/heads/<name>".
func (g *Git) ListBranches(ctx context.Context, t Target) ([]Branch, error) {
	out, err := g.runner.LsRemoteHeads(ctx, t.URL, t.Credential)
	if err != nil {
		return nil, redactErr(fmt.Errorf("provider/git: ls-remote %s: %w", t.URL, err))
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
	out, err := g.runner.ShallowLog(ctx, t.URL, t.Credential, branch, n)
	if err != nil {
		return nil, redactErr(fmt.Errorf("provider/git: shallow log %s@%s: %w", t.URL, branch, err))
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

// --- secret redaction (defense-in-depth) ------------------------------------

var (
	// userinfoTokenRe matches a token embedded as https userinfo (user:token@), in
	// case a url ever carries one or git echoes one.
	userinfoTokenRe = regexp.MustCompile(`(//[^/:@\s]+:)[^@/\s]+@`)
	// authHeaderRe matches an Authorization header value (our extraHeader form).
	authHeaderRe = regexp.MustCompile(`(?i)(authorization:\s*\S+\s+)\S+`)
)

// redactSecrets scrubs any credential that might have leaked into a string (a git
// stderr echo, a misformed url). It is a backstop — the token is injected via env,
// not argv/url, so it should not appear, but errors flow to the API/agent so we
// never risk it.
func redactSecrets(s string) string {
	s = userinfoTokenRe.ReplaceAllString(s, "$1***@")
	s = authHeaderRe.ReplaceAllString(s, "${1}***")
	return s
}

// redactErr returns an error whose text is redacted. The redacted error is plain
// text (the underlying chain is not errors.Is-matched by any caller — these surface
// as messages), so flattening to a redacted string is safe.
func redactErr(err error) error {
	if err == nil {
		return nil
	}
	return errors.New(redactSecrets(err.Error()))
}

// --- production git runner ---------------------------------------------------

// execGitRunner is the production gitRunner over real `git`.
type execGitRunner struct{}

// argv builders — pure + token-free BY CONSTRUCTION. The token is never a parameter
// here, so it can never reach argv (the security invariant TestGitArgs_NeverCarryToken
// pins); auth rides gitAuthEnv (env) instead.
func gitLsRemoteArgs(url string) []string {
	return []string{"ls-remote", "--heads", url}
}

func gitFetchArgs(dir, url, branch string, limit int) []string {
	return []string{"-C", dir, "fetch", "-q", "--depth", fmt.Sprint(limit), url, branch}
}

func (execGitRunner) LsRemoteHeads(ctx context.Context, url, token string) (string, error) {
	cmd := exec.CommandContext(ctx, "git", gitLsRemoteArgs(url)...)
	cmd.Env = gitAuthEnv(token)
	out, err := cmd.Output()
	if err != nil {
		return "", redactErr(err)
	}
	return string(out), nil
}

func (execGitRunner) ShallowLog(ctx context.Context, url, token, branch string, limit int) (string, error) {
	dir, err := os.MkdirTemp("", "coderepo-shallow-*")
	if err != nil {
		return "", err
	}
	defer os.RemoveAll(dir) // throwaway: no working clone survives this call

	// Fetch the branch directly from the (clean) url — no `remote add`, so the url is
	// the only place it appears and the token is never in any argv.
	steps := [][]string{
		{"init", "-q", dir},
		gitFetchArgs(dir, url, branch, limit),
	}
	for _, args := range steps {
		cmd := exec.CommandContext(ctx, "git", args...)
		cmd.Env = gitAuthEnv(token)
		if out, err := cmd.CombinedOutput(); err != nil {
			// args carry only the CLEAN url; redact the combined output as a backstop.
			return "", redactErr(fmt.Errorf("git %s: %w: %s", strings.Join(args, " "), err, strings.TrimSpace(string(out))))
		}
	}
	logCmd := exec.CommandContext(ctx, "git", "-C", dir, "log",
		"--format="+gitLogFormat, "--max-count", fmt.Sprint(limit), "FETCH_HEAD")
	logCmd.Env = gitAuthEnv(token)
	out, err := logCmd.Output()
	if err != nil {
		return "", redactErr(err)
	}
	return string(out), nil
}

// gitAuthEnv builds the child env: non-interactive (no prompts) + the token (when
// present) injected as an http.extraHeader Authorization via GIT_CONFIG_* env. The
// token therefore lives in the child's ENVIRONMENT (/proc/<pid>/environ — readable
// only by the same uid), NEVER in argv (/proc/<pid>/cmdline — world-readable). The
// header form (Basic base64("x-access-token:<token>")) is what GitHub/GitLab accept
// for token-as-password over https.
func gitAuthEnv(token string) []string {
	env := append(os.Environ(),
		"GIT_TERMINAL_PROMPT=0",
		"GIT_SSH_COMMAND=ssh -oBatchMode=yes",
	)
	if strings.TrimSpace(token) != "" {
		basic := base64.StdEncoding.EncodeToString([]byte("x-access-token:" + token))
		env = append(env,
			"GIT_CONFIG_COUNT=1",
			"GIT_CONFIG_KEY_0=http.extraHeader",
			"GIT_CONFIG_VALUE_0=Authorization: Basic "+basic,
		)
	}
	return env
}
