// handlers_attention.go — v2.26.0 I61: the "Needs your attention" panel data
// source, extended to cover agent→human signals that DON'T require a pre-existing
// human-owned task.
//
// GET /api/orgs/{slug}/attention returns, for the logged-in HUMAN, a single
// unified, deduped, severity-sorted list of attention items drawn from TWO
// sources UNIONed:
//
//   1. kind=task  — actionable STUCK tasks: a RUNNING task carrying a non-empty
//      blocked_reason whose blocked_reason_type is input_required (an agent needs
//      the user's reply) or obstacle (an external blocker needs owner/PM action).
//      This is the panel's pre-existing source (web/src/api/stuckTasks.ts derived
//      it client-side from GET /tasks); mirroring it server-side here means the
//      unified endpoint never regresses it.
//
//   2. kind=mention — the human's DIRECTED unread: every unread DM message + every
//      unread @mention of the human in any other conversation kind (channel /
//      task / issue / plan). This is the I61 gap-fill: when an agent escalates by
//      @mentioning the human in a work (plan/task) conversation — and there is NO
//      human-owned task to carry an input_required block — the escalation now
//      surfaces in the panel anyway. Reuses the I23 (T332) unread digest plumbing
//      (ResolveFollowed + UnreadWithMentions + buildUnreadConvRow), so the numbers
//      match the per-row badges and there is no second source of truth.
//
// EXTENSION POINT (二期 / I61 §3, deliberately NOT implemented this round): a
// kind=stuck signal — a node long-RUNNING whose complete_task keeps failing —
// would be a third source appended into `rows` with its own severity. The item
// envelope (kind / severity / ref / ts) is generic enough to carry it with no
// contract change; only a new collector is needed.

package api

import (
	"net/http"
	"sort"
	"strings"
	"time"

	"github.com/oopslink/agent-center/internal/conversation"
	pm "github.com/oopslink/agent-center/internal/projectmanager"
)

// attentionResultCap bounds the unified list (the panel is a digest, not a feed).
const attentionResultCap = 100

// attentionSeverityRank orders items most-urgent first. input_required (an agent
// is literally waiting on the human) ranks above everything; obstacle + directed
// mentions/DMs are "warning"; "info" is reserved for future lower-priority kinds.
var attentionSeverityRank = map[string]int{"urgent": 0, "warning": 1, "info": 2}

// attnRow is an attention item plus its sort keys (rank, ts). The rendered item
// is `row`; rank+ts drive the stable severity-then-recency ordering.
type attnRow struct {
	row  map[string]any
	rank int
	ts   time.Time
}

// listAttentionHandler serves GET /api/orgs/{slug}/attention. Human-only +
// fail-soft: a missing dependency degrades a source to empty rather than 500ing
// the whole panel (the panel must never be the thing that breaks).
func (s *Server) listAttentionHandler(w http.ResponseWriter, r *http.Request) {
	d := hd(r)
	_, _, orgID, ok := requireOrgMember(w, r, d)
	if !ok {
		return
	}
	self := conversation.IdentityRef(d.Actor)

	rows := make([]attnRow, 0, 16)
	// Tasks already surfaced as a kind=task item — used to dedup a directed
	// @mention that points at the SAME task conversation (the task item is richer).
	stuckTaskIDs := map[string]bool{}

	rows = s.collectStuckTaskItems(r, d, orgID, rows, stuckTaskIDs)
	rows = s.collectDirectedMentionItems(r, d, orgID, self, rows, stuckTaskIDs)

	sort.SliceStable(rows, func(i, j int) bool {
		if rows[i].rank != rows[j].rank {
			return rows[i].rank < rows[j].rank
		}
		return rows[i].ts.After(rows[j].ts) // newest first within a severity band
	})

	out := make([]map[string]any, 0, len(rows))
	for _, ar := range rows {
		if len(out) >= attentionResultCap {
			break
		}
		out = append(out, ar.row)
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": out})
}

// collectStuckTaskItems appends one kind=task item per actionable stuck task
// (RUNNING + non-empty blocked_reason ∈ {input_required, obstacle}) and records
// the task ids for dedup. Fail-soft: a PM read error yields no task items.
func (s *Server) collectStuckTaskItems(
	r *http.Request, d HandlerDeps, orgID string, rows []attnRow, stuckTaskIDs map[string]bool,
) []attnRow {
	if d.PM == nil {
		return rows
	}
	projects, err := d.PM.ListProjects(r.Context(), orgID)
	if err != nil {
		return rows
	}
	var ids []pm.ProjectID
	for _, p := range projects {
		if p.Status() == pm.ProjectArchived {
			continue // archived project's tasks are not actionable attention
		}
		ids = append(ids, p.ID())
	}
	if len(ids) == 0 {
		return rows
	}
	tasks, _, lerr := d.PM.ListTasksOrgPage(r.Context(), pm.OrgListQuery{
		ProjectIDs: ids,
		Statuses:   []string{"running"}, // a task is "stuck" only while RUNNING (ADR-0046)
	})
	if lerr != nil {
		return rows
	}
	byID := projectByID(projects)
	for _, t := range tasks {
		reason := strings.TrimSpace(t.BlockedReason())
		rt := string(t.BlockedReasonType())
		if reason == "" || (rt != "input_required" && rt != "obstacle") {
			continue
		}
		severity := "warning"
		if rt == "input_required" {
			severity = "urgent"
		}
		projectName := ""
		if p := byID[string(t.ProjectID())]; p != nil {
			projectName = p.Name()
		}
		row := map[string]any{
			"kind":            "task",
			"ref":             string(t.ID()),
			"task_id":         string(t.ID()),
			"conversation_id": "",
			"title":           t.Title(),
			"snippet":         reason,
			"actor":           "",
			"reason_type":     rt,
			"severity":        severity,
			"project_id":      string(t.ProjectID()),
			"project_name":    projectName,
			"ts":              t.UpdatedAt().UTC().Format(time.RFC3339Nano),
			"route":           "/projects/" + string(t.ProjectID()) + "/tasks/" + string(t.ID()),
		}
		if ref := orgRefToken("T", t.OrgNumber()); ref != "" {
			row["org_ref"] = ref
		}
		rows = append(rows, attnRow{row: row, rank: attentionSeverityRank[severity], ts: t.UpdatedAt()})
		stuckTaskIDs[string(t.ID())] = true
	}
	return rows
}

// collectDirectedMentionItems appends one kind=mention item per conversation in
// which the human has a DIRECTED unread signal: a DM with any unread (every DM
// message is direct), or an @mention of the human in any other kind (channel /
// task / issue / plan). Plain non-mention chatter is excluded. Reuses the I23
// unread-digest plumbing. Human-only + fail-soft when the conv/read/follow
// services are unwired.
func (s *Server) collectDirectedMentionItems(
	r *http.Request, d HandlerDeps, orgID string, self conversation.IdentityRef,
	rows []attnRow, stuckTaskIDs map[string]bool,
) []attnRow {
	// Agents get directed-wake, never a panel digest (mirrors the unread handler).
	if !self.IsHuman() || d.ConvRepo == nil || d.ReadStateSvc == nil || d.FollowStateSvc == nil {
		return rows
	}
	selfDisplayName := resolveDisplayName(r, d, self)

	active := conversation.ConversationActive
	var all []*conversation.Conversation
	var cursor *conversation.ConversationID
	for len(all) < unreadConvScanCap {
		page, err := d.ConvRepo.Find(r.Context(), conversation.ConversationFilter{
			OrganizationID: orgID,
			Status:         &active,
			Cursor:         cursor,
			Limit:          conversation.DefaultConversationLimit,
		})
		if err != nil {
			return rows // fail-soft: no mention items rather than 500
		}
		all = append(all, page...)
		if len(page) < conversation.DefaultConversationLimit {
			break
		}
		last := page[len(page)-1].ID()
		cursor = &last
	}

	followedMap := map[conversation.ConversationID]bool{}
	if m, ferr := d.FollowStateSvc.ResolveFollowed(r.Context(), self, all); ferr == nil {
		followedMap = m
	}
	recentByConv := map[conversation.ConversationID][]*conversation.Message{}
	if d.MsgRepo != nil && len(all) > 0 {
		ids := make([]conversation.ConversationID, len(all))
		for i, c := range all {
			ids[i] = c.ID()
		}
		if m, rerr := d.MsgRepo.RecentByConversations(r.Context(), ids, 1); rerr == nil {
			recentByConv = m
		}
	}

	nr := newNameResolver(r, d)
	for _, c := range all {
		// Top-level only (threads resolve their own follow/unread).
		if c.ParentConversationID() != "" {
			continue
		}
		// An explicit unfollow suppresses (parity with the unread digest).
		if !followedMap[c.ID()] {
			continue
		}
		sum, err := d.ReadStateSvc.UnreadWithMentions(r.Context(), self, c.ID(), selfDisplayName)
		if err != nil || sum.UnreadCount == 0 {
			continue
		}
		kind := c.Kind()
		// DIRECTED gate: a DM the human PARTICIPATES IN (every unread message there
		// is to them) OR an unread @mention of the human in any other kind. Two
		// exclusions the firehose must avoid: plain non-mention channel/work chatter,
		// AND — critically — a DM the human merely OBSERVES but isn't a party to.
		// The conversation scan above is org-wide (no participant filter), so a DM
		// between two OTHER identities (e.g. a system↔agent reminder delivery) is in
		// `all`; without the participant check its every message would count as this
		// human's unread and leak into the panel (a directed-signal false positive
		// AND a mild info leak). A DM only counts when self is an active party.
		if kind == conversation.ConversationKindDM {
			if !isActiveParticipant(c, self) {
				continue
			}
		} else if sum.MentionCount == 0 {
			continue
		}
		base, ts, ok := s.buildUnreadConvRow(r, d, nr, c, sum, recentByConv[c.ID()])
		if !ok {
			continue // unnavigable (owning object gone) — drop rather than dead-link
		}
		// Dedup: this conversation's task already surfaced as a richer kind=task item.
		if st, _ := base["source_type"].(string); st == "task" {
			if sid, _ := base["source_id"].(string); stuckTaskIDs[sid] {
				continue
			}
		}
		rows = append(rows, attnRow{row: mentionAttentionRow(base, recentByConv[c.ID()]), rank: attentionSeverityRank["warning"], ts: ts})
	}
	return rows
}

// mentionAttentionRow adapts an I23 unread-digest row into the attention item
// envelope: it keeps the navigable fields (title / route / project_id / counts)
// and re-labels the digest's last-message preview/sender as the attention item's
// snippet/actor, plus message_id (the dismiss target — mark_seen advances the
// read cursor past it). severity is "warning" (a directed signal needs attention
// but isn't a formal input_required block).
func mentionAttentionRow(base map[string]any, recent []*conversation.Message) map[string]any {
	row := map[string]any{
		"kind":              "mention",
		"ref":               base["conversation_id"],
		"conversation_id":   base["conversation_id"],
		"conversation_kind": base["source_type"],
		"title":             base["title"],
		"snippet":           base["last_message_preview"],
		"actor":             base["last_message_sender"],
		"severity":          "warning",
		"ts":                base["updated_at"],
		"route":             base["route"],
		"unread_count":      base["unread_count"],
		"mention_count":     base["mention_count"],
	}
	if pid, ok := base["project_id"].(string); ok && pid != "" {
		row["project_id"] = pid
	}
	if len(recent) > 0 {
		row["message_id"] = string(recent[0].ID()) // dismiss = mark seen up to here
	}
	return row
}
