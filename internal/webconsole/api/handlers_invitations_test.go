package api

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"
	"testing"

	"github.com/oopslink/agent-center/internal/identity"
)

func TestAPI_Invitations_CreateListCancelAcceptRejected(t *testing.T) {
	deps, db := setupAPIWithAuth(t)
	owner := setupTestSession(t, db, deps)
	s := newTestServer(t, deps)
	defer s.Close()

	ctx := context.Background()
	hash, _ := identity.HashPasscode("Passw0rd1!")
	invitee, err := identity.IdentityFactory{}.NewUser("invitee", hash)
	if err != nil {
		t.Fatal(err)
	}
	if err := deps.IdentityRepo.Save(ctx, invitee); err != nil {
		t.Fatal(err)
	}
	inviteeJWT, err := identity.MintJWT(invitee.ID(), testSigningKey)
	if err != nil {
		t.Fatal(err)
	}
	inviteeCookie := &http.Cookie{Name: jwtCookieName, Value: inviteeJWT}

	createResp := orgScopedPost(t, s.URL+"/api/invitations",
		`{"invitee_user_id":"`+invitee.ID()+`","role":"member"}`, owner)
	createBody := responseBytes(t, createResp)
	if createResp.StatusCode != http.StatusCreated {
		t.Fatalf("create invitation: status=%d body=%s", createResp.StatusCode, createBody)
	}
	var created map[string]any
	if err := json.Unmarshal(createBody, &created); err != nil {
		t.Fatal(err)
	}
	if created["status"] != "pending" || created["invitee_user_id"] != invitee.ID() {
		t.Fatalf("created invitation = %+v", created)
	}

	listResp := orgScopedGet(t, s.URL+"/api/invitations", owner)
	listBody := responseBytes(t, listResp)
	if listResp.StatusCode != http.StatusOK {
		t.Fatalf("list invitations: status=%d body=%s", listResp.StatusCode, listBody)
	}
	if !strings.Contains(string(listBody), invitee.ID()) {
		t.Fatalf("list body does not include invitee %s: %s", invitee.ID(), listBody)
	}

	cancelResp := orgScopedPost(t, s.URL+"/api/invitations/"+created["id"].(string)+"/cancel", `{}`, owner)
	cancelBody := responseBytes(t, cancelResp)
	if cancelResp.StatusCode != http.StatusOK {
		t.Fatalf("cancel invitation: status=%d body=%s", cancelResp.StatusCode, cancelBody)
	}
	var cancelled map[string]any
	if err := json.Unmarshal(cancelBody, &cancelled); err != nil {
		t.Fatal(err)
	}
	if cancelled["status"] != "cancelled" {
		t.Fatalf("cancelled status = %v, want cancelled", cancelled["status"])
	}

	req, err := http.NewRequest(http.MethodPost,
		s.URL+"/api/orgs/"+owner.OrgSlug+"/invitations/"+created["token"].(string)+"/accept",
		nil)
	if err != nil {
		t.Fatal(err)
	}
	req.AddCookie(inviteeCookie)
	acceptResp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer acceptResp.Body.Close()
	if acceptResp.StatusCode != http.StatusGone {
		t.Fatalf("accept cancelled invitation: status=%d, want 410", acceptResp.StatusCode)
	}

	delCreateResp := orgScopedPost(t, s.URL+"/api/invitations",
		`{"invitee_user_id":"`+invitee.ID()+`","role":"member"}`, owner)
	delCreateBody := responseBytes(t, delCreateResp)
	if delCreateResp.StatusCode != http.StatusCreated {
		t.Fatalf("create invitation to delete: status=%d body=%s", delCreateResp.StatusCode, delCreateBody)
	}
	var toDelete map[string]any
	if err := json.Unmarshal(delCreateBody, &toDelete); err != nil {
		t.Fatal(err)
	}
	delReq, err := http.NewRequest(http.MethodDelete,
		s.URL+"/api/orgs/"+owner.OrgSlug+"/invitations/"+toDelete["id"].(string),
		nil)
	if err != nil {
		t.Fatal(err)
	}
	delReq.AddCookie(owner.Cookie)
	delResp, err := http.DefaultClient.Do(delReq)
	if err != nil {
		t.Fatal(err)
	}
	defer delResp.Body.Close()
	if delResp.StatusCode != http.StatusNoContent {
		t.Fatalf("delete invitation: status=%d, want 204", delResp.StatusCode)
	}
	listAfterDelete := responseBytes(t, orgScopedGet(t, s.URL+"/api/invitations", owner))
	if strings.Contains(string(listAfterDelete), toDelete["id"].(string)) {
		t.Fatalf("deleted invitation still listed: %s", listAfterDelete)
	}
}
