// Package workerdaemon — secret_resolver_test.go: verifies the thin
// adminClientSecretResolver shim forwards Resolve to AdminClient.ResolveSecret.
package workerdaemon

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"testing"
)

func TestAdminClientSecretResolver_DelegatesToClient(t *testing.T) {
	fs, client, cleanup := newFakeServer(t)
	defer cleanup()
	// fakeServer.registerRoutes maps /admin/secret/user-secret/resolve to
	// a deterministic base64 payload.
	r := NewAdminClientSecretResolver(client)
	if r == nil {
		t.Fatal("resolver should not be nil for valid client")
	}
	plain, err := r.Resolve(context.Background(), "db_password")
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	// fakeServer returns base64("secret-val") = c2VjcmV0LXZhbA==
	want, _ := base64.StdEncoding.DecodeString("c2VjcmV0LXZhbA==")
	if string(plain) != string(want) {
		t.Fatalf("plaintext mismatch: got=%q want=%q", plain, want)
	}
	// Confirm the request hit the right path with the right body.
	reqs := fs.reqs()
	if len(reqs) != 1 || reqs[0].Path != "/admin/secret/user-secret/resolve" {
		t.Fatalf("expected one resolve request; got %+v", reqs)
	}
	if reqs[0].Method != http.MethodPost {
		t.Fatalf("method=%s", reqs[0].Method)
	}
	var body map[string]string
	_ = json.Unmarshal(reqs[0].Body, &body)
	if body["name"] != "db_password" {
		t.Fatalf("body.name=%q", body["name"])
	}
}

func TestAdminClientSecretResolver_NilClientReturnsNil(t *testing.T) {
	if r := NewAdminClientSecretResolver(nil); r != nil {
		t.Fatalf("nil client should return nil resolver, got %T", r)
	}
}
