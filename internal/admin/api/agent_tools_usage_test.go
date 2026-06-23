package api

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"testing"
	"time"

	pm "github.com/oopslink/agent-center/internal/projectmanager"
	"github.com/oopslink/agent-center/internal/usage"
	usagesql "github.com/oopslink/agent-center/internal/usage/sqlite"
)

// TestReportUsage exercises the v2.15.0 I28/F2 report_usage agent-tool end to
// end over the admin HTTP surface: cost materialization at the in-force price,
// the unknown-model → cost 0 path, project_id derivation from task_id (and the
// "" interaction bucket for a task-less turn), and source='report' persistence.
func TestReportUsage(t *testing.T) {
	f := newWriteToolsFixture(t)
	ueRepo := usagesql.NewUsageEventRepo(f.db)
	mpRepo := usagesql.NewModelPriceRepo(f.db)
	f.deps.UsageEventRepo = ueRepo
	f.deps.ModelPriceRepo = mpRepo
	f.addWorkerToken(t, "acat_ru", atWorker1)

	ctx := context.Background()
	if err := mpRepo.Upsert(ctx, usage.ModelPrice{
		Model:                   "claude-opus-4-8",
		EffectiveFrom:           time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC),
		InputPerMTokMicros:      5_000_000,
		OutputPerMTokMicros:     25_000_000,
		CacheReadPerMTokMicros:  500_000,
		CacheWritePerMTokMicros: 6_250_000,
	}); err != nil {
		t.Fatal(err)
	}

	srv := f.server(t)
	post := func(t *testing.T, body map[string]any) (int, map[string]any) {
		t.Helper()
		b, _ := json.Marshal(body)
		req, _ := http.NewRequest(http.MethodPost, srv.URL+"/admin/agent-tools/report_usage", bytes.NewReader(b))
		req.Header.Set("Authorization", "Bearer acat_ru")
		req.Header.Set("Content-Type", "application/json")
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		defer resp.Body.Close()
		var out map[string]any
		_ = json.NewDecoder(resp.Body).Decode(&out)
		return resp.StatusCode, out
	}

	// 1) Known model, no task → cost materialized, project "" (interaction bucket).
	//    cost = (1000*5e6 + 500*25e6 + 200*5e5 + 100*6.25e6)/1e6
	//         = (5e9 + 12.5e9 + 1e8 + 6.25e8)/1e6 = 18_225_000_000/1e6 = 18225.
	status, out := post(t, map[string]any{
		"agent_id": atAgent1, "model": "claude-opus-4-8",
		"input_tokens": 1000, "output_tokens": 500,
		"cache_read_tokens": 200, "cache_write_tokens": 100,
	})
	if status != http.StatusOK || out["ok"] != true {
		t.Fatalf("report_usage status=%d out=%v", status, out)
	}
	if got := int64(out["cost_micros"].(float64)); got != 18225 {
		t.Fatalf("cost_micros = %d, want 18225", got)
	}
	if out["project_id"] != "" {
		t.Fatalf("task-less turn project_id = %v, want empty", out["project_id"])
	}

	// 2) Unknown model → cost 0, tokens still recorded (recompute on first pricing).
	status, out = post(t, map[string]any{
		"agent_id": atAgent1, "model": "mystery-model-x",
		"input_tokens": 10, "output_tokens": 20,
	})
	if status != http.StatusOK {
		t.Fatalf("unknown-model status=%d out=%v", status, out)
	}
	if got := int64(out["cost_micros"].(float64)); got != 0 {
		t.Fatalf("unknown model cost_micros = %d, want 0", got)
	}

	// 3) With a real task → project_id derived from the task (authoritative).
	tid := f.seedRunningTask(t)
	tk, err := f.pmSvc.GetTask(ctx, pm.TaskID(tid))
	if err != nil {
		t.Fatal(err)
	}
	wantProj := string(tk.ProjectID())
	if wantProj == "" {
		t.Fatal("seeded task has empty project id")
	}
	status, out = post(t, map[string]any{
		"agent_id": atAgent1, "model": "claude-opus-4-8", "task_id": tid,
		"input_tokens": 100, "output_tokens": 0,
	})
	if status != http.StatusOK || out["project_id"] != wantProj {
		t.Fatalf("task turn project_id = %v, want %q (status=%d)", out["project_id"], wantProj, status)
	}

	// 4) Missing model → 400.
	if status, _ := post(t, map[string]any{"agent_id": atAgent1}); status != http.StatusBadRequest {
		t.Fatalf("missing model status = %d, want 400", status)
	}

	// Persistence: 3 successful events for the agent, all source='report'.
	evs, err := ueRepo.ListByAgent(ctx, "agent:"+atAgent1, time.Time{}, time.Time{})
	if err != nil {
		t.Fatal(err)
	}
	if len(evs) != 3 {
		t.Fatalf("recorded %d usage events, want 3", len(evs))
	}
	for _, e := range evs {
		if e.Source != usage.SourceReport {
			t.Fatalf("event source = %q, want report", e.Source)
		}
	}
}
