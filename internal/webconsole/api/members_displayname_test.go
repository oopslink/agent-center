package api

import (
	"encoding/json"
	"net/http"
	"testing"
)

// v2.7 #160: GET /api/members must include each member's display_name (resolved
// from the Identity via IdentityRepo) so the UI can render human names for
// message senders + the participant list instead of raw "user:<id>" refs.
func TestAPI_ListMembers_IncludesDisplayName(t *testing.T) {
	deps, db := setupAPIWithAuth(t)
	sess := setupTestSession(t, db, deps) // creates identity "testuser" + owner member
	s := newTestServer(t, deps)
	defer s.Close()

	resp := orgScopedGet(t, s.URL+"/api/members", sess)
	body := responseBytes(t, resp)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("list members: status=%d body=%s", resp.StatusCode, body)
	}
	var members []map[string]any
	if err := json.Unmarshal(body, &members); err != nil {
		t.Fatalf("decode: %v body=%s", err, body)
	}
	var found bool
	for _, m := range members {
		if m["identity_id"] == sess.IdentityID {
			found = true
			if m["display_name"] != "testuser" {
				t.Fatalf("member display_name = %v, want %q (raw ref shown instead of name)", m["display_name"], "testuser")
			}
		}
	}
	if !found {
		t.Fatalf("session member (%s) not in list: %s", sess.IdentityID, body)
	}
}
