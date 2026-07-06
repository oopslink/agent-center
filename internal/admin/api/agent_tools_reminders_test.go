package api

import (
	"testing"
	"time"

	"github.com/oopslink/agent-center/internal/cognition/reminder"
)

// scheduleToMap must report the REAL schedule kind for all three shapes. The wire
// nit (issue-68ccb310): the on_event case fell through the once-check to the cron
// return, so list/get_reminder mislabeled an event-driven reminder as "cron" and the
// Web Reminders Trigger column showed "Recurring".
func TestScheduleToMap_ReportsRealKind(t *testing.T) {
	at := time.Date(2026, 7, 6, 10, 0, 0, 0, time.UTC)

	once := scheduleToMap(reminder.Schedule{Kind: reminder.ScheduleOnce, OnceAt: at})
	if once["kind"] != "once" {
		t.Errorf("once kind = %v, want once", once["kind"])
	}
	if once["once_at"] == nil {
		t.Errorf("once map missing once_at: %v", once)
	}

	cron := scheduleToMap(reminder.Schedule{Kind: reminder.ScheduleCron, CronExpr: "0 9 * * *", Timezone: "Asia/Shanghai"})
	if cron["kind"] != "cron" {
		t.Errorf("cron kind = %v, want cron", cron["kind"])
	}
	if cron["cron_expr"] != "0 9 * * *" || cron["timezone"] != "Asia/Shanghai" {
		t.Errorf("cron map = %v, want cron_expr+timezone", cron)
	}

	// The bug's target: an on_event schedule must report kind "on_event", NOT "cron".
	event := scheduleToMap(reminder.EventScheduleFor())
	if event["kind"] != "on_event" {
		t.Errorf("on_event kind = %v, want on_event (regression: mislabeled as cron)", event["kind"])
	}
	// It carries no fixed-time / cron fields — the trigger spec rides in the separate
	// on_event block emitted by reminderToMap.
	if _, hasCron := event["cron_expr"]; hasCron {
		t.Errorf("on_event map should not carry cron_expr: %v", event)
	}
	if _, hasOnce := event["once_at"]; hasOnce {
		t.Errorf("on_event map should not carry once_at: %v", event)
	}
}
