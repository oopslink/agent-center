package api

import (
	"errors"
	"net/http"
	"strings"
	"time"

	"github.com/oopslink/agent-center/internal/cognition/reminder"
	cogservice "github.com/oopslink/agent-center/internal/cognition/reminder/service"
)

// =============================================================================
// T207 — the HUMAN web-console Reminder CRUD: /api/orgs/{slug}/reminders.
//
// These are the session-authed (user:<id>) counterpart of T206's agent-tools
// (/admin/agent-tools/*). They are thin wrappers over the SAME cognition
// ReminderAppService — the cross-project guard + creator/owner authz live there,
// unchanged. The console user is a `user:<id>` ref (an "owner" per the reminder
// Directory), so it may see the org-wide "全部" view and create reminders for any
// project agent. Behavior/authz is the service's; this layer only adapts HTTP.
// =============================================================================

// remSchedDTO / remEndDTO mirror the agent-tools wire shapes (kept local to the
// webconsole package; same field names so the FE speaks one schema).
type remSchedDTO struct {
	Kind     string `json:"kind"`      // once | cron
	OnceAt   string `json:"once_at"`   // RFC3339 (once)
	CronExpr string `json:"cron_expr"` // (cron)
	Timezone string `json:"timezone"`  // IANA tz (cron)
}

func (d remSchedDTO) toDomain() (reminder.Schedule, error) {
	switch strings.ToLower(strings.TrimSpace(d.Kind)) {
	case "once":
		at, err := time.Parse(time.RFC3339, strings.TrimSpace(d.OnceAt))
		if err != nil {
			return reminder.Schedule{}, errors.New("once_at must be RFC3339")
		}
		return reminder.OnceScheduleAt(at.UTC()), nil
	case "cron":
		return reminder.CronScheduleAt(d.CronExpr, d.Timezone), nil
	default:
		return reminder.Schedule{}, errors.New("schedule.kind must be once or cron")
	}
}

type remEndDTO struct {
	Kind     string `json:"kind"`      // never | until | max_count
	Until    string `json:"until"`     // RFC3339 (until)
	MaxCount int    `json:"max_count"` // (max_count)
}

func (d remEndDTO) toDomain() (reminder.EndCondition, error) {
	switch strings.ToLower(strings.TrimSpace(d.Kind)) {
	case "", "never":
		return reminder.NeverEnd(), nil
	case "until":
		u, err := time.Parse(time.RFC3339, strings.TrimSpace(d.Until))
		if err != nil {
			return reminder.EndCondition{}, errors.New("until must be RFC3339")
		}
		return reminder.EndCondition{Kind: reminder.EndUntil, Until: u.UTC()}, nil
	case "max_count":
		return reminder.EndCondition{Kind: reminder.EndMaxCount, MaxCount: d.MaxCount}, nil
	default:
		return reminder.EndCondition{}, errors.New("end_condition.kind must be never|until|max_count")
	}
}

// remReminderMap projects a Reminder to the FE wire shape.
func remReminderMap(r *reminder.Reminder) map[string]any {
	m := map[string]any{
		"id":                 r.ID().String(),
		"organization_id":    r.OrganizationID(),
		"project_id":         r.ProjectID(),
		"creator_ref":        r.CreatorRef(),
		"remindee_agent_id":  r.RemindeeAgentID(),
		"content":            r.Content(),
		"status":             string(r.Status()),
		"skip_if_overlap":    r.SkipIfOverlap(),
		"deliver_as_creator": r.DeliverAsCreator(),
		"fired_count":        r.FiredCount(),
		"version":            r.Version(),
		"schedule":           remSchedToMap(r.Schedule()),
		"created_at":         r.CreatedAt().UTC().Format(time.RFC3339Nano),
		"updated_at":         r.UpdatedAt().UTC().Format(time.RFC3339Nano),
	}
	if r.NextRunAt() != nil {
		m["next_run_at"] = r.NextRunAt().UTC().Format(time.RFC3339Nano)
	}
	if r.LastFiredAt() != nil {
		m["last_fired_at"] = r.LastFiredAt().UTC().Format(time.RFC3339Nano)
	}
	return m
}

func remSchedToMap(s reminder.Schedule) map[string]any {
	if s.Kind == reminder.ScheduleOnce {
		return map[string]any{"kind": "once", "once_at": s.OnceAt.UTC().Format(time.RFC3339Nano)}
	}
	return map[string]any{"kind": "cron", "cron_expr": s.CronExpr, "timezone": s.Timezone}
}

func remFiringMap(f reminder.Firing) map[string]any {
	return map[string]any{
		"id":          f.ID,
		"reminder_id": f.ReminderID,
		"fired_at":    f.FiredAt.UTC().Format(time.RFC3339Nano),
		"outcome":     string(f.Outcome),
		"detail":      f.Detail,
	}
}

// writeRemErr maps cognition reminder domain/app errors to HTTP.
func writeRemErr(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, reminder.ErrReminderNotFound):
		writeError(w, http.StatusNotFound, "reminder_not_found", err.Error())
	case errors.Is(err, reminder.ErrCrossProjectReminder):
		writeError(w, http.StatusForbidden, "cross_project_reminder", err.Error())
	case errors.Is(err, cogservice.ErrReminderForbidden):
		writeError(w, http.StatusForbidden, "reminder_forbidden", err.Error())
	case errors.Is(err, reminder.ErrReminderTerminal):
		writeError(w, http.StatusConflict, "reminder_terminal", err.Error())
	case errors.Is(err, reminder.ErrInvalidSchedule), errors.Is(err, reminder.ErrInvalidEndCondition),
		errors.Is(err, reminder.ErrReminderContentEmpty), errors.Is(err, reminder.ErrReminderRemindeeEmpty):
		writeError(w, http.StatusBadRequest, "invalid_reminder", err.Error())
	default:
		mapDomainError(w, err)
	}
}

// remCaller resolves the org + the session user's reminder ref ("user:<id>") and
// the operating identity. ok=false means the error envelope was already written.
func (s *Server) remCaller(w http.ResponseWriter, r *http.Request, d HandlerDeps) (orgID, callerRef string, ok bool) {
	if d.Reminder == nil {
		writeError(w, http.StatusNotImplemented, "reminder_not_wired", "")
		return "", "", false
	}
	id, _, org, k := requireOrgMember(w, r, d)
	if !k {
		return "", "", false
	}
	return org, "user:" + id.ID(), true
}

// GET /api/orgs/{slug}/reminders?filter=all|created|remindee&status=active,paused,...
func (s *Server) remListHandler(w http.ResponseWriter, r *http.Request) {
	d := hd(r)
	orgID, caller, ok := s.remCaller(w, r, d)
	if !ok {
		return
	}
	statuses := parseRemStatuses(r.URL.Query().Get("status"))
	var (
		rs  []*reminder.Reminder
		err error
	)
	switch r.URL.Query().Get("filter") {
	case "created":
		// 我创建的 — reminders this caller created.
		rs, err = d.Reminder.ListReminders(r.Context(), cogservice.ListRemindersQuery{CreatorRef: caller, Statuses: statuses})
	case "remindee":
		// 提醒我的 — reminders TARGETING the viewing identity (remindee dimension),
		// filtered server-side. The remindee key is the bare id (no "user:"/"agent:"
		// prefix), matching reminder_firings' remindee_agent_id.
		rs, err = d.Reminder.ListReminders(r.Context(), cogservice.ListRemindersQuery{RemindeeAgentID: bareRef(caller), Statuses: statuses})
	default:
		// default "全部" — owner (any console user) sees the org-wide set.
		rs, err = d.Reminder.ListOrgReminders(r.Context(), orgID, caller, statuses)
	}
	if err != nil {
		writeRemErr(w, err)
		return
	}
	q := strings.ToLower(strings.TrimSpace(r.URL.Query().Get("q"))) // content contains (server-side search)
	out := make([]map[string]any, 0, len(rs))
	for _, rm := range rs {
		if q != "" && !strings.Contains(strings.ToLower(rm.Content()), q) {
			continue
		}
		out = append(out, remReminderMap(rm))
	}
	// Server-side sort + pagination (shared with the org list handlers). With no
	// sort/limit params this is updated_at DESC + all rows; `total` is the full
	// (pre-page) count so the client can render pagination.
	page, total := applyPageItems(out, parsePageParams(r))
	writeJSON(w, http.StatusOK, map[string]any{"reminders": page, "total": total})
}

// bareRef strips a "user:"/"agent:" prefix to the bare id, so a session
// identity can be matched against a reminder's remindee_agent_id.
func bareRef(ref string) string {
	if i := strings.IndexByte(ref, ':'); i >= 0 {
		return ref[i+1:]
	}
	return ref
}

func parseRemStatuses(s string) []reminder.ReminderStatus {
	s = strings.TrimSpace(s)
	if s == "" {
		return nil
	}
	var out []reminder.ReminderStatus
	for _, p := range strings.Split(s, ",") {
		if p = strings.TrimSpace(p); p != "" {
			out = append(out, reminder.ReminderStatus(p))
		}
	}
	return out
}

// POST /api/orgs/{slug}/reminders
func (s *Server) remCreateHandler(w http.ResponseWriter, r *http.Request) {
	d := hd(r)
	orgID, caller, ok := s.remCaller(w, r, d)
	if !ok {
		return
	}
	var req struct {
		RemindeeAgentID  string      `json:"remindee_agent_id"`
		Schedule         remSchedDTO `json:"schedule"`
		Content          string      `json:"content"`
		SkipIfOverlap    *bool       `json:"skip_if_overlap"`
		DeliverAsCreator *bool       `json:"deliver_as_creator"`
		EndCondition     remEndDTO   `json:"end_condition"`
	}
	if err := decodeJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_json", err.Error())
		return
	}
	sched, err := req.Schedule.toDomain()
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid_reminder", err.Error())
		return
	}
	end, err := req.EndCondition.toDomain()
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid_reminder", err.Error())
		return
	}
	skip := true // default per design (skip_if_overlap default true)
	if req.SkipIfOverlap != nil {
		skip = *req.SkipIfOverlap
	}
	deliverAsCreator := true // default ON per the mockup (F-B)
	if req.DeliverAsCreator != nil {
		deliverAsCreator = *req.DeliverAsCreator
	}
	rm, err := d.Reminder.CreateReminder(r.Context(), cogservice.CreateReminderCommand{
		OrganizationID:   orgID,
		CreatorRef:       caller,
		RemindeeAgentID:  strings.TrimSpace(req.RemindeeAgentID),
		Schedule:         sched,
		Content:          req.Content,
		SkipIfOverlap:    skip,
		DeliverAsCreator: deliverAsCreator,
		EndCondition:     end,
	})
	if err != nil {
		writeRemErr(w, err)
		return
	}
	writeJSON(w, http.StatusOK, remReminderMap(rm))
}

// GET /api/orgs/{slug}/reminders/{reminder_id} — detail + 历史触发 (firings).
func (s *Server) remGetHandler(w http.ResponseWriter, r *http.Request) {
	d := hd(r)
	_, caller, ok := s.remCaller(w, r, d)
	if !ok {
		return
	}
	id := reminder.ReminderID(r.PathValue("reminder_id"))
	rm, err := d.Reminder.GetReminder(r.Context(), id, caller)
	if err != nil {
		writeRemErr(w, err)
		return
	}
	firings, err := d.Reminder.GetReminderFirings(r.Context(), id, caller)
	if err != nil {
		writeRemErr(w, err)
		return
	}
	fs := make([]map[string]any, 0, len(firings))
	for _, f := range firings {
		fs = append(fs, remFiringMap(f))
	}
	out := remReminderMap(rm)
	out["firings"] = fs
	writeJSON(w, http.StatusOK, out)
}

// PATCH /api/orgs/{slug}/reminders/{reminder_id} — {action: pause|resume|cancel|edit}.
func (s *Server) remUpdateHandler(w http.ResponseWriter, r *http.Request) {
	d := hd(r)
	_, caller, ok := s.remCaller(w, r, d)
	if !ok {
		return
	}
	var req struct {
		Action   string       `json:"action"`
		Schedule *remSchedDTO `json:"schedule"`
		Content  string       `json:"content"`
	}
	if err := decodeJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_json", err.Error())
		return
	}
	cmd := cogservice.UpdateReminderCommand{
		ID:           reminder.ReminderID(r.PathValue("reminder_id")),
		RequesterRef: caller,
		Action:       cogservice.UpdateAction(strings.ToLower(strings.TrimSpace(req.Action))),
		Content:      req.Content,
	}
	if req.Schedule != nil {
		sched, err := req.Schedule.toDomain()
		if err != nil {
			writeError(w, http.StatusBadRequest, "invalid_reminder", err.Error())
			return
		}
		cmd.Schedule = &sched
	}
	rm, err := d.Reminder.UpdateReminder(r.Context(), cmd)
	if err != nil {
		writeRemErr(w, err)
		return
	}
	writeJSON(w, http.StatusOK, remReminderMap(rm))
}
