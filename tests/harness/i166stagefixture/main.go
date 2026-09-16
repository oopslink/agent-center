// Command i166stagefixture adds staged-plan acceptance data to an isolated
// agent-center database through the Project Manager application service.
//
// It is deliberately a test-only command: the surrounding harness creates the
// database, identity, project, plan, and tasks through the real HTTP service,
// then invokes this command to exercise the stage authoring boundary that is
// otherwise exposed only through agent tools.
package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"os"

	"github.com/oopslink/agent-center/internal/clock"
	"github.com/oopslink/agent-center/internal/idgen"
	"github.com/oopslink/agent-center/internal/outbox/sqlite"
	"github.com/oopslink/agent-center/internal/persistence"
	pm "github.com/oopslink/agent-center/internal/projectmanager"
	pmservice "github.com/oopslink/agent-center/internal/projectmanager/service"
	pmsql "github.com/oopslink/agent-center/internal/projectmanager/sqlite"
)

type result struct {
	StageA string   `json:"stage_a"`
	StageB string   `json:"stage_b"`
	TasksA []string `json:"tasks_a"`
	TasksB []string `json:"tasks_b"`
}

func main() {
	var dbPath, planID, actor, a1, a2, b1, b2 string
	flag.StringVar(&dbPath, "db", "", "isolated SQLite database path")
	flag.StringVar(&planID, "plan", "", "plan id")
	flag.StringVar(&actor, "actor", "", "project-member identity ref")
	flag.StringVar(&a1, "a1", "", "first task in stage A")
	flag.StringVar(&a2, "a2", "", "second task in stage A")
	flag.StringVar(&b1, "b1", "", "first task in stage B")
	flag.StringVar(&b2, "b2", "", "second task in stage B")
	flag.Parse()
	if dbPath == "" || planID == "" || actor == "" || a1 == "" || a2 == "" || b1 == "" || b2 == "" {
		fatalf("all of --db, --plan, --actor, --a1, --a2, --b1, and --b2 are required")
	}

	db, err := persistence.Open(dbPath)
	if err != nil {
		fatalf("open isolated database: %v", err)
	}
	defer db.Close()

	clk := clock.SystemClock{}
	gen := idgen.NewGenerator(clk)
	svc := pmservice.New(pmservice.Deps{
		DB:        db,
		Projects:  pmsql.NewProjectRepo(db),
		Members:   pmsql.NewProjectMemberRepo(db),
		Tasks:     pmsql.NewTaskRepo(db),
		TaskSubs:  pmsql.NewTaskSubscriberRepo(db),
		IssueSubs: pmsql.NewIssueSubscriberRepo(db),
		Plans:     pmsql.NewPlanRepo(db),
		Stages:    pmsql.NewStageRepo(db),
		Outbox:    sqlite.NewOutboxRepo(db),
		OrgSeq:    pmsql.NewOrgSequenceRepo(db),
		IDGen:     gen,
		Clock:     clk,
	})

	ctx := context.Background()
	pid := pm.PlanID(planID)
	actorRef := pm.IdentityRef(actor)
	stageA, err := svc.CreateStage(ctx, pmservice.CreateStageCommand{
		PlanID: pid, Name: "Build and unit verification", Actor: actorRef,
	})
	if err != nil {
		fatalf("create stage A: %v", err)
	}
	stageB, err := svc.CreateStage(ctx, pmservice.CreateStageCommand{
		PlanID: pid, Name: "Integrated acceptance", DependsOnStages: []pm.StageID{stageA}, Actor: actorRef,
	})
	if err != nil {
		fatalf("create stage B: %v", err)
	}

	assign := func(task string, stage pm.StageID) {
		if err := svc.AssignTaskToStage(ctx, pid, pm.TaskID(task), stage, actorRef); err != nil {
			fatalf("assign task %s to stage %s: %v", task, stage, err)
		}
	}
	assign(a1, stageA)
	assign(a2, stageA)
	assign(b1, stageB)
	assign(b2, stageB)

	// Within-stage dependencies. Cross-stage ordering is represented by
	// stageB.depends_on_stages=[stageA], as required by the domain invariant.
	if err := svc.AddPlanDependency(ctx, pid, pm.TaskID(a2), pm.TaskID(a1), actorRef); err != nil {
		fatalf("add stage A dependency: %v", err)
	}
	if err := svc.AddPlanDependency(ctx, pid, pm.TaskID(b2), pm.TaskID(b1), actorRef); err != nil {
		fatalf("add stage B dependency: %v", err)
	}

	if err := json.NewEncoder(os.Stdout).Encode(result{
		StageA: string(stageA), StageB: string(stageB),
		TasksA: []string{a1, a2}, TasksB: []string{b1, b2},
	}); err != nil {
		fatalf("encode result: %v", err)
	}
}

func fatalf(format string, args ...any) {
	fmt.Fprintf(os.Stderr, "i166stagefixture: "+format+"\n", args...)
	os.Exit(1)
}
