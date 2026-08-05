package executor

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// EvidenceArtifact is runtime-authored, durable proof for an evidence_only task.
// The model cannot forge its digest or forget to commit it: Finalize owns both.
type EvidenceArtifact struct {
	TaskID      string   `json:"task_id"`
	ExecutionID string   `json:"execution_id"`
	ReviewedSHA string   `json:"reviewed_sha"`
	BaseSHA     string   `json:"base_sha"`
	Commands    []string `json:"commands"`
	ExitStatus  int      `json:"exit_status"`
	Verdict     string   `json:"verdict"`
	Summary     string   `json:"summary"`
	Digest      string   `json:"artifact_digest"`
	Path        string   `json:"path"`
	Branch      string   `json:"branch"`
	Pushed      bool     `json:"pushed"`
	Error       string   `json:"error,omitempty"`
}

func artifactDigest(a *EvidenceArtifact) string {
	unsigned, _ := json.Marshal(struct {
		TaskID, ExecutionID, ReviewedSHA, BaseSHA string
		Commands                                  []string
		ExitStatus                                int
		Verdict, Summary, Path                    string
	}{a.TaskID, a.ExecutionID, a.ReviewedSHA, a.BaseSHA, a.Commands, a.ExitStatus, a.Verdict, a.Summary, a.Path})
	sum := sha256.Sum256(unsigned)
	return "sha256:" + hex.EncodeToString(sum[:])
}

func (m *Monitor) materializeEvidence(ctx context.Context, c Completion) Completion {
	in, err := m.fx.ReadInput(c.ExecutorID)
	if err != nil || EffectiveDeliveryContract(in.DeliveryContract) != DeliveryContractEvidenceOnly {
		return c
	}
	artifact := &EvidenceArtifact{TaskID: in.Source.TaskRef, ExecutionID: c.ExecutorID}
	c.Evidence = artifact
	if c.Git == nil || !c.Git.Probed {
		return m.evidenceNonDelivery(c, "evidence artifact requires a probed git worktree")
	}
	if m.tracker == nil {
		return m.evidenceNonDelivery(c, "executor record tracker unavailable")
	}
	rec, err := m.tracker.Read(c.ExecutorID)
	if err != nil {
		return m.evidenceNonDelivery(c, "read executor record: "+err.Error())
	}
	artifact.ReviewedSHA, artifact.BaseSHA = c.Git.HeadSHA, rec.BaseRef
	artifact.Commands = append([]string(nil), rec.RunnerCmd...)
	artifact.ExitStatus = 1
	artifact.Verdict = "fail"
	if c.Kind == OutcomeSucceeded {
		artifact.ExitStatus, artifact.Verdict = 0, "pass"
	}
	if c.Status != nil {
		artifact.Summary = strings.TrimSpace(c.Status.Summary)
	}
	if artifact.Summary == "" && c.Output != nil {
		artifact.Summary = strings.TrimSpace(c.Output.Result)
	}
	if artifact.Summary == "" && c.Error != nil {
		artifact.Summary = c.Error.Message
	}
	artifact.Path = filepath.ToSlash(filepath.Join(".agent-center", "evidence", artifact.TaskID, artifact.ExecutionID+".json"))
	artifact.Branch = c.Git.Branch
	artifact.Digest = artifactDigest(artifact)
	ws, err := m.fx.Layout().WorkspaceDir(c.ExecutorID)
	if err != nil {
		return m.evidenceNonDelivery(c, err.Error())
	}
	abs := filepath.Join(ws, filepath.FromSlash(artifact.Path))
	if err := os.MkdirAll(filepath.Dir(abs), 0o755); err != nil {
		return m.evidenceNonDelivery(c, err.Error())
	}
	// A retained artifact is authoritative on retry: HEAD now points at the evidence
	// commit, not the source SHA it reviewed. Reusing the first body prevents a second,
	// self-referential evidence commit.
	reused := false
	if old, readErr := os.ReadFile(abs); readErr == nil {
		var prior EvidenceArtifact
		subject, _ := m.git.Run(ctx, ws, gitNetworkEnv(), "log", "-1", "--format=%s")
		parent, _ := m.git.Run(ctx, ws, gitNetworkEnv(), "rev-parse", "HEAD^")
		if json.Unmarshal(old, &prior) == nil && prior.TaskID == artifact.TaskID && prior.ExecutionID == artifact.ExecutionID &&
			prior.Path == artifact.Path && prior.Digest == artifactDigest(&prior) && strings.TrimSpace(subject) == "chore(evidence): persist "+artifact.TaskID+" verification" && strings.TrimSpace(parent) == prior.ReviewedSHA {
			artifact = &prior
			c.Evidence = artifact
			reused = true
		}
	}
	if !reused {
		body, _ := json.MarshalIndent(artifact, "", "  ")
		body = append(body, '\n')
		if err := os.WriteFile(abs, body, 0o644); err != nil {
			return m.evidenceNonDelivery(c, err.Error())
		}
	}
	if _, err := m.git.Run(ctx, ws, gitNetworkEnv(), "add", "--", artifact.Path); err != nil {
		return m.evidenceNonDelivery(c, "git add evidence: "+err.Error())
	}
	if out, err := m.git.Run(ctx, ws, gitNetworkEnv(), "diff", "--cached", "--quiet", "--", artifact.Path); err != nil {
		if _, cerr := m.git.Run(ctx, ws, gitNetworkEnv(), "-c", "user.name=agent-center runtime", "-c", "user.email=runtime@agent-center.local", "commit", "-m", "chore(evidence): persist "+artifact.TaskID+" verification", "--", artifact.Path); cerr != nil {
			return m.evidenceNonDelivery(c, fmt.Sprintf("commit evidence: %v: %s", cerr, strings.TrimSpace(out)))
		}
	}
	// Re-probe after the runtime commit, then use the same guarded unique-branch push.
	gs := probeGitStatus(ctx, m.git, ws, rec.BaseRef)
	c.Git = &gs
	pushed, err := m.eagerSupervisorPush(ctx, c)
	if err != nil || !pushed {
		if err == nil {
			err = fmt.Errorf("evidence branch was not pushed")
		}
		artifact.Error = err.Error()
		return m.evidenceNonDelivery(c, artifact.Error)
	}
	c.Git.Pushed, artifact.Pushed = true, true
	return c
}

func (m *Monitor) evidenceNonDelivery(c Completion, reason string) Completion {
	if c.Evidence != nil {
		c.Evidence.Error = reason
	}
	// A red verification remains a real failed verdict; artifact persistence errors are
	// appended so writeback retains both facts. A green result may never pass without proof.
	if c.Kind != OutcomeSucceeded {
		if c.Error == nil {
			c.Error = &ErrorDetail{Kind: "evidence_persistence", Message: reason}
		} else {
			c.Error.Message += " — evidence persistence: " + reason
		}
		return c
	}
	c.Kind, c.Retryable = OutcomeCrashed, true
	c.Error = &ErrorDetail{Kind: "non_delivery", Message: "evidence_only result has no durable artifact: " + reason}
	return c
}
