package api

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"
	"testing"

	"github.com/oopslink/agent-center/internal/identity"
)

// v2.7.1 #215: a DM with yourself is rejected.
func TestDM_SelfRejected(t *testing.T) {
	deps, _ := setupAPI(t) // caller = user:hayang
	s := newTestServer(t, deps)
	defer s.Close()
	resp, _ := http.Post(s.URL+"/api/conversations", "application/json",
		strings.NewReader(`{"kind":"dm","members":["user:hayang"]}`))
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("self-DM got %d, want 400", resp.StatusCode)
	}
}

// v2.7.1 #215: a second createDM for the same (caller, peer) reuses the existing
// conversation (200 + existing id) instead of opening a duplicate.
func TestDM_Dedup(t *testing.T) {
	deps, _ := setupAPI(t)
	s := newTestServer(t, deps)
	defer s.Close()

	first, _ := http.Post(s.URL+"/api/conversations", "application/json",
		strings.NewReader(`{"kind":"dm","members":["agent:s-1"]}`))
	if first.StatusCode != http.StatusCreated {
		t.Fatalf("first DM got %d, want 201", first.StatusCode)
	}
	var b1 map[string]any
	_ = json.NewDecoder(first.Body).Decode(&b1)
	id1, _ := b1["conversation_id"].(string)

	second, _ := http.Post(s.URL+"/api/conversations", "application/json",
		strings.NewReader(`{"kind":"dm","members":["agent:s-1"]}`))
	if second.StatusCode != http.StatusOK {
		t.Fatalf("second DM got %d, want 200 (dedup reuse)", second.StatusCode)
	}
	var b2 map[string]any
	_ = json.NewDecoder(second.Body).Decode(&b2)
	if b2["conversation_id"] != id1 {
		t.Fatalf("dedup returned %v, want existing %s", b2["conversation_id"], id1)
	}
	if b2["existing"] != true {
		t.Fatalf("dedup must flag existing=true, got %v", b2["existing"])
	}
}

// v2.7.1 #215: the DM list enriches each DM with the peer (participants − self):
// peer_identity_id (bare member-id) + peer_display_name (resolved).
func TestDM_ListPeerEnrichment(t *testing.T) {
	deps, db := setupAPIWithAuth(t)
	sess := setupTestSession(t, db, deps)
	s := newTestServer(t, deps)
	defer s.Close()
	ctx := context.Background()

	// Create a peer user identity so its display name resolves.
	hash, _ := identity.HashPasscode("123456")
	peer, err := identity.IdentityFactory{}.NewUser("PeerUser", hash)
	if err != nil {
		t.Fatal(err)
	}
	if err := identity.NewSQLiteIdentityRepo(db).Save(ctx, peer); err != nil {
		t.Fatal(err)
	}
	peerRef := "user:" + peer.ID()

	// Open the DM as the session caller.
	resp := orgScopedPost(t, s.URL+"/api/conversations", `{"kind":"dm","members":["`+peerRef+`"]}`, sess)
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("create DM got %d", resp.StatusCode)
	}

	// List DMs → the row carries the peer fields.
	listResp := orgScopedGet(t, s.URL+"/api/conversations?kind=dm", sess)
	var rows []map[string]any
	if err := json.NewDecoder(listResp.Body).Decode(&rows); err != nil {
		t.Fatal(err)
	}
	var found bool
	for _, row := range rows {
		if row["peer_identity_id"] == peer.ID() {
			found = true
			if row["peer_display_name"] != "PeerUser" {
				t.Errorf("peer_display_name=%v want PeerUser", row["peer_display_name"])
			}
		}
	}
	if !found {
		t.Fatalf("DM row with peer_identity_id=%s not found: %+v", peer.ID(), rows)
	}
}
