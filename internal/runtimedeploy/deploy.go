package runtimedeploy

import (
	"context"
	"errors"
	"fmt"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"time"
)

const CommandType = "runtime.deploy_restart"

type Request struct {
	AgentID   string `json:"agent_id"`
	RepoURL   string `json:"repo_url"`
	TargetRef string `json:"target_ref"`
	TargetSHA string `json:"target_sha"`
	BaseRef   string `json:"base_ref,omitempty"`
	Mode      string `json:"mode,omitempty"`
	Prefix    string `json:"prefix,omitempty"`
	TimeoutMS int    `json:"timeout_ms,omitempty"`

	VerifiedTargetSHA string `json:"verified_target_sha,omitempty"`
	VerifiedBaseSHA   string `json:"verified_base_sha,omitempty"`
	VerifiedAt        string `json:"verified_at,omitempty"`
}

type VerifiedRef struct {
	TargetSHA string
	BaseSHA   string
}

type Result struct {
	TargetSHA string `json:"target_sha"`
	Mode      string `json:"mode"`
	Output    string `json:"output,omitempty"`
}

var fullSHARe = regexp.MustCompile(`^[0-9a-fA-F]{40}$`)

func VerifyRemote(ctx context.Context, req Request) (VerifiedRef, error) {
	repoURL := strings.TrimSpace(req.RepoURL)
	targetRef := normalizeRef(req.TargetRef)
	baseRef := normalizeRef(req.BaseRef)
	targetSHA := strings.ToLower(strings.TrimSpace(req.TargetSHA))
	if repoURL == "" {
		return VerifiedRef{}, errors.New("repo_url required")
	}
	if _, err := url.Parse(repoURL); err != nil {
		return VerifiedRef{}, fmt.Errorf("invalid repo_url: %w", err)
	}
	if targetRef == "" {
		return VerifiedRef{}, errors.New("target_ref required")
	}
	if baseRef == "" {
		baseRef = "refs/heads/main"
	}
	if !fullSHARe.MatchString(targetSHA) {
		return VerifiedRef{}, errors.New("target_sha must be a full 40 character commit SHA")
	}
	dir, err := os.MkdirTemp("", "ac-runtime-deploy-verify-*")
	if err != nil {
		return VerifiedRef{}, err
	}
	defer os.RemoveAll(dir)
	if out, err := runGit(ctx, dir, "init"); err != nil {
		return VerifiedRef{}, fmt.Errorf("git init: %w: %s", err, trimOutput(out))
	}
	if out, err := runGit(ctx, dir, "remote", "add", "origin", repoURL); err != nil {
		return VerifiedRef{}, fmt.Errorf("git remote add: %w: %s", err, trimOutput(out))
	}
	refspecs := []string{
		"+" + targetRef + ":refs/ac-runtime-deploy/target",
		"+" + baseRef + ":refs/ac-runtime-deploy/base",
	}
	args := append([]string{"fetch", "--no-tags", "origin"}, refspecs...)
	if out, err := runGit(ctx, dir, args...); err != nil {
		return VerifiedRef{}, fmt.Errorf("git fetch verified refs: %w: %s", err, trimOutput(out))
	}
	resolvedTarget, err := gitRevParse(ctx, dir, "refs/ac-runtime-deploy/target^{commit}")
	if err != nil {
		return VerifiedRef{}, err
	}
	resolvedBase, err := gitRevParse(ctx, dir, "refs/ac-runtime-deploy/base^{commit}")
	if err != nil {
		return VerifiedRef{}, err
	}
	if !strings.EqualFold(resolvedTarget, targetSHA) {
		return VerifiedRef{}, fmt.Errorf("target_ref resolves to %s, not requested target_sha %s", resolvedTarget, targetSHA)
	}
	if out, err := runGit(ctx, dir, "merge-base", "--is-ancestor", resolvedBase, resolvedTarget); err != nil {
		return VerifiedRef{}, fmt.Errorf("base_ref %s (%s) is not an ancestor of target_sha %s: %w: %s",
			baseRef, resolvedBase, resolvedTarget, err, trimOutput(out))
	}
	return VerifiedRef{TargetSHA: strings.ToLower(resolvedTarget), BaseSHA: strings.ToLower(resolvedBase)}, nil
}

func normalizeRef(ref string) string {
	ref = strings.TrimSpace(ref)
	if ref == "" || strings.HasPrefix(ref, "refs/") {
		return ref
	}
	return "refs/heads/" + ref
}

func gitRevParse(ctx context.Context, dir, rev string) (string, error) {
	out, err := runGit(ctx, dir, "rev-parse", rev)
	if err != nil {
		return "", fmt.Errorf("git rev-parse %s: %w: %s", rev, err, trimOutput(out))
	}
	sha := strings.TrimSpace(string(out))
	if !fullSHARe.MatchString(sha) {
		return "", fmt.Errorf("git rev-parse %s returned non-sha %q", rev, sha)
	}
	return sha, nil
}

func runGit(ctx context.Context, dir string, args ...string) ([]byte, error) {
	cmd := exec.CommandContext(ctx, "git", args...)
	cmd.Dir = dir
	cmd.Env = append(os.Environ(),
		"GIT_TERMINAL_PROMPT=0",
		"GIT_CONFIG_GLOBAL=/dev/null",
		"GIT_CONFIG_SYSTEM=/dev/null",
	)
	return cmd.CombinedOutput()
}

func trimOutput(out []byte) string {
	s := strings.TrimSpace(string(out))
	if len(s) > 4000 {
		s = s[:4000]
	}
	return s
}

func Timeout(req Request, fallback time.Duration) time.Duration {
	if req.TimeoutMS <= 0 {
		return fallback
	}
	return time.Duration(req.TimeoutMS) * time.Millisecond
}

func ManagedSourceDir(root, sha string) string {
	return filepath.Join(root, "runtime-deploy", sha)
}
