package agentruntime

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"
)

const interruptedConverseFile = "interrupted_converse.json"

type interruptedConverseRecord struct {
	Request    ConverseRequest `json:"request"`
	Attempts   int             `json:"attempts"`
	AcceptedAt time.Time       `json:"accepted_at"`
	UpdatedAt  time.Time       `json:"updated_at"`
}

func (r *LocalRuntime) interruptedConversePath(agentID string) (string, error) {
	if strings.TrimSpace(r.cfg.AgentHomeBase) == "" {
		return "", nil
	}
	home, _, _, err := r.agentPaths(agentID)
	if err != nil {
		return "", err
	}
	return filepath.Join(home, interruptedConverseFile), nil
}

func (r *LocalRuntime) persistInterruptedConverse(agentID string, req ConverseRequest) error {
	if strings.TrimSpace(req.ConversationID) == "" || strings.TrimSpace(req.MessageID) == "" {
		return nil
	}
	path, err := r.interruptedConversePath(agentID)
	if err != nil {
		return err
	}
	if path == "" {
		return nil
	}
	now := r.now()
	rec := interruptedConverseRecord{
		Request:    req,
		Attempts:   0,
		AcceptedAt: now.UTC(),
		UpdatedAt:  now.UTC(),
	}
	return writeInterruptedConverseFile(path, rec)
}

func (r *LocalRuntime) clearInterruptedConverse(agentID string) {
	path, err := r.interruptedConversePath(agentID)
	if err != nil {
		return
	}
	if path == "" {
		return
	}
	if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
		r.log("agent=%s interrupted-converse clear: %v", agentID, err)
	}
}

// RecoverInterruptedConverse replays one accepted-but-not-cleanly-finished
// agent.converse turn after an agent-runtime process restart. A second interrupted
// replay surfaces a visible system notice instead of looping silently forever.
func (r *LocalRuntime) RecoverInterruptedConverse(ctx context.Context) error {
	agentID := r.cfg.AgentID
	path, err := r.interruptedConversePath(agentID)
	if err != nil {
		return err
	}
	if path == "" {
		return nil
	}
	rec, ok := readInterruptedConverseFile(path)
	if !ok {
		return nil
	}
	req := rec.Request
	if strings.TrimSpace(req.ConversationID) == "" || strings.TrimSpace(req.MessageID) == "" {
		r.clearInterruptedConverse(agentID)
		return nil
	}
	if rec.Attempts >= 1 {
		summary := "agent turn was interrupted during runtime restart and retry already failed"
		if r.cfg.Reporter != nil {
			if err := r.cfg.Reporter.ReportConverseError(ctx, agentID, req.ConversationID, summary, r.now()); err != nil {
				r.log("agent=%s interrupted-converse visible failure conv=%s: %v", agentID, req.ConversationID, err)
				return err
			}
		}
		r.clearInterruptedConverse(agentID)
		r.log("agent=%s interrupted-converse conv=%s message=%s retry spent — visible failure posted", agentID, req.ConversationID, req.MessageID)
		return nil
	}

	rec.Attempts++
	rec.UpdatedAt = r.now().UTC()
	if err := writeInterruptedConverseFile(path, rec); err != nil {
		return err
	}

	r.mu.Lock()
	sess := r.state.Session
	r.mu.Unlock()
	if sess == nil {
		return fmt.Errorf("agent=%s interrupted-converse recover: no running session", agentID)
	}

	if r.cfg.Reporter != nil {
		payload := interruptedConversePayload(req, rec.Attempts)
		if err := r.cfg.Reporter.ReportAgentActivity(ctx, agentID, "agent_turn_interrupted", payload, "", "", r.now()); err != nil {
			r.log("agent=%s interrupted-converse activity report: %v", agentID, err)
		}
	}
	if err := sess.Inject(ctx, BuildConverseBrief(req)); err != nil {
		r.signalFatalIfSessionClosed("interrupted_converse recover", err)
		return fmt.Errorf("agent=%s interrupted-converse recover inject: %w", agentID, err)
	}
	r.recordWake(req.MessageID)
	r.mu.Lock()
	r.state.CurrentConversationID = req.ConversationID
	r.state.CurrentTaskID = ""
	r.mu.Unlock()
	r.log("agent=%s interrupted-converse conv=%s message=%s replayed attempt=%d", agentID, req.ConversationID, req.MessageID, rec.Attempts)
	return nil
}

func interruptedConversePayload(req ConverseRequest, attempt int) string {
	p := map[string]any{
		"type":            "agent_turn_interrupted",
		"action":          "replay",
		"conversation_id": req.ConversationID,
		"message_id":      req.MessageID,
		"attempt":         attempt,
	}
	b, err := json.Marshal(p)
	if err != nil {
		return `{"type":"agent_turn_interrupted","action":"replay"}`
	}
	return string(b)
}

func readInterruptedConverseFile(path string) (interruptedConverseRecord, bool) {
	var rec interruptedConverseRecord
	if path == "" {
		return rec, false
	}
	b, err := os.ReadFile(path)
	if err != nil {
		return rec, false
	}
	if err := json.Unmarshal(b, &rec); err != nil {
		return interruptedConverseRecord{}, false
	}
	return rec, true
}

func writeInterruptedConverseFile(path string, rec interruptedConverseRecord) error {
	if path == "" {
		return nil
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	b, err := json.Marshal(rec)
	if err != nil {
		return err
	}
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, b, 0o600); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}
