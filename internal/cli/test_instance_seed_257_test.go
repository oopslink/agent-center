package cli

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// v2.8 #257: seedTestInstanceTenant orchestration — signup → project → channel
// over the real HTTP API with a session cookie carried (cookiejar) between
// calls, populating the access pack's signin + entity_refs. Mocked center
// endpoints (the full chain against a real center is the deployed smoke).

func TestSeedTestInstanceTenant_257_PopulatesAccessPack(t *testing.T) {
	const sessionCookie = "ac_session"
	var sawProjectCookie, sawChannelCookie bool

	mux := http.NewServeMux()
	mux.HandleFunc("POST /api/auth/signup", func(w http.ResponseWriter, r *http.Request) {
		// Auto-signin sets a session cookie the jar must carry forward.
		http.SetCookie(w, &http.Cookie{Name: sessionCookie, Value: "tok", Path: "/"})
		w.WriteHeader(http.StatusCreated)
		_, _ = w.Write([]byte(`{"organization_id":"organization-abc","identity_id":"user-1"}`))
	})
	mux.HandleFunc("POST /api/projects", func(w http.ResponseWriter, r *http.Request) {
		if _, err := r.Cookie(sessionCookie); err == nil {
			sawProjectCookie = true
		}
		_, _ = w.Write([]byte(`{"id":"project-xyz","name":"Alpha"}`))
	})
	mux.HandleFunc("POST /api/conversations", func(w http.ResponseWriter, r *http.Request) {
		if _, err := r.Cookie(sessionCookie); err == nil {
			sawChannelCookie = true
		}
		w.WriteHeader(http.StatusCreated)
		_, _ = w.Write([]byte(`{"conversation_id":"channel-789","kind":"channel"}`))
	})
	srv := httptest.NewServer(mux)
	defer srv.Close()

	pack := accessPack{ID: "t1", WebURL: srv.URL}
	if err := seedTestInstanceTenant(context.Background(), &pack); err != nil {
		t.Fatalf("seed: %v", err)
	}

	// Session cookie from signup must have been carried to the authed creates.
	if !sawProjectCookie || !sawChannelCookie {
		t.Errorf("session cookie not carried (cookiejar): project=%v channel=%v", sawProjectCookie, sawChannelCookie)
	}
	if pack.Signin == nil || pack.Entities == nil {
		t.Fatal("signin / entities not populated")
	}
	if pack.Signin.OrgSlug != "acme-t1" || pack.Signin.Passcode == "" || pack.Signin.Email == "" {
		t.Errorf("signin = %+v", *pack.Signin)
	}
	if pack.Entities.OrgID != "organization-abc" || pack.Entities.ProjectID != "project-xyz" || pack.Entities.ChannelID != "channel-789" {
		t.Errorf("entities = %+v", *pack.Entities)
	}
	if !strings.Contains(pack.WebLogin, "seeded") || !strings.Contains(pack.EntityIDs, "seeded") {
		t.Errorf("hints not updated to seeded: web_login=%q entity_ids=%q", pack.WebLogin, pack.EntityIDs)
	}
}

func TestSeedTestInstanceTenant_257_SignupFailureSurfaces(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("POST /api/auth/signup", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusBadRequest)
		_, _ = w.Write([]byte(`{"error":"email_required"}`))
	})
	srv := httptest.NewServer(mux)
	defer srv.Close()

	pack := accessPack{ID: "t1", WebURL: srv.URL}
	err := seedTestInstanceTenant(context.Background(), &pack)
	if err == nil {
		t.Fatal("expected signup failure to surface as an error")
	}
	if !strings.Contains(err.Error(), "signup") {
		t.Errorf("error should mention the failed step: %v", err)
	}
	if pack.Signin != nil {
		t.Error("pack must not be populated on seed failure")
	}
}
