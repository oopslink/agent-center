package api

import (
	"context"
	"errors"
	"net/http"
	"strings"
	"time"

	"github.com/oopslink/agent-center/internal/identity"
)

const jwtCookieName = "ac_session"

// bootstrapHandler handles GET /api/auth/bootstrap (PUBLIC — exempt from the
// auth middleware via the /api/auth/ prefix). v2.7 #145: reports whether the
// system has been initialized (any user identity exists). The SPA calls this
// when no session is present to decide signup (fresh install, initialized=false)
// vs signin (initialized=true) WITHOUT bouncing off an authenticated
// /api/orgs 401. Anonymous-safe: returns only a single boolean, no PII.
func (s *Server) bootstrapHandler(w http.ResponseWriter, r *http.Request) {
	d := hd(r)
	initialized := false
	if d.IdentityRepo != nil {
		ids, err := d.IdentityRepo.List(r.Context())
		if err != nil {
			writeError(w, http.StatusInternalServerError, "bootstrap_failed", err.Error())
			return
		}
		initialized = len(ids) > 0
	}
	writeJSON(w, http.StatusOK, map[string]any{"initialized": initialized})
}

// signupHandler handles POST /api/auth/signup.
func (s *Server) signupHandler(w http.ResponseWriter, r *http.Request) {
	d := hd(r)
	if d.SignupSvc == nil {
		writeError(w, http.StatusNotImplemented, "not_configured", "auth not configured")
		return
	}
	var body struct {
		DisplayName      string `json:"display_name"`
		Passcode         string `json:"passcode"`
		OrganizationName string `json:"organization_name"`
		Email            string `json:"email"` // v2.7.1 #214: required for new signups
	}
	if err := decodeJSON(r, &body); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_body", err.Error())
		return
	}
	// v2.7.1 #214: email is REQUIRED for new signups (a v2.7.1 API policy enforced
	// here, not in the signup domain flow). Pre-v2.7.1 users keep NULL email.
	if strings.TrimSpace(body.Email) == "" {
		writeError(w, http.StatusBadRequest, "email_required", "email is required")
		return
	}
	// T237: the org slug is no longer collected from the client — SignupService
	// auto-generates a unique "org-<hex>" slug server-side (OrganizationSlug left
	// empty here triggers that path).
	form := identity.SignupForm{
		DisplayName:      body.DisplayName,
		PasscodePlain:    body.Passcode,
		OrganizationName: body.OrganizationName,
		Email:            body.Email,
	}
	res, err := d.SignupSvc.Execute(r.Context(), form)
	if err != nil {
		writeError(w, mapIdentityError(err), identityErrCode(err), err.Error())
		return
	}
	// Auto-signin: mint JWT and set cookie.
	if d.SigninSvc != nil {
		if token, err := d.SigninSvc.Execute(r.Context(), body.DisplayName, body.Passcode); err == nil {
			setSessionCookie(w, token.JWT)
		}
	}
	writeJSON(w, http.StatusCreated, map[string]any{
		"identity_id":     res.Identity.ID(),
		"organization_id": res.Organization.ID(),
		"display_name":    res.Identity.DisplayName(),
	})
}

// signinHandler handles POST /api/auth/signin.
func (s *Server) signinHandler(w http.ResponseWriter, r *http.Request) {
	d := hd(r)
	if d.SigninSvc == nil {
		writeError(w, http.StatusNotImplemented, "not_configured", "auth not configured")
		return
	}
	var body struct {
		DisplayName string `json:"display_name"`
		Passcode    string `json:"passcode"`
	}
	if err := decodeJSON(r, &body); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_body", err.Error())
		return
	}
	res, err := d.SigninSvc.Execute(r.Context(), body.DisplayName, body.Passcode)
	if err != nil {
		writeError(w, http.StatusUnauthorized, "auth_failed", "invalid display name or passcode")
		return
	}
	setSessionCookie(w, res.JWT)
	writeJSON(w, http.StatusOK, map[string]any{
		"identity_id": res.IdentityID,
	})
}

// signoutHandler handles POST /api/auth/signout.
func (s *Server) signoutHandler(w http.ResponseWriter, r *http.Request) {
	d := hd(r)
	// Best-effort: emit event for the current identity if we can verify.
	if d.AuthSvc != nil && d.SignoutSvc != nil {
		if cookie, err := r.Cookie(jwtCookieName); err == nil {
			if id, err := d.AuthSvc.AuthenticateToken(r.Context(), cookie.Value); err == nil {
				_ = d.SignoutSvc.Execute(r.Context(), id.ID(), "")
			}
		}
	}
	clearSessionCookie(w)
	w.WriteHeader(http.StatusNoContent)
}

// meHandler handles GET /api/auth/me.
func (s *Server) meHandler(w http.ResponseWriter, r *http.Request) {
	d := hd(r)
	if d.AuthSvc == nil {
		writeError(w, http.StatusNotImplemented, "not_configured", "auth not configured")
		return
	}
	cookie, err := r.Cookie(jwtCookieName)
	if err != nil {
		writeError(w, http.StatusUnauthorized, "unauthenticated", "no session")
		return
	}
	id, err := d.AuthSvc.AuthenticateToken(r.Context(), cookie.Value)
	if err != nil {
		writeError(w, http.StatusUnauthorized, "unauthenticated", "invalid or expired session")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"identity_id":  id.ID(),
		"display_name": id.DisplayName(),
		"kind":         string(id.Kind()),
	})
}

// changePasscodeHandler handles PATCH /api/auth/me/passcode.
func (s *Server) changePasscodeHandler(w http.ResponseWriter, r *http.Request) {
	d := hd(r)
	if d.PasscodeChangeSvc == nil || d.AuthSvc == nil {
		writeError(w, http.StatusNotImplemented, "not_configured", "passcode change not configured")
		return
	}
	cookie, err := r.Cookie(jwtCookieName)
	if err != nil {
		writeError(w, http.StatusUnauthorized, "unauthenticated", "no session")
		return
	}
	id, err := d.AuthSvc.AuthenticateToken(r.Context(), cookie.Value)
	if err != nil {
		writeError(w, http.StatusUnauthorized, "unauthenticated", "invalid session")
		return
	}
	var body struct {
		CurrentPasscode string `json:"current_passcode"`
		NewPasscode     string `json:"new_passcode"`
	}
	if err := decodeJSON(r, &body); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_body", err.Error())
		return
	}
	if err := d.PasscodeChangeSvc.Change(r.Context(), id.ID(), body.CurrentPasscode, body.NewPasscode); err != nil {
		writeError(w, mapIdentityError(err), identityErrCode(err), err.Error())
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// authMiddleware wraps API routes with JWT cookie verification.
// Routes under /api/auth/* and /api/health are exempt.
//
// Production guarantee: when running through `runWebConsole`, AuthSvc must
// be set (startup error otherwise — see webconsole_wiring.go). When AuthSvc
// is nil here (legacy / test-only paths), the middleware passes through.
// The fail-closed behavior is enforced at startup, not per-request.
func authMiddleware(deps HandlerDeps) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if deps.AuthSvc == nil {
				next.ServeHTTP(w, r)
				return
			}
			path := r.URL.Path
			// v2.7 #145: the embedded SPA (/, /signin, /signup, /assets/*,
			// react-router deep links) is public — only /api/* is auth-gated.
			// Non-API paths pass through to the SPA catch-all so a fresh/unauth
			// visitor gets the register/login UI, not a JSON 401.
			if len(path) < 5 || path[:5] != "/api/" {
				next.ServeHTTP(w, r)
				return
			}
			// Exempt public API endpoints (health + version + auth/*) from the
			// cookie gate. /api/system/version is non-sensitive build identity.
			if path == "/api/health" || path == "/api/system/version" ||
				len(path) >= 10 && path[:10] == "/api/auth/" {
				next.ServeHTTP(w, r)
				return
			}
			cookie, err := r.Cookie(jwtCookieName)
			if err != nil {
				writeError(w, http.StatusUnauthorized, "unauthenticated", "no session cookie")
				return
			}
			id, err := deps.AuthSvc.AuthenticateToken(r.Context(), cookie.Value)
			if err != nil {
				writeError(w, http.StatusUnauthorized, "unauthenticated", "invalid or expired session")
				return
			}
			ctx := context.WithValue(r.Context(), currentIdentityKey{}, id)
			next.ServeHTTP(w, r.WithContext(ctx))
		})
	}
}

type currentIdentityKey struct{}

// CurrentIdentity retrieves the authenticated identity injected by authMiddleware.
// Returns nil if auth middleware was not applied or auth is unconfigured.
func CurrentIdentity(r *http.Request) *identity.Identity {
	v, _ := r.Context().Value(currentIdentityKey{}).(*identity.Identity)
	return v
}

func setSessionCookie(w http.ResponseWriter, jwtToken string) {
	http.SetCookie(w, &http.Cookie{
		Name:     jwtCookieName,
		Value:    jwtToken,
		Path:     "/",
		HttpOnly: true,
		Secure:   true, // browsers on localhost allow Secure over http; production requires https
		SameSite: http.SameSiteLaxMode,
		MaxAge:   int((7 * 24 * time.Hour).Seconds()),
	})
}

func clearSessionCookie(w http.ResponseWriter) {
	http.SetCookie(w, &http.Cookie{
		Name:     jwtCookieName,
		Value:    "",
		Path:     "/",
		HttpOnly: true,
		MaxAge:   -1,
	})
}

func mapIdentityError(err error) int {
	switch {
	case errors.Is(err, identity.ErrPasscodeInvalid),
		errors.Is(err, identity.ErrUnauthenticated):
		return http.StatusUnauthorized
	case errors.Is(err, identity.ErrIdentityDisplayNameTaken),
		errors.Is(err, identity.ErrOrganizationSlugTaken),
		errors.Is(err, identity.ErrIdentityEmailTaken),
		errors.Is(err, identity.ErrIdentityAlreadyExists):
		return http.StatusConflict
	case errors.Is(err, identity.ErrIdentityNotFound),
		errors.Is(err, identity.ErrOrganizationNotFound):
		return http.StatusNotFound
	default:
		return http.StatusBadRequest
	}
}

func identityErrCode(err error) string {
	switch {
	case errors.Is(err, identity.ErrPasscodeInvalid):
		return "auth_failed"
	case errors.Is(err, identity.ErrUnauthenticated):
		return "unauthenticated"
	case errors.Is(err, identity.ErrIdentityDisplayNameTaken):
		return "display_name_taken"
	case errors.Is(err, identity.ErrIdentityEmailTaken):
		return "email_taken"
	case errors.Is(err, identity.ErrOrganizationSlugTaken):
		return "slug_taken"
	case errors.Is(err, identity.ErrIdentityAlreadyExists):
		return "already_exists"
	case errors.Is(err, identity.ErrIdentityNotFound):
		return "not_found"
	case errors.Is(err, identity.ErrOrganizationNotFound):
		return "org_not_found"
	default:
		return "validation_error"
	}
}
