package collaborationeffect

import (
	"context"
	"encoding/json"
	"reflect"
	"sort"
	"testing"
	"time"

	"github.com/oopslink/agent-center/internal/persistence"
)

var testTime = time.Date(2026, 9, 3, 10, 0, 0, 0, time.UTC)

func fact(id, typ, task, actor string, payload map[string]any) Fact {
	return Fact{EventID: id, EventType: typ, OccurredAt: testTime, ActorRef: actor, ProjectID: "P1", TaskID: task, Payload: payload}
}

func TestEngineFrozenRulesAndFailClosed(t *testing.T) {
	e := NewEngine("")
	cases := []struct {
		name string
		f    Fact
		deps []Dependency
		rel  RelationType
		pol  Polarity
	}{
		{"assign", fact("e1", "pm.task.assigned", "T1", "system", map[string]any{"assignee": "agent:a"}), nil, RelationAssign, PolarityNeutral},
		{"reassign", fact("e2", "pm.task.reassigned", "T1", "agent:lead", map[string]any{"previous_assignee": "agent:a", "assignee": "agent:b"}), nil, RelationReassign, PolarityMixed},
		{"block", fact("e3", "pm.task.state_changed", "T1", "agent:a", map[string]any{"prev_status": "running", "status": "blocked"}), nil, RelationBlock, PolarityNegative},
		{"unblock", fact("e4", "pm.audit_recorded", "T1", "agent:a", map[string]any{"change_type": "status_changed", "from_value": "blocked", "to_value": "running"}), nil, RelationUnblock, PolarityPositive},
		{"complete", fact("e5", "pm.task.state_changed", "T1", "agent:a", map[string]any{"prev_status": "running", "status": "completed"}), nil, RelationComplete, PolarityPositive},
		{"dependency-release", fact("e6", "pm.task.state_changed", "UP", "agent:a", map[string]any{"prev_status": "running", "status": "completed"}), []Dependency{{ProjectID: "P1", UpstreamTaskID: "UP", DownstreamTaskID: "DOWN", SourceEventID: "dep1"}}, RelationDependencyRelease, PolarityPositive},
		{"review-accept", fact("e7", "pm.audit_recorded", "R1", "agent:reviewer", map[string]any{"change_type": "review_verdict", "to_value": "pass", "detail": map[string]any{"blocking": false}}), nil, RelationReviewAccept, PolarityPositive},
		{"review-reject-mixed", fact("e8", "pm.audit_recorded", "R1", "agent:reviewer", map[string]any{"change_type": "review_verdict", "to_value": "reject", "detail": map[string]any{"blocking": true}}), nil, RelationReviewReject, PolarityMixed},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, _, diag := e.Evaluate(tc.f, tc.deps)
			if diag != nil {
				t.Fatalf("unexpected diagnostic: %+v", diag)
			}
			found := false
			for _, v := range got {
				if v.RelationType == tc.rel && v.Polarity == tc.pol {
					found = true
					if v.Magnitude < 1 || v.Magnitude > 3 || v.Confidence != "high" {
						t.Fatalf("invalid effect: %+v", v)
					}
				}
			}
			if !found {
				t.Fatalf("relation %s not found in %+v", tc.rel, got)
			}
			if tc.rel == RelationReviewReject {
				if got[0].AfterState["progress"] != "delayed" || got[0].AfterState["quality"] != "improved_or_unknown" {
					t.Fatalf("mixed dimensions missing: %#v", got[0].AfterState)
				}
			}
		})
	}
	got, _, diag := e.Evaluate(fact("bad", "pm.task.assigned", "T1", "system", map[string]any{}), nil)
	if len(got) != 0 || diag == nil {
		t.Fatalf("missing fields must fail closed")
	}
}

func testRepo(t *testing.T) *SQLiteRepository {
	t.Helper()
	db, err := persistence.Open(t.TempDir() + "/effects.db")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	if err := persistence.NewMigrator(db).Up(context.Background()); err != nil {
		t.Fatal(err)
	}
	r, err := NewSQLiteRepository(db)
	if err != nil {
		t.Fatal(err)
	}
	return r
}

func TestProjectorDuplicateTenTimesAndVersionCoexistence(t *testing.T) {
	ctx := context.Background()
	r := testRepo(t)
	f := fact("event-1", "pm.task.state_changed", "T1", "agent:a", map[string]any{"prev_status": "running", "status": "completed"})
	p := NewProjector(r, NewEngine(""))
	for i := 0; i < 10; i++ {
		if err := p.ProjectFact(ctx, f); err != nil {
			t.Fatal(err)
		}
	}
	got, _, err := r.List(ctx, Filter{ProjectID: "P1", RuleVersion: RuleVersionV1})
	if err != nil || len(got) != 1 {
		t.Fatalf("got=%d err=%v", len(got), err)
	}
	p2 := NewProjector(r, NewEngine("collaboration-effect.mvp.v2-shadow"))
	if err := p2.ProjectFact(ctx, f); err != nil {
		t.Fatal(err)
	}
	v2, _, _ := r.List(ctx, Filter{ProjectID: "P1", RuleVersion: "collaboration-effect.mvp.v2-shadow"})
	if len(v2) != 1 || v2[0].EffectID == got[0].EffectID {
		t.Fatalf("version partition failed")
	}
}

type sliceSource struct{ events []*fakeEvent }
type fakeEvent struct{}

func canonical(t *testing.T, e []Effect) string {
	t.Helper()
	sort.Slice(e, func(i, j int) bool { return e[i].EffectID < e[j].EffectID })
	b, err := json.Marshal(e)
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

func TestOutOfOrderDependencyReplayConvergesWhenOrderedLedgerIsReplayed(t *testing.T) {
	ctx := context.Background()
	r := testRepo(t)
	p := NewProjector(r, NewEngine(""))
	dep := fact("01DEP", "pm.audit_recorded", "", "agent:lead", map[string]any{"change_type": "dependency_added", "detail": map[string]any{"from": "DOWN", "to": "UP", "plan_id": "PL"}})
	complete := fact("02DONE", "pm.task.state_changed", "UP", "agent:a", map[string]any{"prev_status": "running", "status": "completed"})
	if err := p.ProjectFact(ctx, complete); err != nil {
		t.Fatal(err)
	}
	if err := p.ProjectFact(ctx, dep); err != nil {
		t.Fatal(err)
	}
	first, _, _ := r.List(ctx, Filter{ProjectID: "P1", RuleVersion: RuleVersionV1})
	if len(first) != 1 {
		t.Fatalf("out of order should not guess release, got %d", len(first))
	}
	if err := r.DeleteVersion(ctx, RuleVersionV1); err != nil {
		t.Fatal(err)
	}
	if err := p.ProjectFact(ctx, dep); err != nil {
		t.Fatal(err)
	}
	if err := p.ProjectFact(ctx, complete); err != nil {
		t.Fatal(err)
	}
	second, _, _ := r.List(ctx, Filter{ProjectID: "P1", RuleVersion: RuleVersionV1})
	if len(second) != 2 {
		t.Fatalf("ordered replay should pair dependency, got %d", len(second))
	}
	if canonical(t, second) == "" || reflect.DeepEqual(first, second) {
		t.Fatal("rebuild did not add release")
	}
}
