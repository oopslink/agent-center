package api

import (
	"context"
	"net/http"
	"time"

	agentbc "github.com/oopslink/agent-center/internal/agent"
	"github.com/oopslink/agent-center/internal/conversation"
	"github.com/oopslink/agent-center/internal/environment"
	"github.com/oopslink/agent-center/internal/files"
	pm "github.com/oopslink/agent-center/internal/projectmanager"
)

// v2.7 E1 #138 — Environment domain web surface (org-scoped READS).
//
// The Environment page shows the org's WORKERS from the CONTROL-CONNECTED view:
// these are environment.Worker ARs (created when a worker connects the control
// channel — D1, ADR-0050), carrying control-channel state (status / last-acked
// offset / heartbeat). This is DISTINCT from the Fleet page's workers segment,
// which derives from the legacy workforce.Worker (enrolled set). The two models
// are being converged in the workforce carve-out; until then the UI labels this
// page explicitly as the control-connected view so operators don't expect the
// full enrolled set here.
//
// Agents-on-worker are NOT a new endpoint: the page reuses the already org-scoped
// GET /api/agents (each Agent carries worker_id) and groups client-side.
// File-transfer sessions are slice-2 (#139).

// envWorkerMap serializes an environment.Worker (control-connected view) to JSON.
func envWorkerMap(w *environment.Worker) map[string]any {
	m := map[string]any{
		"worker_id":         string(w.ID()),
		"organization_id":   w.OrganizationID(),
		"name":              w.Name(),
		"status":            string(w.Status()), // online | offline (control-connection state)
		"last_acked_offset": w.LastAckedOffset(),
		"created_at":        w.CreatedAt().Format(time.RFC3339Nano),
		"updated_at":        w.UpdatedAt().Format(time.RFC3339Nano),
		"version":           w.Version(),
	}
	if hb := w.LastHeartbeatAt(); !hb.IsZero() {
		m["last_heartbeat_at"] = hb.Format(time.RFC3339Nano)
	}
	return m
}

// listWorkersHandler serves GET /api/workers — the org's control-connected
// workers. Org-scoped at the source via environment.WorkerRepository.ListByOrg,
// so a caller only ever sees their own org's workers (no cross-org leak).
func (s *Server) listWorkersHandler(w http.ResponseWriter, r *http.Request) {
	d := hd(r)
	if d.EnvWorkerRepo == nil {
		writeError(w, http.StatusNotImplemented, "env_workers_not_wired", "environment worker repo not wired")
		return
	}
	_, _, orgID, ok := requireOrgMember(w, r, d)
	if !ok {
		return
	}
	workers, err := d.EnvWorkerRepo.ListByOrg(r.Context(), orgID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "env_workers_error", err.Error())
		return
	}
	out := make([]map[string]any, 0, len(workers))
	for _, wk := range workers {
		out = append(out, envWorkerMap(wk))
	}
	writeJSON(w, http.StatusOK, map[string]any{"workers": out})
}

// getWorkerHandler serves GET /api/workers/{id} — one control-connected worker.
// Org isolation is enforced by FETCH-then-CHECK (not a scoped query): the worker
// is fetched by id and a cross-org (or unknown) id returns 404, so an attacker
// cannot probe another org's worker ids. (E-10b hard invariant.)
func (s *Server) getWorkerHandler(w http.ResponseWriter, r *http.Request) {
	d := hd(r)
	if d.EnvWorkerRepo == nil {
		writeError(w, http.StatusNotImplemented, "env_workers_not_wired", "environment worker repo not wired")
		return
	}
	_, _, orgID, ok := requireOrgMember(w, r, d)
	if !ok {
		return
	}
	wk, err := d.EnvWorkerRepo.FindByID(r.Context(), environment.WorkerID(r.PathValue("id")))
	if err != nil || wk.OrganizationID() != orgID {
		writeError(w, http.StatusNotFound, "not_found", "worker not found in this organization")
		return
	}
	writeJSON(w, http.StatusOK, envWorkerMap(wk))
}

// --- transfer sessions (#139 slice-2) ---------------------------------------
//
// The Environment page's in-flight file-transfer view. Transfer sessions have NO
// org column — org is resolved via the session's scope (task/issue/project/
// conversation/agent), FAIL-CLOSED: a session whose scope→org does not resolve to
// the caller's org (unknown entity / cross-org / tmp / unscoped) is EXCLUDED, not
// leaked. This mirrors the download gate's scope-switch (refReachableForHuman) but
// for the org-operator "what's transferring in my org" intent, so agent-scoped IS
// included (resolved agent→org), unlike the human-download gate.

// transferSessionOrg resolves a session's owning org via its scope, fail-closed.
// Returns "" when the org cannot be determined (→ the session is excluded).
func (s *Server) transferSessionOrg(ctx context.Context, d HandlerDeps, sess *files.FileTransferSession) string {
	switch sess.Scope() {
	case files.ScopeProject:
		if d.PM == nil {
			return ""
		}
		pr, err := d.PM.GetProject(ctx, pm.ProjectID(sess.ScopeID()))
		if err != nil || pr == nil {
			return ""
		}
		return pr.OrganizationID()
	case files.ScopeTask:
		if d.PM == nil {
			return ""
		}
		tk, err := d.PM.GetTask(ctx, pm.TaskID(sess.ScopeID()))
		if err != nil || tk == nil {
			return ""
		}
		pr, err := d.PM.GetProject(ctx, tk.ProjectID())
		if err != nil || pr == nil {
			return ""
		}
		return pr.OrganizationID()
	case files.ScopeIssue:
		if d.PM == nil {
			return ""
		}
		is, err := d.PM.GetIssue(ctx, pm.IssueID(sess.ScopeID()))
		if err != nil || is == nil {
			return ""
		}
		pr, err := d.PM.GetProject(ctx, is.ProjectID())
		if err != nil || pr == nil {
			return ""
		}
		return pr.OrganizationID()
	case files.ScopeConversation:
		if d.ConvRepo == nil {
			return ""
		}
		c, err := d.ConvRepo.FindByID(ctx, conversation.ConversationID(sess.ScopeID()))
		if err != nil || c == nil {
			return ""
		}
		return c.OrganizationID()
	case files.ScopeAgent:
		if d.AgentSvc == nil {
			return ""
		}
		a, err := d.AgentSvc.GetAgent(ctx, agentbc.AgentID(sess.ScopeID()))
		if err != nil || a == nil {
			return ""
		}
		return a.OrganizationID()
	default:
		// tmp + unscoped + unknown scope → not an org-resolvable view → exclude.
		return ""
	}
}

// transferSessionMap serializes a transfer session for the Environment view.
func transferSessionMap(sess *files.FileTransferSession) map[string]any {
	return map[string]any{
		"id":           sess.ID(),
		"file_uri":     string(sess.FileURI()),
		"transfer_uri": sess.TransferURI(),
		"direction":    string(sess.Direction()),
		"status":       string(sess.Status()),
		"scope":        string(sess.Scope()),
		"scope_id":     sess.ScopeID(),
		"content_type": sess.ContentType(),
		"size":         sess.Size(),
		"created_by":   sess.CreatedBy(),
		"created_at":   sess.CreatedAt().Format(time.RFC3339Nano),
		"expires_at":   sess.ExpiresAt().Format(time.RFC3339Nano),
	}
}

// listTransfersHandler serves GET /api/files/transfers — the caller org's LIVE
// in-flight transfer sessions. ListOpen returns ALL open+unexpired sessions (no
// global cap → no #126 truncation); each is kept only if its scope→org resolves
// to the caller's org (fail-closed), so cross-org / tmp / unresolved sessions are
// excluded with no existence leak.
func (s *Server) listTransfersHandler(w http.ResponseWriter, r *http.Request) {
	d := hd(r)
	if d.FileTransferRepo == nil {
		writeError(w, http.StatusNotImplemented, "file_transfers_not_wired", "file transfer repo not wired")
		return
	}
	_, _, orgID, ok := requireOrgMember(w, r, d)
	if !ok {
		return
	}
	sessions, err := d.FileTransferRepo.ListOpen(r.Context(), time.Now().UTC())
	if err != nil {
		writeError(w, http.StatusInternalServerError, "file_transfers_error", err.Error())
		return
	}
	out := make([]map[string]any, 0)
	for _, sess := range sessions {
		if s.transferSessionOrg(r.Context(), d, sess) == orgID {
			out = append(out, transferSessionMap(sess))
		}
	}
	writeJSON(w, http.StatusOK, map[string]any{"transfer_sessions": out})
}
