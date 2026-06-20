package api

import (
	"net/http"
	"time"

	"github.com/oopslink/agent-center/internal/cognition/wakeguard"
)

// secondsToDuration converts a whole-seconds count to a Duration.
func secondsToDuration(sec int) time.Duration { return time.Duration(sec) * time.Second }

// requireAuthed gates an endpoint behind a valid session. When auth is not
// configured (headless/test deps with a nil AuthSvc) it passes through, matching
// the degrade-open convention of the other handlers. authMiddleware installs the
// identity for every /api route, so CurrentIdentity is the fast path; the cookie
// check is the direct-call fallback. Returns false (and writes 401) when unauthed.
func (s *Server) requireAuthed(w http.ResponseWriter, r *http.Request, d HandlerDeps) bool {
	if d.AuthSvc == nil {
		return true
	}
	if CurrentIdentity(r) != nil {
		return true
	}
	cookie, err := r.Cookie(jwtCookieName)
	if err != nil {
		writeError(w, http.StatusUnauthorized, "unauthenticated", "no session")
		return false
	}
	if _, err := d.AuthSvc.AuthenticateToken(r.Context(), cookie.Value); err != nil {
		writeError(w, http.StatusUnauthorized, "unauthenticated", "invalid session")
		return false
	}
	return true
}

// wakeGuardrailBody is the GET/PUT JSON shape for the wake-guardrail config
// (design 04-wake-guardrail.md §3.5). All five thresholds are positive ints;
// cycle_window is expressed in seconds. This is the contract the I7-D3 Settings
// UI consumes.
type wakeGuardrailBody struct {
	MaxDepth         int `json:"max_depth"`
	CycleWindowSec   int `json:"cycle_window_sec"`
	CycleThreshold   int `json:"cycle_threshold"`
	RatePerMin       int `json:"rate_per_min"`
	ChainTokenBudget int `json:"chain_token_budget"`
}

func wakeGuardrailBodyFromConfig(c wakeguard.Config) wakeGuardrailBody {
	return wakeGuardrailBody{
		MaxDepth:         c.MaxDepth,
		CycleWindowSec:   int(c.CycleWindow.Seconds()),
		CycleThreshold:   c.CycleN,
		RatePerMin:       c.RatePerMin,
		ChainTokenBudget: c.TokenBudget,
	}
}

// getWakeGuardrailHandler returns the EFFECTIVE wake-guardrail config: stored
// overrides merged onto the conservative code defaults (a missing/blank key falls
// back to its default, so the guard is never reported as disabled).
func (s *Server) getWakeGuardrailHandler(w http.ResponseWriter, r *http.Request) {
	d := hd(r)
	if !s.requireAuthed(w, r, d) {
		return
	}
	stored := map[string]string{}
	if d.SettingsStore != nil {
		m, err := d.SettingsStore.GetByPrefix(r.Context(), "wake.")
		if err != nil {
			writeError(w, http.StatusInternalServerError, "settings_read_failed", err.Error())
			return
		}
		stored = m
	}
	writeJSON(w, http.StatusOK, wakeGuardrailBodyFromConfig(wakeguard.ConfigFromMap(stored)))
}

// putWakeGuardrailHandler validates and persists the wake-guardrail thresholds.
// All five must be > 0 (the §3.5 contract); a bad value → 400 and nothing is
// written. On success the persisted (effective) config is returned. Because the
// WakeGuard reads its config through the settings store on every evaluation, the
// new thresholds take effect immediately — no restart (T224 "参数可配生效").
func (s *Server) putWakeGuardrailHandler(w http.ResponseWriter, r *http.Request) {
	d := hd(r)
	if !s.requireAuthed(w, r, d) {
		return
	}
	if d.SettingsStore == nil {
		writeError(w, http.StatusNotImplemented, "not_configured", "settings store not configured")
		return
	}
	var body wakeGuardrailBody
	if err := decodeJSON(r, &body); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_json", err.Error())
		return
	}
	cfg := wakeguard.Config{
		MaxDepth:    body.MaxDepth,
		CycleWindow: secondsToDuration(body.CycleWindowSec),
		CycleN:      body.CycleThreshold,
		RatePerMin:  body.RatePerMin,
		TokenBudget: body.ChainTokenBudget,
	}
	if err := cfg.Validate(); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_config", err.Error())
		return
	}
	for k, v := range cfg.ToMap() {
		if err := d.SettingsStore.Set(r.Context(), k, v); err != nil {
			writeError(w, http.StatusInternalServerError, "settings_write_failed", err.Error())
			return
		}
	}
	writeJSON(w, http.StatusOK, wakeGuardrailBodyFromConfig(cfg))
}
