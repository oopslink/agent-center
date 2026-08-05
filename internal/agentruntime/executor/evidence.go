package executor

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

type EvidenceCommand struct {
	Source     string `json:"source,omitempty"`
	ToolUseID  string `json:"tool_use_id,omitempty"`
	Command    string `json:"command"`
	ExitStatus int    `json:"exit_status"`
}

// EvidenceArtifact is runtime-authored, durable proof for an evidence_only task.
// The model cannot forge its digest or forget to commit it: Finalize owns both.
type EvidenceArtifact struct {
	TaskID                    string            `json:"task_id"`
	ExecutionID               string            `json:"execution_id"`
	ReviewedSHA               string            `json:"reviewed_sha"`
	BaseSHA                   string            `json:"base_sha"`
	Commands                  []EvidenceCommand `json:"commands,omitempty"`
	CommandsAvailable         bool              `json:"commands_available"`
	CommandsUnavailableReason string            `json:"commands_unavailable_reason,omitempty"`
	CommandEventPath          string            `json:"command_event_path,omitempty"`
	CommandEventDigest        string            `json:"command_event_digest,omitempty"`
	ExitStatus                int               `json:"exit_status"`
	Verdict                   string            `json:"verdict"`
	Summary                   string            `json:"summary"`
	Digest                    string            `json:"artifact_digest"`
	Path                      string            `json:"path"`
	Branch                    string            `json:"branch"`
	Error                     string            `json:"error,omitempty"`
}

func artifactDigest(a *EvidenceArtifact) string {
	unsigned, _ := json.Marshal(struct {
		TaskID, ExecutionID, ReviewedSHA, BaseSHA string
		Commands                                  []EvidenceCommand
		CommandsAvailable                         bool
		CommandsUnavailableReason                 string
		CommandEventPath, CommandEventDigest      string
		ExitStatus                                int
		Verdict, Summary, Path, Branch            string
	}{a.TaskID, a.ExecutionID, a.ReviewedSHA, a.BaseSHA, a.Commands, a.CommandsAvailable,
		a.CommandsUnavailableReason, a.CommandEventPath, a.CommandEventDigest,
		a.ExitStatus, a.Verdict, a.Summary, a.Path, a.Branch})
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
	baseRef := c.Git.BaseRef
	if baseRef == "" {
		baseRef = m.recordBaseRef(c.ExecutorID)
	}
	artifact.ReviewedSHA, artifact.BaseSHA = c.Git.HeadSHA, baseRef
	artifact.Commands, artifact.CommandsAvailable, artifact.CommandsUnavailableReason, artifact.CommandEventPath, artifact.CommandEventDigest = m.evidenceCommands(c.ExecutorID)
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
	ws, err := m.executorWorkspacePath(c.ExecutorID)
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
	gs := probeGitStatus(ctx, m.git, ws, baseRef)
	c.Git = &gs
	pushed, err := m.eagerSupervisorPush(ctx, c)
	if err != nil || !pushed {
		if err == nil {
			err = fmt.Errorf("evidence branch was not pushed")
		}
		artifact.Error = err.Error()
		return m.evidenceNonDelivery(c, artifact.Error)
	}
	c.Git.Pushed = true
	return c
}

func (m *Monitor) evidenceCommands(executorID string) ([]EvidenceCommand, bool, string, string, string) {
	sourcePath := "executor://" + executorID + "/" + commandEventsFileName
	events, raw, err := m.fx.ReadCommandEvents(executorID)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			if p, digest, ok := m.progressDigest(executorID); ok {
				return nil, false, commandEventsFileName + " unavailable; attached progress event digest instead", p, digest
			}
			return nil, false, commandEventsFileName + " unavailable", sourcePath, bytesDigest(nil)
		}
		return nil, false, err.Error(), sourcePath, bytesDigest(raw)
	}
	commands, ok, reason := evidenceCommandsFromEvents(events)
	if !ok {
		return commands, false, reason, sourcePath, bytesDigest(raw)
	}
	return commands, true, "", sourcePath, bytesDigest(raw)
}

func (m *Monitor) progressDigest(executorID string) (string, string, bool) {
	p, err := m.fx.Layout().ProgressPath(executorID)
	if err != nil {
		return "", "", false
	}
	b, err := os.ReadFile(p)
	if err != nil {
		return "", "", false
	}
	return "executor://" + executorID + "/" + progressFileName, bytesDigest(b), true
}

func evidenceCommandsFromEvents(events []CommandExecutionEvent) ([]EvidenceCommand, bool, string) {
	if len(events) == 0 {
		return nil, false, "no command execution events captured"
	}
	type startedCommand struct {
		command string
		source  string
	}
	started := map[string]startedCommand{}
	finished := map[string]bool{}
	var commands []EvidenceCommand
	var incomplete []string
	for _, ev := range events {
		id := strings.TrimSpace(ev.ToolUseID)
		switch ev.Type {
		case commandEventStarted:
			if id != "" {
				started[id] = startedCommand{command: strings.TrimSpace(ev.Command), source: ev.Source}
			}
		case commandEventFinished:
			cmd := strings.TrimSpace(ev.Command)
			source := ev.Source
			if st, ok := started[id]; ok {
				if cmd == "" {
					cmd = st.command
				}
				if source == "" {
					source = st.source
				}
			}
			if cmd == "" {
				incomplete = append(incomplete, id+":missing command")
				if id != "" {
					finished[id] = true
				}
				continue
			}
			if !ev.ExitStatusAvailable || ev.ExitStatus == nil {
				incomplete = append(incomplete, cmd+":missing exit_status")
				if id != "" {
					finished[id] = true
				}
				continue
			}
			commands = append(commands, EvidenceCommand{
				Source:     source,
				ToolUseID:  id,
				Command:    cmd,
				ExitStatus: *ev.ExitStatus,
			})
			finished[id] = true
		}
	}
	for id, st := range started {
		if !finished[id] {
			label := st.command
			if label == "" {
				label = id
			}
			incomplete = append(incomplete, label+":missing completion")
		}
	}
	if len(incomplete) > 0 {
		return commands, false, "incomplete command event capture: " + strings.Join(incomplete, "; ")
	}
	if len(commands) == 0 {
		return nil, false, "no completed command executions captured"
	}
	return commands, true, ""
}

func bytesDigest(b []byte) string {
	sum := sha256.Sum256(b)
	return "sha256:" + hex.EncodeToString(sum[:])
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
