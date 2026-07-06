package api

import (
	"errors"
	"net/http"
	"strings"
	"time"

	"github.com/oopslink/agent-center/internal/cognition/reminder"
	cogservice "github.com/oopslink/agent-center/internal/cognition/reminder/service"
)

// Reminder agent-tools (T206, design 03-reminder.md §5.1). The 4 tools
// create_reminder / list_reminders / get_reminder / update_reminder are thin
// HTTP wrappers over the cognition ReminderAppService: requireAgentOnWorker gates
// the operating agent, the creator/requester is the PROCESS-FIXED agentActor (never
// from model args), and the AppService enforces the cross-project guard + authz.

// scheduleDTO is the wire shape of a Schedule on the agent-tools boundary.
type scheduleDTO struct {
	Kind     string `json:"kind"`      // "once" | "cron"
	OnceAt   string `json:"once_at"`   // RFC3339 (once)
	CronExpr string `json:"cron_expr"` // cron expr (cron)
	Timezone string `json:"timezone"`  // IANA tz (cron)
}

func (d scheduleDTO) toDomain() (reminder.Schedule, error) {
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

// onEventDTO is the wire shape of an OnEvent trigger on the agent-tools boundary
// (reminder-event feature). entity_type ∈ plan|task|issue; event is the watched
// transition (validated by the domain factory against the per-type vocabulary).
type onEventDTO struct {
	EntityType string `json:"entity_type"`
	EntityID   string `json:"entity_id"`
	Event      string `json:"event"`
}

// parseDelay parses the optional on_event delay: a Go duration string ("5m",
// "30s", "0") or empty (⇒ 0 = fire at the next tick after the event).
func parseDelay(s string) (time.Duration, error) {
	s = strings.TrimSpace(s)
	if s == "" {
		return 0, nil
	}
	d, err := time.ParseDuration(s)
	if err != nil {
		return 0, errors.New("delay must be a duration like 5m, 30s, or 0")
	}
	if d < 0 {
		return 0, errors.New("delay must be >= 0")
	}
	return d, nil
}

// endConditionDTO is the wire shape of an EndCondition.
type endConditionDTO struct {
	Kind     string `json:"kind"`      // "never" | "until" | "max_count"
	Until    string `json:"until"`     // RFC3339 (until)
	MaxCount int    `json:"max_count"` // (max_count)
}

func (d endConditionDTO) toDomain() (reminder.EndCondition, error) {
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

// reminderMap projects a Reminder to the wire shape.
func reminderMap(r *reminder.Reminder) map[string]any {
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
		"schedule":           scheduleToMap(r.Schedule()),
		"created_at":         r.CreatedAt().UTC().Format(time.RFC3339Nano),
		"updated_at":         r.UpdatedAt().UTC().Format(time.RFC3339Nano),
	}
	if oe := r.OnEvent(); oe != nil {
		m["on_event"] = map[string]any{
			"entity_type":   string(oe.EntityType),
			"entity_id":     oe.EntityID,
			"event":         oe.Event,
			"delay_seconds": int64(oe.Delay / time.Second),
		}
	}
	if r.NextRunAt() != nil {
		m["next_run_at"] = r.NextRunAt().UTC().Format(time.RFC3339Nano)
	}
	if r.LastFiredAt() != nil {
		m["last_fired_at"] = r.LastFiredAt().UTC().Format(time.RFC3339Nano)
	}
	return m
}

func scheduleToMap(s reminder.Schedule) map[string]any {
	if s.Kind == reminder.ScheduleOnce {
		return map[string]any{"kind": "once", "once_at": s.OnceAt.UTC().Format(time.RFC3339Nano)}
	}
	return map[string]any{"kind": "cron", "cron_expr": s.CronExpr, "timezone": s.Timezone}
}

// writeReminderError maps cognition reminder domain/app errors to HTTP.
func writeReminderError(w http.ResponseWriter, err error) {
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
		errors.Is(err, reminder.ErrInvalidOnEvent),
		errors.Is(err, reminder.ErrReminderContentEmpty), errors.Is(err, reminder.ErrReminderRemindeeEmpty):
		writeError(w, http.StatusBadRequest, "invalid_reminder", err.Error())
	default:
		mapDomainError(w, err)
	}
}

// --- create_reminder ---------------------------------------------------------

type createReminderReq struct {
	AgentID          string          `json:"agent_id"`
	RemindeeAgentID  string          `json:"remindee_agent_id"`
	Schedule         scheduleDTO     `json:"schedule"`
	Content          string          `json:"content"`
	SkipIfOverlap    *bool           `json:"skip_if_overlap"`
	DeliverAsCreator *bool           `json:"deliver_as_creator"`
	EndCondition     endConditionDTO `json:"end_condition"`
	// reminder-event feature. When on_event is set the reminder is EVENT-DRIVEN:
	// schedule/end_condition are ignored; it arms on the watched pm entity transition
	// and fires once after delay. target is the @target agent to wake (defaults to
	// remindee_agent_id when omitted).
	OnEvent *onEventDTO `json:"on_event"`
	Delay   string      `json:"delay"`  // duration ("5m"|"30s"|"0"); on_event only
	Target  string      `json:"target"` // @target agent id; on_event only
}

func (s *Server) createReminderHandler(w http.ResponseWriter, r *http.Request) {
	d := hd(r)
	var req createReminderReq
	if err := decodeJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_json", err.Error())
		return
	}
	a, ok := s.requireAgentOnWorker(w, r, d, req.AgentID)
	if !ok {
		return
	}
	if d.ReminderSvc == nil {
		writeError(w, http.StatusNotImplemented, "reminder_not_wired", "")
		return
	}
	skip := true
	if req.SkipIfOverlap != nil {
		skip = *req.SkipIfOverlap
	}
	deliverAsCreator := true // default ON per the mockup (F-B)
	if req.DeliverAsCreator != nil {
		deliverAsCreator = *req.DeliverAsCreator
	}
	cmd := cogservice.CreateReminderCommand{
		OrganizationID:   a.OrganizationID(),
		CreatorRef:       agentActor(a),
		Content:          req.Content,
		SkipIfOverlap:    skip,
		DeliverAsCreator: deliverAsCreator,
	}
	if req.OnEvent != nil {
		// Event-driven reminder: build the OnEvent trigger; @target defaults to the
		// remindee. The domain factory validates the entity_type/event vocabulary.
		delay, derr := parseDelay(req.Delay)
		if derr != nil {
			writeError(w, http.StatusBadRequest, "invalid_delay", derr.Error())
			return
		}
		target := strings.TrimSpace(req.Target)
		if target == "" {
			target = strings.TrimSpace(req.RemindeeAgentID)
		}
		cmd.RemindeeAgentID = target
		cmd.Schedule = reminder.EventScheduleFor()
		cmd.OnEvent = &reminder.OnEvent{
			EntityType: reminder.EntityType(strings.ToLower(strings.TrimSpace(req.OnEvent.EntityType))),
			EntityID:   strings.TrimSpace(req.OnEvent.EntityID),
			Event:      strings.ToLower(strings.TrimSpace(req.OnEvent.Event)),
			Delay:      delay,
		}
		cmd.EndCondition = reminder.NeverEnd()
	} else {
		sched, err := req.Schedule.toDomain()
		if err != nil {
			writeError(w, http.StatusBadRequest, "invalid_schedule", err.Error())
			return
		}
		end, err := req.EndCondition.toDomain()
		if err != nil {
			writeError(w, http.StatusBadRequest, "invalid_end_condition", err.Error())
			return
		}
		cmd.RemindeeAgentID = strings.TrimSpace(req.RemindeeAgentID)
		cmd.Schedule = sched
		cmd.EndCondition = end
	}
	rem, err := d.ReminderSvc.CreateReminder(r.Context(), cmd)
	if err != nil {
		writeReminderError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, reminderMap(rem))
}

// --- list_reminders ----------------------------------------------------------

type listRemindersReq struct {
	AgentID         string   `json:"agent_id"`
	CreatorRef      string   `json:"creator_ref"`       // default: the calling agent
	RemindeeAgentID string   `json:"remindee_agent_id"` // alternative selector
	Statuses        []string `json:"statuses"`
}

func (s *Server) listRemindersHandler(w http.ResponseWriter, r *http.Request) {
	d := hd(r)
	var req listRemindersReq
	if err := decodeJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_json", err.Error())
		return
	}
	a, ok := s.requireAgentOnWorker(w, r, d, req.AgentID)
	if !ok {
		return
	}
	if d.ReminderSvc == nil {
		writeError(w, http.StatusNotImplemented, "reminder_not_wired", "")
		return
	}
	q := cogservice.ListRemindersQuery{Statuses: toStatuses(req.Statuses)}
	switch {
	case strings.TrimSpace(req.RemindeeAgentID) != "":
		q.RemindeeAgentID = strings.TrimSpace(req.RemindeeAgentID)
	case strings.TrimSpace(req.CreatorRef) != "":
		q.CreatorRef = strings.TrimSpace(req.CreatorRef)
	default:
		q.CreatorRef = agentActor(a) // default: what I created
	}
	rems, err := d.ReminderSvc.ListReminders(r.Context(), q)
	if err != nil {
		writeReminderError(w, err)
		return
	}
	out := make([]map[string]any, len(rems))
	for i, rm := range rems {
		out[i] = reminderMap(rm)
	}
	writeJSON(w, http.StatusOK, map[string]any{"reminders": out})
}

func toStatuses(ss []string) []reminder.ReminderStatus {
	if len(ss) == 0 {
		return nil
	}
	out := make([]reminder.ReminderStatus, 0, len(ss))
	for _, s := range ss {
		out = append(out, reminder.ReminderStatus(strings.TrimSpace(s)))
	}
	return out
}

// --- get_reminder ------------------------------------------------------------

type getReminderReq struct {
	AgentID    string `json:"agent_id"`
	ReminderID string `json:"reminder_id"`
}

func (s *Server) getReminderHandler(w http.ResponseWriter, r *http.Request) {
	d := hd(r)
	var req getReminderReq
	if err := decodeJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_json", err.Error())
		return
	}
	a, ok := s.requireAgentOnWorker(w, r, d, req.AgentID)
	if !ok {
		return
	}
	if d.ReminderSvc == nil {
		writeError(w, http.StatusNotImplemented, "reminder_not_wired", "")
		return
	}
	rem, err := d.ReminderSvc.GetReminder(r.Context(), reminder.ReminderID(strings.TrimSpace(req.ReminderID)), agentActor(a))
	if err != nil {
		writeReminderError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, reminderMap(rem))
}

// --- update_reminder ---------------------------------------------------------

type updateReminderReq struct {
	AgentID    string       `json:"agent_id"`
	ReminderID string       `json:"reminder_id"`
	Action     string       `json:"action"` // pause | resume | cancel | edit
	Schedule   *scheduleDTO `json:"schedule"`
	Content    string       `json:"content"`
}

func (s *Server) updateReminderHandler(w http.ResponseWriter, r *http.Request) {
	d := hd(r)
	var req updateReminderReq
	if err := decodeJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_json", err.Error())
		return
	}
	a, ok := s.requireAgentOnWorker(w, r, d, req.AgentID)
	if !ok {
		return
	}
	if d.ReminderSvc == nil {
		writeError(w, http.StatusNotImplemented, "reminder_not_wired", "")
		return
	}
	action := cogservice.UpdateAction(strings.ToLower(strings.TrimSpace(req.Action)))
	cmd := cogservice.UpdateReminderCommand{
		ID:           reminder.ReminderID(strings.TrimSpace(req.ReminderID)),
		RequesterRef: agentActor(a),
		Action:       action,
		Content:      req.Content,
	}
	if action == cogservice.ActionEdit && req.Schedule != nil {
		sched, err := req.Schedule.toDomain()
		if err != nil {
			writeError(w, http.StatusBadRequest, "invalid_schedule", err.Error())
			return
		}
		cmd.Schedule = &sched
	}
	rem, err := d.ReminderSvc.UpdateReminder(r.Context(), cmd)
	if err != nil {
		writeReminderError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, reminderMap(rem))
}
