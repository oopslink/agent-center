package api

import (
	"errors"
	"net/http"
	"sort"
	"time"

	"github.com/oopslink/agent-center/internal/conversation"
	convservice "github.com/oopslink/agent-center/internal/conversation/service"
	pm "github.com/oopslink/agent-center/internal/projectmanager"
)

// =============================================================================
// I23 (T332) — cross-source "unread conversations" aggregation.
//
// GET /api/orgs/{slug}/unread-conversations returns, for the logged-in HUMAN
// user, every conversation they have unread in across ALL kinds (channel / dm /
// task / issue / plan) — the data behind the main-sidebar "未读会话 · 跨来源"
// region (mockup-conversations-reachability). plan/issue/task conversations are
// otherwise buried in their detail pages; this surfaces them in one list so the
// user can jump straight to the source's conversation area.
//
// It REUSES the v2.8 #268 badge model verbatim (ResolveFollowed +
// UnreadWithMentions, both agent-aware) — same numbers the per-row badges on
// GET /conversations show, no second source of truth.
//
// RELEVANCE GATE (avoid the firehose). The follow model defaults every TOP-LEVEL
// conversation to followed, and an absent read-state row counts as "everything
// unread" (read_state.go). Naively that would surface EVERY task/issue/plan
// conversation in the org for a user who never touched it. So:
//   - a row is included only when followed (explicit unfollow suppresses) AND
//     unread_count > 0;
//   - task/issue/plan additionally require the user to have ENGAGED — a
//     read-state row (opened/posted before), OR an unread @mention, OR active
//     participation — so only the pm conversations the user actually cares about
//     appear. channel/dm keep the same semantics as the existing list.
// Threads (a non-empty parent) never surface here — only top-level conversations.
// =============================================================================

// unreadConvScanCap bounds how many of the org's conversations we page through
// when assembling the digest (the gate filters the RESULT, not the scan, so this
// caps the work). unreadConvResultCap bounds the returned rows.
const (
	unreadConvScanCap   = 2000
	unreadConvResultCap = 100
)

func (s *Server) listUnreadConversationsHandler(w http.ResponseWriter, r *http.Request) {
	d := hd(r)
	_, _, orgID, ok := requireOrgMember(w, r, d)
	if !ok {
		return
	}
	self := conversation.IdentityRef(d.Actor)
	// Human-only (Q-T1): agents get directed-wake, never a badge digest. Also a
	// fail-soft empty list when the badge services aren't wired.
	if !self.IsHuman() || d.ConvRepo == nil || d.ReadStateSvc == nil || d.FollowStateSvc == nil {
		writeJSON(w, http.StatusOK, []map[string]any{})
		return
	}
	selfDisplayName := resolveDisplayName(r, d, self)

	// Page through the org's ACTIVE conversations (id ASC keyset, like the agent
	// inbox), bounded by unreadConvScanCap.
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
			writeError(w, http.StatusInternalServerError, "find_failed", err.Error())
			return
		}
		all = append(all, page...)
		if len(page) < conversation.DefaultConversationLimit {
			break
		}
		last := page[len(page)-1].ID()
		cursor = &last
	}

	// Batch-resolve followed (one repo round-trip) — same as the list path.
	followedMap := map[conversation.ConversationID]bool{}
	if m, ferr := d.FollowStateSvc.ResolveFollowed(r.Context(), self, all); ferr == nil {
		followedMap = m
	}

	// Newest message per conversation for the last_message_preview (ONE batch
	// window query — no N+1). Fail-soft: a batch error leaves previews empty.
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
	type sortableRow struct {
		row map[string]any
		ts  time.Time
	}
	rows := make([]sortableRow, 0, 16)
	for _, c := range all {
		if len(rows) >= unreadConvResultCap {
			break
		}
		// Top-level only (threads resolve their own follow/unread, not this digest).
		if c.ParentConversationID() != "" {
			continue
		}
		if !followedMap[c.ID()] {
			continue
		}
		sum, err := d.ReadStateSvc.UnreadWithMentions(r.Context(), self, c.ID(), selfDisplayName)
		if err != nil || sum.UnreadCount == 0 {
			continue
		}
		kind := c.Kind()
		if isPMConversationKind(kind) && !s.userEngagedWith(r, d, self, c, sum) {
			continue
		}
		row, ts, ok := s.buildUnreadConvRow(r, d, nr, c, sum, recentByConv[c.ID()])
		if !ok {
			continue
		}
		rows = append(rows, sortableRow{row: row, ts: ts})
	}

	// Most-recent activity first (mockup orders by last-message time).
	sort.SliceStable(rows, func(i, j int) bool { return rows[i].ts.After(rows[j].ts) })
	out := make([]map[string]any, len(rows))
	for i, sr := range rows {
		out[i] = sr.row
	}
	writeJSON(w, http.StatusOK, out)
}

// POST /api/orgs/{slug}/unread-conversations/mark-all-read (T334) — mark EVERY
// conversation in the digest read for the logged-in human in one call. It mirrors
// the list handler's scan + relevance gate (followed, top-level, engaged-for-pm),
// then advances each unread conversation's read cursor to its newest message via
// the same ReadStateSvc.MarkSeen the per-conversation /seen endpoint uses (only-
// forward, idempotent). Returns {"marked": N}. Human-only + fail-soft when unwired.
func (s *Server) markAllUnreadConversationsSeenHandler(w http.ResponseWriter, r *http.Request) {
	d := hd(r)
	_, _, orgID, ok := requireOrgMember(w, r, d)
	if !ok {
		return
	}
	self := conversation.IdentityRef(d.Actor)
	if !self.IsHuman() || d.ConvRepo == nil || d.ReadStateSvc == nil || d.FollowStateSvc == nil || d.MsgRepo == nil {
		writeJSON(w, http.StatusOK, map[string]any{"marked": 0})
		return
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
			writeError(w, http.StatusInternalServerError, "find_failed", err.Error())
			return
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
	// Newest message per conversation — the cursor target for MarkSeen.
	recentByConv := map[conversation.ConversationID][]*conversation.Message{}
	if len(all) > 0 {
		ids := make([]conversation.ConversationID, len(all))
		for i, c := range all {
			ids[i] = c.ID()
		}
		if m, rerr := d.MsgRepo.RecentByConversations(r.Context(), ids, 1); rerr == nil {
			recentByConv = m
		}
	}

	marked := 0
	for _, c := range all {
		if c.ParentConversationID() != "" || !followedMap[c.ID()] {
			continue
		}
		sum, err := d.ReadStateSvc.UnreadWithMentions(r.Context(), self, c.ID(), selfDisplayName)
		if err != nil || sum.UnreadCount == 0 {
			continue
		}
		if isPMConversationKind(c.Kind()) && !s.userEngagedWith(r, d, self, c, sum) {
			continue
		}
		recent := recentByConv[c.ID()]
		if len(recent) == 0 {
			continue // nothing to advance the cursor to
		}
		if _, merr := d.ReadStateSvc.MarkSeen(r.Context(), convservice.MarkSeenCommand{
			UserID:            self,
			ConversationID:    c.ID(),
			LastSeenMessageID: recent[0].ID(),
			Actor:             d.Actor,
		}); merr == nil {
			marked++
		}
	}
	writeJSON(w, http.StatusOK, map[string]any{"marked": marked})
}

// isPMConversationKind reports whether a kind is a ProjectManager-owned
// conversation (task/issue/plan) — the kinds that get the extra "engaged" gate
// and need a project_id resolved for their route.
func isPMConversationKind(k conversation.ConversationKind) bool {
	return k == conversation.ConversationKindTask ||
		k == conversation.ConversationKindIssue ||
		k == conversation.ConversationKindPlan
}

// userEngagedWith reports whether the human has actually engaged with a pm
// conversation: an unread @mention, an active participation, or a pre-existing
// read-state row (they opened or posted before). Used to keep the digest to the
// pm conversations the user cares about rather than every one in the org.
func (s *Server) userEngagedWith(r *http.Request, d HandlerDeps,
	self conversation.IdentityRef, c *conversation.Conversation, sum convservice.MentionSummary,
) bool {
	if sum.MentionCount > 0 {
		return true
	}
	for _, p := range c.Participants() {
		if p.IsActive() && p.IdentityID == self {
			return true
		}
	}
	if d.ReadStateRepo != nil {
		rs, err := d.ReadStateRepo.FindByUserAndConv(r.Context(), self, c.ID())
		if err == nil && rs != nil {
			return true
		}
		if err != nil && !errors.Is(err, conversation.ErrReadStateNotFound) {
			// A read-state lookup hiccup must not hide a genuine unread; treat as
			// engaged (fail-open for the user's own digest).
			return true
		}
	}
	return false
}

// buildUnreadConvRow renders one digest row + its sort timestamp. Returns ok=false
// when the row can't be made navigable (e.g. a pm conversation whose owning object
// is gone), so it is dropped rather than rendered as a dead link.
func (s *Server) buildUnreadConvRow(r *http.Request, d HandlerDeps, nr *nameResolver,
	c *conversation.Conversation, sum convservice.MentionSummary, recent []*conversation.Message,
) (map[string]any, time.Time, bool) {
	kind := c.Kind()
	row := map[string]any{
		"conversation_id": string(c.ID()),
		"source_type":     string(kind),
		"source_ref":      string(c.OwnerRef()),
		"unread_count":    sum.UnreadCount,
		"mention_count":   sum.MentionCount,
	}

	// last_message_preview / sender + the activity timestamp used for sorting and
	// the displayed time. Falls back to the conversation's updated_at when empty.
	ts := c.UpdatedAt()
	if len(recent) > 0 {
		m := recent[0]
		row["last_message_preview"] = plainTextPreview(m.Content())
		row["last_message_sender"] = nr.resolveDisplayName(r.Context(), m.SenderIdentityID())
		ts = m.PostedAt()
	} else {
		row["last_message_preview"] = ""
		row["last_message_sender"] = ""
	}
	row["updated_at"] = ts.UTC().Format(time.RFC3339Nano)

	switch kind {
	case conversation.ConversationKindChannel:
		row["title"] = c.Name()
		row["source_id"] = string(c.ID())
		row["route"] = "/channels/" + string(c.ID())
	case conversation.ConversationKindDM:
		dm := map[string]any{}
		enrichDMProjection(r.Context(), nr, c, conversation.IdentityRef(d.Actor), dm)
		title, _ := dm["dm_title"].(string)
		if title == "" {
			title = "Direct message"
		}
		row["title"] = title
		row["source_id"] = string(c.ID())
		row["route"] = "/dms/" + string(c.ID())
	case conversation.ConversationKindTask, conversation.ConversationKindIssue, conversation.ConversationKindPlan:
		title, projectID, ok := s.resolvePMOwner(r, d, kind, c.OwnerRef())
		if !ok {
			return nil, ts, false
		}
		oc, _ := conversation.ResolveOwnerContext(string(c.OwnerRef()))
		row["title"] = title
		row["source_id"] = oc.ID
		row["project_id"] = projectID
		row["route"] = pmRoute(kind, projectID, oc.ID)
	default:
		// Unknown kind — not navigable here.
		return nil, ts, false
	}
	return row, ts, true
}

// resolvePMOwner resolves the owning object's title + project id for a
// task/issue/plan conversation. Returns ok=false when PM is unwired or the
// object is gone (the row is then dropped — a dead link helps no one).
func (s *Server) resolvePMOwner(r *http.Request, d HandlerDeps,
	kind conversation.ConversationKind, owner conversation.OwnerRef,
) (title, projectID string, ok bool) {
	if d.PM == nil {
		return "", "", false
	}
	oc, parsed := conversation.ResolveOwnerContext(string(owner))
	if !parsed || oc.ID == "" {
		return "", "", false
	}
	switch kind {
	case conversation.ConversationKindTask:
		t, err := d.PM.GetTask(r.Context(), pm.TaskID(oc.ID))
		if err != nil || t == nil {
			return "", "", false
		}
		return t.Title(), string(t.ProjectID()), true
	case conversation.ConversationKindIssue:
		i, err := d.PM.GetIssue(r.Context(), pm.IssueID(oc.ID))
		if err != nil || i == nil {
			return "", "", false
		}
		return i.Title(), string(i.ProjectID()), true
	case conversation.ConversationKindPlan:
		p, err := d.PM.GetPlan(r.Context(), pm.PlanID(oc.ID))
		if err != nil || p == nil {
			return "", "", false
		}
		return p.Name(), string(p.ProjectID()), true
	}
	return "", "", false
}

// pmRoute builds the relative (orgBase-less) SPA route to a pm conversation's
// detail page — the FE prepends orgBase. Mirrors web/src/App.tsx route patterns.
func pmRoute(kind conversation.ConversationKind, projectID, objectID string) string {
	switch kind {
	case conversation.ConversationKindTask:
		return "/projects/" + projectID + "/tasks/" + objectID
	case conversation.ConversationKindIssue:
		return "/projects/" + projectID + "/issues/" + objectID
	case conversation.ConversationKindPlan:
		return "/projects/" + projectID + "/plans/" + objectID
	}
	return ""
}
