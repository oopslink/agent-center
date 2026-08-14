package api

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/oopslink/agent-center/internal/admintoken"
	authz "github.com/oopslink/agent-center/internal/authorization"
	"github.com/oopslink/agent-center/internal/persistence"
)

func TestRequireScope_DelegatesToAuthorizationService(t *testing.T) {
	db, err := persistence.Open(t.TempDir() + "/admin-authz.db")
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if err := persistence.NewMigrator(db).Up(context.Background()); err != nil {
		t.Fatal(err)
	}
	svc := authz.New(authz.Deps{DB: db})
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !RequireScope(w, r, "secret:resolve") {
			return
		}
		w.WriteHeader(http.StatusNoContent)
	})
	tok := mintAR(t, admintoken.Scope("secret:resolve"))
	h := AuthMiddleware(&fakeVerifier{tokens: map[string]*admintoken.AdminToken{"acat_ok": tok}})(
		WithDeps(HandlerDeps{Authorizer: svc})(handler))

	req := httptest.NewRequest(http.MethodPost, "/admin/secret/user-secret/resolve", nil)
	req.Header.Set("Authorization", "Bearer acat_ok")
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusNoContent {
		t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
	}
}

func TestRequireScope_AuthorizationDelegateRejectsMissingScope(t *testing.T) {
	db, err := persistence.Open(t.TempDir() + "/admin-authz-deny.db")
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if err := persistence.NewMigrator(db).Up(context.Background()); err != nil {
		t.Fatal(err)
	}
	svc := authz.New(authz.Deps{DB: db})
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !RequireScope(w, r, "secret:resolve") {
			return
		}
		w.WriteHeader(http.StatusNoContent)
	})
	tok := mintAR(t, admintoken.Scope("task:*"))
	h := AuthMiddleware(&fakeVerifier{tokens: map[string]*admintoken.AdminToken{"acat_no": tok}})(
		WithDeps(HandlerDeps{Authorizer: svc})(handler))

	req := httptest.NewRequest(http.MethodPost, "/admin/secret/user-secret/resolve", nil)
	req.Header.Set("Authorization", "Bearer acat_no")
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusForbidden {
		t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), "scope_forbidden") {
		t.Fatalf("body=%s", rec.Body.String())
	}
}
