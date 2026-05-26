package api

import (
	"context"
	"errors"
	"net"
	"net/http"
	"strings"

	"github.com/oopslink/agent-center/internal/admintoken"
)

// authKey is the request context key holding the verified bearer.
type authKey struct{}

// AuthContext is the verified bearer attached to each request after
// middleware authentication.
type AuthContext struct {
	TokenID admintoken.TokenID
	Owner   admintoken.Owner
	Scopes  []admintoken.Scope
	// ClientIP is the request's remote IP (host portion of
	// http.Request.RemoteAddr; empty for unix-socket requests where
	// RemoteAddr is "@"). v2.3-7c (task #27): added for audit log so
	// rate-limit / auth events can attribute cross-host traffic.
	ClientIP string
}

// HasScope reports whether the token carries the required scope or the
// superuser `*` scope.
func (a AuthContext) HasScope(s admintoken.Scope) bool {
	for _, have := range a.Scopes {
		if have == "*" || have == s {
			return true
		}
	}
	return false
}

// AuthFromContext pulls the AuthContext from a request context. Returns
// zero value + false when missing (means middleware was bypassed —
// unit-test harness or /admin/health).
func AuthFromContext(ctx context.Context) (AuthContext, bool) {
	v, ok := ctx.Value(authKey{}).(AuthContext)
	return v, ok
}

// RequireScope returns 403 + the standard error envelope if the request
// auth context lacks the required scope. Handlers that gate scope-sensitive
// operations call this near the top.
func RequireScope(w http.ResponseWriter, r *http.Request, scope admintoken.Scope) bool {
	auth, ok := AuthFromContext(r.Context())
	if !ok {
		writeError(w, http.StatusUnauthorized, "auth_missing",
			"endpoint requires authenticated bearer")
		return false
	}
	if !auth.HasScope(scope) {
		writeError(w, http.StatusForbidden, "scope_forbidden",
			"token lacks required scope: "+string(scope))
		return false
	}
	return true
}

// Verifier is the slim contract the middleware needs from
// admintoken/service.Service. Defined here so tests can stub without
// pulling the whole service tree.
type Verifier interface {
	VerifyPlaintext(ctx context.Context, plaintext string) (*admintoken.AdminToken, error)
	MarkUsedAsync(id admintoken.TokenID)
	// ConsumeEnrollToken burns a one-time-use enroll token. v2.4-D-A3
	// (task #37). Called by the middleware AFTER a successful verify
	// for enroll tokens; idempotent at the repo CAS level.
	ConsumeEnrollToken(ctx context.Context, id admintoken.TokenID) error
}

// AuthMiddleware wraps the admin mux. Every request except whitelisted
// public paths must carry a valid bearer. On 200-path the request ctx
// is enriched with AuthContext.
//
// Public path whitelist:
//   - GET /admin/health — uptime / readiness probe
//
// All other paths return 401 on missing/invalid/revoked tokens.
func AuthMiddleware(verifier Verifier) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if isPublicPath(r) {
				next.ServeHTTP(w, r)
				return
			}
			if verifier == nil {
				// No verifier wired — fail closed. This protects against
				// accidentally booting the admin endpoint without auth.
				writeError(w, http.StatusServiceUnavailable, "auth_not_wired",
					"admin endpoint started without a token verifier")
				return
			}
			plaintext, err := admintoken.ParseBearer(r.Header.Get("Authorization"))
			if err != nil {
				writeAuthError(w, err)
				return
			}
			tok, err := verifier.VerifyPlaintext(r.Context(), plaintext)
			if err != nil {
				writeAuthError(w, err)
				return
			}
			auth := AuthContext{
				TokenID:  tok.ID(),
				Owner:    tok.Owner(),
				Scopes:   tok.Scopes(),
				ClientIP: clientIPFromRequest(r),
			}
			// v2.4-D-A3 (task #37): enroll tokens are one-time-use.
			// Consume BEFORE forwarding so a racing 2nd verify on the
			// same plaintext fails (the repo CAS guarantees atomicity).
			// Long-term tokens go through MarkUsedAsync as before.
			if tok.IsEnroll() {
				if err := verifier.ConsumeEnrollToken(r.Context(), tok.ID()); err != nil {
					writeAuthError(w, err)
					return
				}
			} else {
				verifier.MarkUsedAsync(tok.ID())
			}
			ctx := context.WithValue(r.Context(), authKey{}, auth)
			next.ServeHTTP(w, r.WithContext(ctx))
		})
	}
}

// isPublicPath enumerates routes the middleware lets through. Keep this
// set tiny — every entry is an attack surface that bypasses auth.
func isPublicPath(r *http.Request) bool {
	if r.Method != http.MethodGet {
		return false
	}
	switch r.URL.Path {
	case "/admin/health":
		return true
	}
	return false
}

// writeAuthError maps token errors to HTTP responses + a stable error
// code in the JSON envelope. The body is intentionally terse — auth
// failures should not leak diagnostic context to the client.
func writeAuthError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, admintoken.ErrTokenMissingBearer):
		writeError(w, http.StatusUnauthorized, "auth_missing",
			"missing Authorization bearer")
	case errors.Is(err, admintoken.ErrTokenInvalidFormat):
		writeError(w, http.StatusUnauthorized, "auth_invalid_format",
			"bearer must start with "+admintoken.PlaintextPrefix)
	case errors.Is(err, admintoken.ErrTokenNotFound):
		writeError(w, http.StatusUnauthorized, "auth_unknown",
			"token unknown")
	case errors.Is(err, admintoken.ErrTokenRevoked):
		writeError(w, http.StatusUnauthorized, "auth_revoked",
			"token has been revoked")
	case errors.Is(err, admintoken.ErrTokenExpired):
		writeError(w, http.StatusUnauthorized, "auth_expired",
			"enroll token expired (30-min cap)")
	case errors.Is(err, admintoken.ErrTokenConsumed):
		writeError(w, http.StatusUnauthorized, "auth_consumed",
			"enroll token already used (one-time-use)")
	default:
		// Don't expose unexpected errors verbatim — could leak DB
		// internals on a misconfigured deploy.
		_ = err
		writeError(w, http.StatusUnauthorized, "auth_failed",
			"authentication failed")
	}
}

// suppress unused import in tests where strings may not be referenced
var _ = strings.TrimSpace

// clientIPFromRequest extracts the host part of r.RemoteAddr. For unix
// socket connections (`r.RemoteAddr` is "@" or similar) returns empty.
// For TCP connections returns the host (without port).
func clientIPFromRequest(r *http.Request) string {
	addr := strings.TrimSpace(r.RemoteAddr)
	if addr == "" || addr == "@" {
		return ""
	}
	// SplitHostPort handles both [::1]:7300 and 1.2.3.4:7300.
	host, _, err := net.SplitHostPort(addr)
	if err != nil {
		// Not host:port (e.g. unix socket peer with weird addr); return
		// the raw value so audit log still has something.
		return addr
	}
	return host
}
