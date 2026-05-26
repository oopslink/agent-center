// ratelimit.go — per-token request rate limit for the admin endpoint
// (v2.3-7c, task #27). Strictly an in-memory token bucket; survives
// process restarts only as much as token storage does (i.e. doesn't —
// buckets reset on boot, intentionally, since rate limits are best-
// effort defense against token-theft burst, not durable accounting).
//
// Defaults (frozen by @oopslink decision 2026-05-26 v2.3 security
// matrix): burst=20, refill=60 req/min (≈ 1/sec). Tuned for normal
// worker daemon poll cadence (~1 req/sec) + occasional CLI burst
// (a 10-item list query expands to a few requests) with headroom.
package api

import (
	"fmt"
	"net/http"
	"sync"
	"time"

	"github.com/oopslink/agent-center/internal/admintoken"
)

// RateLimitDefaults captures the production defaults. Tuned permissive
// enough that the standard worker daemon poll cadence + interactive
// CLI scripts / Web Console fan-out + e2e/smoke pipelines all fit
// comfortably:
//
//   - Burst 200 — Web Console loads ~10 endpoints in parallel; smoke
//     pipeline issues a quick burst of ~20-30 CLI calls; safe headroom.
//   - Refill 10/sec — sustains worker daemon poll at 1/sec while still
//     leaving room for CLI scripts that burst.
//
// These limits are per-token. v0 model: the protection is "an
// attacker who stole one token can't make millions of calls before
// admin notices"; not "fine-grained throttling per endpoint". Tighter
// per-endpoint limits would belong to a future ST.
var RateLimitDefaults = RateLimitConfig{
	Burst:      200,
	RefillRate: 10.0,
	IdleGC:     10 * time.Minute,
}

// RateLimitConfig parameterises the per-token bucket.
type RateLimitConfig struct {
	// Burst is the maximum bucket capacity (= max requests in a sudden
	// spike before throttling kicks in).
	Burst int
	// RefillRate is tokens added to the bucket per second. 1.0 == 60
	// req/min sustained.
	RefillRate float64
	// IdleGC removes buckets whose tokens have been idle longer than
	// this; bounds memory under bot-token churn.
	IdleGC time.Duration
}

// RateLimitSink is the narrow contract used to emit
// `admin.rate_limit_hit` observability events. Decoupled from the
// concrete EventSink so tests can stub.
type RateLimitSink interface {
	EmitRateLimitHit(tokenID admintoken.TokenID, clientIP, method, path string)
}

// noopRateLimitSink is the safe default when middleware is wired
// without an observability sink (tests, embedded harness).
type noopRateLimitSink struct{}

func (noopRateLimitSink) EmitRateLimitHit(admintoken.TokenID, string, string, string) {}

// RateLimitMiddleware wraps the admin handler chain BELOW
// AuthMiddleware (it requires AuthFromContext to identify the bucket
// key). Returns 429 with Retry-After: 1 when the bucket is empty.
//
// Configure with explicit cfg; passing zero-value cfg falls back to
// RateLimitDefaults.
func RateLimitMiddleware(cfg RateLimitConfig, sink RateLimitSink) func(http.Handler) http.Handler {
	if cfg.Burst <= 0 {
		cfg = RateLimitDefaults
	}
	if cfg.RefillRate <= 0 {
		cfg.RefillRate = RateLimitDefaults.RefillRate
	}
	if cfg.IdleGC <= 0 {
		cfg.IdleGC = RateLimitDefaults.IdleGC
	}
	if sink == nil {
		sink = noopRateLimitSink{}
	}
	rl := &rateLimiter{
		cfg:     cfg,
		buckets: map[admintoken.TokenID]*tokenBucket{},
		now:     time.Now,
	}
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			// Skip rate limit for the same public paths as auth — the
			// health endpoint must always answer for ops probes.
			if isPublicPath(r) {
				next.ServeHTTP(w, r)
				return
			}
			auth, ok := AuthFromContext(r.Context())
			if !ok {
				// Should not happen: this middleware runs AFTER auth.
				// Fail-closed: surface 503 rather than silently passing.
				writeError(w, http.StatusServiceUnavailable,
					"ratelimit_no_auth",
					"rate limit middleware reached without auth context")
				return
			}
			if !rl.allow(auth.TokenID) {
				sink.EmitRateLimitHit(auth.TokenID, auth.ClientIP, r.Method, r.URL.Path)
				w.Header().Set("Retry-After", "1")
				writeError(w, http.StatusTooManyRequests,
					"rate_limited",
					"per-token rate limit exceeded; back off and retry")
				return
			}
			next.ServeHTTP(w, r)
		})
	}
}

// --- internals -------------------------------------------------------

// tokenBucket is a classic float-token bucket. Not threadsafe by
// itself — caller (rateLimiter) holds a per-bucket mutex.
type tokenBucket struct {
	mu       sync.Mutex
	tokens   float64
	lastFill time.Time
}

func (b *tokenBucket) take(now time.Time, cfg RateLimitConfig) bool {
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.lastFill.IsZero() {
		// First request — start full.
		b.tokens = float64(cfg.Burst)
		b.lastFill = now
	}
	// Refill since last touch.
	elapsed := now.Sub(b.lastFill).Seconds()
	if elapsed > 0 {
		b.tokens += elapsed * cfg.RefillRate
		if b.tokens > float64(cfg.Burst) {
			b.tokens = float64(cfg.Burst)
		}
		b.lastFill = now
	}
	if b.tokens < 1.0 {
		return false
	}
	b.tokens -= 1.0
	return true
}

// rateLimiter holds per-token-id buckets. Locking strategy: top-level
// mutex guards the buckets map (rare write); each bucket has its own
// mutex for take() (hot path).
type rateLimiter struct {
	cfg     RateLimitConfig
	mu      sync.Mutex
	buckets map[admintoken.TokenID]*tokenBucket
	now     func() time.Time // injectable for tests
}

func (r *rateLimiter) allow(id admintoken.TokenID) bool {
	r.mu.Lock()
	b, ok := r.buckets[id]
	if !ok {
		b = &tokenBucket{}
		r.buckets[id] = b
	}
	r.mu.Unlock()
	return b.take(r.now(), r.cfg)
}

// (no GC implemented in v0; under realistic single-tenant deployment
// the per-token map is tiny. v3 deployment-as-product theme can add
// periodic GC if token churn grows.)

// writeRateLimitDebug is a debug helper for tests; production code
// uses writeError directly.
func writeRateLimitDebug(w http.ResponseWriter, msg string) {
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	w.WriteHeader(http.StatusTooManyRequests)
	_, _ = w.Write([]byte(fmt.Sprintf("rate-limit: %s", msg)))
}
