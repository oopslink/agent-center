package api

import (
	"encoding/json"
	"net/http"
	"testing"
)

// TestAPI_ChannelArchive_DefaultExcludesAndFilters is the v2.9.1 task-169c598d
// guard: an archived channel is excluded from the default conversation list,
// reachable via ?status=archived (or ?status=all), and read-only — posting a
// message to it rejects with 409, the same semantic as an archived project
// (#297). Mirrors TestPM_ListProjects_StatusFilter for the channel surface.
func TestAPI_ChannelArchive_DefaultExcludesAndFilters(t *testing.T) {
	deps, db := setupAPIWithAuth(t)
	sess := setupTestSession(t, db, deps)
	s := newTestServer(t, deps)
	defer s.Close()

	mkChannel := func(name string) string {
		resp := orgScopedPost(t, s.URL+"/api/conversations", `{"kind":"channel","name":"`+name+`"}`, sess)
		if resp.StatusCode != http.StatusCreated {
			t.Fatalf("create channel %q status=%d", name, resp.StatusCode)
		}
		var b map[string]any
		json.NewDecoder(resp.Body).Decode(&b)
		id, _ := b["conversation_id"].(string)
		if id == "" {
			t.Fatalf("create %q: missing conversation_id; got %v", name, b)
		}
		return id
	}
	alpha := mkChannel("alpha")
	beta := mkChannel("beta")

	// Archive beta (archived_by defaults to the session actor).
	if resp := orgScopedPost(t, s.URL+"/api/conversations/"+beta+"/archive", `{}`, sess); resp.StatusCode != http.StatusOK {
		t.Fatalf("archive beta status=%d", resp.StatusCode)
	}

	listChannels := func(query string) map[string]string { // channel id -> status
		resp := orgScopedGet(t, s.URL+"/api/conversations"+query, sess)
		if resp.StatusCode != http.StatusOK {
			t.Fatalf("list %q status=%d", query, resp.StatusCode)
		}
		var rows []map[string]any
		json.NewDecoder(resp.Body).Decode(&rows)
		out := map[string]string{}
		for _, row := range rows {
			if row["kind"] == "channel" {
				id, _ := row["id"].(string)
				st, _ := row["status"].(string)
				out[id] = st
			}
		}
		return out
	}

	// Default (no ?status=) → active alpha present, archived beta excluded.
	def := listChannels("?kind=channel")
	if _, ok := def[alpha]; !ok {
		t.Fatalf("default channel list must include active alpha, got %+v", def)
	}
	if _, ok := def[beta]; ok {
		t.Fatalf("default channel list must EXCLUDE archived beta, got %+v", def)
	}

	// ?status=archived → only beta (archived).
	if arch := listChannels("?kind=channel&status=archived"); len(arch) != 1 || arch[beta] != "archived" {
		t.Fatalf("?status=archived should be [beta archived], got %+v", arch)
	}

	// ?status=all → both.
	all := listChannels("?kind=channel&status=all")
	if _, ok := all[alpha]; !ok {
		t.Fatalf("?status=all missing alpha, got %+v", all)
	}
	if _, ok := all[beta]; !ok {
		t.Fatalf("?status=all missing beta, got %+v", all)
	}

	// Read-only: posting to the archived channel → 409 (project-archive parity).
	if resp := orgScopedPost(t, s.URL+"/api/conversations/"+beta+"/messages", `{"content":"hi"}`, sess); resp.StatusCode != http.StatusConflict {
		t.Fatalf("post to archived channel: got %d, want 409", resp.StatusCode)
	}
}
