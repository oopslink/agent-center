package api

import (
	"net/http"
	"testing"
)

// The model catalog import must apply VALID batches and reject INVALID ones WHOLE —
// a bad row (malformed JSON, duplicate model_id, negative cost) leaves the catalog
// untouched (no half-swallow). Also covers CRUD round-trip via the agent tools.
func TestModelCatalog_CRUDAndWholeBatchImport(t *testing.T) {
	f := newWriteToolsFixture(t)
	f.addWorkerToken(t, "acat_w1", atWorker1)
	srv := f.server(t)

	catalogLen := func() int {
		st, body := postBearer(t, srv.URL, "/admin/agent-tools/list_model_catalog_entry", "acat_w1", map[string]any{"agent_id": atAgent1})
		if st != http.StatusOK {
			t.Fatalf("list status=%d body=%v", st, body)
		}
		entries, _ := body["entries"].([]any)
		return len(entries)
	}

	// create one entry.
	st, body := postBearer(t, srv.URL, "/admin/agent-tools/create_model_catalog_entry", "acat_w1",
		map[string]any{"agent_id": atAgent1, "model_id": "opus", "input_cost": 15, "output_cost": 75, "context_window": 200000, "tier": "hardest"})
	if st != http.StatusCreated {
		t.Fatalf("create status=%d body=%v", st, body)
	}
	id, _ := body["id"].(string)
	if catalogLen() != 1 {
		t.Fatalf("after create: len=%d want 1", catalogLen())
	}
	// duplicate model_id → 409.
	st, _ = postBearer(t, srv.URL, "/admin/agent-tools/create_model_catalog_entry", "acat_w1",
		map[string]any{"agent_id": atAgent1, "model_id": "opus"})
	if st != http.StatusConflict {
		t.Fatalf("dup create status=%d want 409", st)
	}
	// update.
	st, _ = postBearer(t, srv.URL, "/admin/agent-tools/update_model_catalog_entry", "acat_w1",
		map[string]any{"agent_id": atAgent1, "id": id, "model_id": "opus", "input_cost": 10, "tier": "hard"})
	if st != http.StatusOK {
		t.Fatalf("update status=%d", st)
	}
	// delete.
	st, _ = postBearer(t, srv.URL, "/admin/agent-tools/delete_model_catalog_entry", "acat_w1",
		map[string]any{"agent_id": atAgent1, "id": id})
	if st != http.StatusOK {
		t.Fatalf("delete status=%d", st)
	}
	if catalogLen() != 0 {
		t.Fatalf("after delete: len=%d want 0", catalogLen())
	}

	// import a VALID replace batch of 2.
	st, body = postBearer(t, srv.URL, "/admin/agent-tools/import_model_catalog", "acat_w1",
		map[string]any{"agent_id": atAgent1, "mode": "replace", "json": `[{"model_id":"a","input_cost":1,"output_cost":1,"context_window":1000,"tier":"cheap"},{"model_id":"b","input_cost":2,"output_cost":2}]`})
	if st != http.StatusOK {
		t.Fatalf("valid import status=%d body=%v", st, body)
	}
	if catalogLen() != 2 {
		t.Fatalf("after valid import: len=%d want 2", catalogLen())
	}

	// INVALID: duplicate model_id in batch → 400, catalog UNCHANGED (still 2).
	st, _ = postBearer(t, srv.URL, "/admin/agent-tools/import_model_catalog", "acat_w1",
		map[string]any{"agent_id": atAgent1, "mode": "replace", "json": `[{"model_id":"x"},{"model_id":"x"}]`})
	if st != http.StatusBadRequest {
		t.Fatalf("dup import status=%d want 400", st)
	}
	if catalogLen() != 2 {
		t.Fatalf("after rejected dup import: len=%d want 2 (whole batch rejected)", catalogLen())
	}

	// INVALID: negative cost → 400, unchanged.
	st, _ = postBearer(t, srv.URL, "/admin/agent-tools/import_model_catalog", "acat_w1",
		map[string]any{"agent_id": atAgent1, "mode": "replace", "json": `[{"model_id":"y","input_cost":-5}]`})
	if st != http.StatusBadRequest {
		t.Fatalf("negative-cost import status=%d want 400", st)
	}
	if catalogLen() != 2 {
		t.Fatalf("after rejected negative-cost import: len=%d want 2", catalogLen())
	}

	// INVALID: malformed JSON → 400, unchanged.
	st, _ = postBearer(t, srv.URL, "/admin/agent-tools/import_model_catalog", "acat_w1",
		map[string]any{"agent_id": atAgent1, "mode": "replace", "json": `{not json`})
	if st != http.StatusBadRequest {
		t.Fatalf("malformed-json import status=%d want 400", st)
	}
	if catalogLen() != 2 {
		t.Fatalf("after rejected malformed import: len=%d want 2", catalogLen())
	}

	// INVALID: missing model_id → 400, unchanged.
	st, _ = postBearer(t, srv.URL, "/admin/agent-tools/import_model_catalog", "acat_w1",
		map[string]any{"agent_id": atAgent1, "mode": "replace", "json": `[{"display_name":"no id"}]`})
	if st != http.StatusBadRequest {
		t.Fatalf("missing-model_id import status=%d want 400", st)
	}
	if catalogLen() != 2 {
		t.Fatalf("after rejected missing-id import: len=%d want 2", catalogLen())
	}
}
