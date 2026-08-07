package cli

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"time"

	"github.com/oopslink/agent-center/internal/admin/clienttransport"
	"github.com/oopslink/agent-center/internal/config"
)

// WorkerRecoverDeliveryCommand is the operator-facing manual recovery delivery path.
// It runs on the worker/operator machine, optionally commits/pushes a retained worktree,
// then reports a manual_recovery delivery through the same report_delivery agent-tool the
// runtime uses. The center persists/audits the result; it never performs git operations.
func WorkerRecoverDeliveryCommand() *Command {
	return &Command{
		Name:    "recover-delivery",
		Summary: "Register a manually recovered pushed delivery for a task (worker/runtime side)",
		LongHelp: "Validates a local git worktree, optionally commits/pushes it, then calls " +
			"the worker-authenticated report_delivery path with source=manual_recovery. " +
			"The center only records the resulting delivery; git operations happen here.",
		Flags: func(fs *flag.FlagSet) Handler {
			cfgPath := fs.String("config", "", "path to agent-center.yaml")
			workerID := fs.String("worker-id", "", "worker identity used to discover config")
			agentID := fs.String("agent-id", "", "agent identity that owns the task (required)")
			taskID := fs.String("task-id", "", "task id to register delivery for (required)")
			executorID := fs.String("executor-id", "", "dead/exhausted executor id being recovered, when known")
			worktree := fs.String("worktree", "", "retained/recovered git worktree path (required)")
			baseRef := fs.String("base-ref", "origin/main", "base ref used to prove HEAD is ahead")
			branch := fs.String("branch", "", "delivery branch/ref; defaults to current branch")
			sha := fs.String("sha", "", "expected HEAD SHA; defaults to current HEAD")
			remote := fs.String("remote", "origin", "git remote used for push/remote verification")
			evidence := fs.String("evidence", "", "test/evidence summary (required)")
			reason := fs.String("reason", "", "manual recovery reason (required)")
			commitMsg := fs.String("commit-message", "", "optional commit message before registering")
			commitAll := fs.Bool("commit-all", false, "with --commit-message, stage all changes before commit")
			push := fs.Bool("push", false, "push HEAD to the delivery branch before registering")
			dryRun := fs.Bool("dry-run", false, "print the report_delivery payload without sending it")
			adminToken := fs.String("admin-token", "", "admin bearer token; falls back to worker.token / AGENT_CENTER_ADMIN_TOKEN")
			adminTarget := fs.String("admin-target", "", "admin endpoint; falls back to worker.bootstrap / server.admin_socket_path")
			bootstrap := fs.String("bootstrap", "", "admin endpoint URL (alias of --admin-target)")
			token := fs.String("token", "", "admin bearer token (alias of --admin-token)")
			serverFingerprint := fs.String("server-fingerprint", "", "pinned server cert fingerprint; falls back to worker.server_fingerprint")
			return func(ctx context.Context, args []string, out, errw io.Writer) ExitCode {
				req := recoverDeliveryRequest{
					ConfigPath: *cfgPath, WorkerID: *workerID, AgentID: *agentID, TaskID: *taskID,
					ExecutorID: *executorID, Worktree: *worktree, BaseRef: *baseRef, Branch: *branch,
					SHA: *sha, Remote: *remote, Evidence: *evidence, Reason: *reason,
					CommitMessage: *commitMsg, CommitAll: *commitAll, Push: *push, DryRun: *dryRun,
					AdminToken: coalesceWorkerFlag(*token, *adminToken), AdminTarget: coalesceWorkerFlag(*bootstrap, *adminTarget),
					ServerFingerprint: *serverFingerprint,
				}
				if err := runRecoverDelivery(ctx, req, out); err != nil {
					fmt.Fprintf(errw, "Error: worker recover-delivery: %v\n", err)
					return ExitBusinessError
				}
				return ExitOK
			}
		},
	}
}

type recoverDeliveryRequest struct {
	ConfigPath, WorkerID, AgentID, TaskID, ExecutorID string
	Worktree, BaseRef, Branch, SHA, Remote            string
	Evidence, Reason                                  string
	CommitMessage                                     string
	CommitAll, Push, DryRun                           bool
	AdminToken, AdminTarget, ServerFingerprint        string
}

func runRecoverDelivery(ctx context.Context, req recoverDeliveryRequest, out io.Writer) error {
	if strings.TrimSpace(req.AgentID) == "" {
		return fmt.Errorf("--agent-id is required")
	}
	if strings.TrimSpace(req.TaskID) == "" {
		return fmt.Errorf("--task-id is required")
	}
	if strings.TrimSpace(req.Worktree) == "" {
		return fmt.Errorf("--worktree is required")
	}
	if strings.TrimSpace(req.Reason) == "" {
		return fmt.Errorf("--reason is required")
	}
	if strings.TrimSpace(req.Evidence) == "" {
		return fmt.Errorf("--evidence is required")
	}
	wt := strings.TrimSpace(req.Worktree)
	if st, err := os.Stat(wt); err != nil {
		return fmt.Errorf("stat worktree: %w", err)
	} else if !st.IsDir() {
		return fmt.Errorf("worktree is not a directory: %s", wt)
	}
	if req.CommitMessage != "" {
		if req.CommitAll {
			if _, err := gitOutput(ctx, wt, "add", "-A"); err != nil {
				return fmt.Errorf("git add -A: %w", err)
			}
		}
		if _, err := gitOutput(ctx, wt, "commit", "-m", req.CommitMessage); err != nil {
			return fmt.Errorf("git commit: %w", err)
		}
	}
	branch := strings.TrimSpace(req.Branch)
	if branch == "" {
		b, err := gitOutput(ctx, wt, "rev-parse", "--abbrev-ref", "HEAD")
		if err != nil {
			return fmt.Errorf("resolve branch: %w", err)
		}
		branch = strings.TrimSpace(b)
	}
	if branch == "" || branch == "HEAD" {
		return fmt.Errorf("delivery branch is required for detached HEAD (pass --branch)")
	}
	head, err := gitOutput(ctx, wt, "rev-parse", "HEAD")
	if err != nil {
		return fmt.Errorf("resolve HEAD: %w", err)
	}
	head = strings.TrimSpace(head)
	if want := strings.TrimSpace(req.SHA); want != "" && want != head {
		return fmt.Errorf("--sha %s does not match worktree HEAD %s", want, head)
	}
	if req.Push {
		ref := strings.TrimPrefix(branch, "refs/heads/")
		if _, err := gitOutput(ctx, wt, "push", strings.TrimSpace(req.Remote), "HEAD:refs/heads/"+ref); err != nil {
			return fmt.Errorf("git push: %w", err)
		}
	}
	delivery, err := probeManualDelivery(ctx, wt, branch, head, req.BaseRef, req.Remote)
	if err != nil {
		return err
	}
	payload := map[string]any{
		"agent_id":    strings.TrimSpace(req.AgentID),
		"task_id":     strings.TrimSpace(req.TaskID),
		"source":      "manual_recovery",
		"executor_id": strings.TrimSpace(req.ExecutorID),
		"worktree":    wt,
		"evidence":    strings.TrimSpace(req.Evidence),
		"reason":      strings.TrimSpace(req.Reason),
		"git":         delivery,
	}
	if req.DryRun {
		return json.NewEncoder(out).Encode(payload)
	}
	cfgPath := resolveWorkerConfigPath(req.ConfigPath, req.WorkerID)
	cfg, _ := config.Load(config.LoadOptions{Path: cfgPath})
	targetSpec := firstNonEmptyWorker(req.AdminTarget, cfg.Worker.Bootstrap)
	if targetSpec == "" {
		targetSpec = cfg.Server.AdminSocketPath
	}
	if targetSpec == "" {
		return fmt.Errorf("admin target not configured (--admin-target/--bootstrap or config)")
	}
	token := firstNonEmptyWorker(req.AdminToken, cfg.Worker.Token)
	if token == "" {
		token = strings.TrimSpace(os.Getenv("AGENT_CENTER_ADMIN_TOKEN"))
	}
	if token == "" {
		return fmt.Errorf("admin token not configured (--token/--admin-token, worker.token, or AGENT_CENTER_ADMIN_TOKEN)")
	}
	fingerprint := firstNonEmptyWorker(req.ServerFingerprint, cfg.Worker.ServerFingerprint)
	target, err := clienttransport.ParseTarget(targetSpec)
	if err != nil {
		return fmt.Errorf("parse admin target: %w", err)
	}
	client, err := NewClientFromTarget(target, fingerprint, 30*time.Second)
	if err != nil {
		return fmt.Errorf("admin client: %w", err)
	}
	client.WithToken(token)
	var resp map[string]any
	if err := client.postJSON(ctx, "/admin/agent-tools/report_delivery", payload, &resp); err != nil {
		return err
	}
	return json.NewEncoder(out).Encode(resp)
}

func probeManualDelivery(ctx context.Context, worktree, branch, head, baseRef, remote string) (map[string]any, error) {
	if strings.TrimSpace(remote) == "" {
		remote = "origin"
	}
	dirtyOut, err := gitOutput(ctx, worktree, "status", "--porcelain")
	if err != nil {
		return nil, fmt.Errorf("git status: %w", err)
	}
	baseKnown := false
	ahead := 0
	if b := strings.TrimSpace(baseRef); b != "" {
		if out, err := gitOutput(ctx, worktree, "rev-list", "--count", b+"..HEAD"); err == nil {
			if n, perr := strconv.Atoi(strings.TrimSpace(out)); perr == nil && n >= 0 {
				baseKnown = true
				ahead = n
			}
		}
	}
	pushed, pushErr := remoteBranchContains(ctx, worktree, remote, branch, head)
	payload := map[string]any{
		"branch":        branch,
		"head_sha":      head,
		"dirty":         strings.TrimSpace(dirtyOut) != "",
		"pushed":        pushed,
		"probed":        true,
		"base_ref":      strings.TrimSpace(baseRef),
		"base_known":    baseKnown,
		"ahead_of_base": ahead,
	}
	if pushErr != "" {
		payload["push_error"] = pushErr
	}
	return payload, nil
}

func remoteBranchContains(ctx context.Context, worktree, remote, branch, head string) (bool, string) {
	ref := strings.TrimPrefix(strings.TrimSpace(branch), "refs/heads/")
	out, err := gitOutput(ctx, worktree, "ls-remote", "--heads", strings.TrimSpace(remote), ref)
	if err != nil {
		return false, err.Error()
	}
	for _, line := range strings.Split(out, "\n") {
		fields := strings.Fields(line)
		if len(fields) >= 2 && fields[0] == head {
			return true, ""
		}
	}
	return false, fmt.Sprintf("%s does not point at HEAD %s on remote %s", ref, head, remote)
}

func gitOutput(ctx context.Context, dir string, args ...string) (string, error) {
	cmd := exec.CommandContext(ctx, "git", args...)
	cmd.Dir = dir
	cmd.Env = append(os.Environ(), "GIT_TERMINAL_PROMPT=0")
	out, err := cmd.CombinedOutput()
	if err != nil {
		return "", fmt.Errorf("%v: %w: %s", append([]string{"git"}, args...), err, strings.TrimSpace(string(out)))
	}
	return string(out), nil
}
