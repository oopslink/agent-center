package service

import (
	"context"
	"testing"
	"time"

	"github.com/oopslink/agent-center/internal/clock"
	"github.com/oopslink/agent-center/internal/idgen"
	"github.com/oopslink/agent-center/internal/observability"
	obsqlite "github.com/oopslink/agent-center/internal/observability/sqlite"
	outboxsql "github.com/oopslink/agent-center/internal/outbox/sqlite"
	"github.com/oopslink/agent-center/internal/persistence"
	pm "github.com/oopslink/agent-center/internal/projectmanager"
	pmsql "github.com/oopslink/agent-center/internal/projectmanager/sqlite"
)

func TestRecordChangeMirrorsAuditAsReplayableEvent(t *testing.T) {
	ctx := context.Background()
	db, err := persistence.Open(t.TempDir() + "/audit-mirror.db")
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if err := persistence.NewMigrator(db).Up(ctx); err != nil {
		t.Fatal(err)
	}
	clk := clock.NewFakeClock(time.Date(2026, 9, 3, 10, 0, 0, 0, time.UTC))
	gen := idgen.NewGenerator(clk)
	er, err := obsqlite.NewEventRepo(ctx, db)
	if err != nil {
		t.Fatal(err)
	}
	ob := outboxsql.NewOutboxRepo(db)
	svc := New(Deps{DB: db, Audit: pmsql.NewAuditLogRepo(db, gen), AuditEvents: observability.NewEventSink(er, er, gen, clk), Outbox: ob, IDGen: gen, Clock: clk})
	err = persistence.RunInTx(ctx, db, func(txCtx context.Context) error {
		svc.recordChange(txCtx, pm.AuditEntry{ProjectID: "P1", ObjectType: pm.AuditObjectTask, ObjectID: "T1", ChangeType: pm.AuditTaskReviewVerdict, Field: "verdict", ToValue: "reject", ActorRef: "agent:reviewer", Detail: `{"blocking":true,"reason":"tests failed","round":2,"plan_id":"PL1"}`})
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	typ := observability.EventType("pm.audit_recorded")
	events, err := er.Find(ctx, observability.EventQueryFilter{EventType: &typ, Limit: 10})
	if err != nil {
		t.Fatal(err)
	}
	if len(events) != 1 {
		t.Fatalf("want one mirror event, got %d", len(events))
	}
	p := events[0].Payload()
	if p["change_type"] != "review_verdict" || p["actor_ref"] != "agent:reviewer" {
		t.Fatalf("unexpected payload %#v", p)
	}
	detail, ok := p["detail"].(map[string]any)
	if !ok || detail["plan_id"] != "PL1" {
		t.Fatalf("detail not structured: %#v", p["detail"])
	}
	out, err := ob.FetchUnprocessed(ctx, 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(out) != 1 || out[0].ID != events[0].ID().String() || out[0].EventType != "pm.audit_recorded" {
		t.Fatalf("realtime mirror must reuse durable event id: %#v", out)
	}
}
