package api

import (
	"context"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

	"github.com/oopslink/agent-center/internal/admintoken"
)

// v2.3-7c tests for per-token rate limit middleware.

type recordingRateLimitSink struct {
	hits atomic.Int32
}

func (s *recordingRateLimitSink) EmitRateLimitHit(admintoken.TokenID, string, string, string) {
	s.hits.Add(1)
}

// withRateAuth puts an AuthContext on the request so RateLimitMiddleware
// can identify the bucket. Tests don't go through AuthMiddleware here.
// Named to avoid colliding with blob_test.go's withAuth.
func withRateAuth(r *http.Request, tokID admintoken.TokenID) *http.Request {
	ctx := context.WithValue(r.Context(), authKey{}, AuthContext{
		TokenID:  tokID,
		ClientIP: "1.2.3.4",
	})
	return r.WithContext(ctx)
}

func TestRateLimit_AllowsBurst(t *testing.T) {
	called := atomic.Int32{}
	next := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		called.Add(1)
		w.WriteHeader(http.StatusOK)
	})
	cfg := RateLimitConfig{Burst: 5, RefillRate: 0.001, IdleGC: time.Minute}
	mw := RateLimitMiddleware(cfg, nil)(next)

	for i := 0; i < 5; i++ {
		w := httptest.NewRecorder()
		r := withRateAuth(httptest.NewRequest("GET", "/admin/foo", nil), "tok-1")
		mw.ServeHTTP(w, r)
		if w.Code != http.StatusOK {
			t.Fatalf("burst req %d: got %d", i, w.Code)
		}
	}
	if called.Load() != 5 {
		t.Fatalf("called=%d", called.Load())
	}
}

func TestRateLimit_RejectsOverBurst(t *testing.T) {
	next := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	})
	sink := &recordingRateLimitSink{}
	cfg := RateLimitConfig{Burst: 3, RefillRate: 0.001, IdleGC: time.Minute}
	mw := RateLimitMiddleware(cfg, sink)(next)

	for i := 0; i < 3; i++ {
		w := httptest.NewRecorder()
		r := withRateAuth(httptest.NewRequest("GET", "/admin/foo", nil), "tok-burst")
		mw.ServeHTTP(w, r)
		if w.Code != http.StatusOK {
			t.Fatalf("burst req %d: got %d", i, w.Code)
		}
	}
	// 4th request: bucket empty (and refill rate too slow to matter).
	w := httptest.NewRecorder()
	r := withRateAuth(httptest.NewRequest("GET", "/admin/foo", nil), "tok-burst")
	mw.ServeHTTP(w, r)
	if w.Code != http.StatusTooManyRequests {
		t.Fatalf("4th req: got %d, want 429", w.Code)
	}
	if w.Header().Get("Retry-After") != "1" {
		t.Errorf("Retry-After=%q", w.Header().Get("Retry-After"))
	}
	if sink.hits.Load() != 1 {
		t.Fatalf("sink hits=%d", sink.hits.Load())
	}
}

func TestRateLimit_PerTokenIsolation(t *testing.T) {
	next := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	})
	cfg := RateLimitConfig{Burst: 2, RefillRate: 0.001, IdleGC: time.Minute}
	mw := RateLimitMiddleware(cfg, nil)(next)

	// Token A exhausts its bucket.
	for i := 0; i < 2; i++ {
		w := httptest.NewRecorder()
		r := withRateAuth(httptest.NewRequest("GET", "/x", nil), "tok-A")
		mw.ServeHTTP(w, r)
		if w.Code != http.StatusOK {
			t.Fatalf("A req %d: got %d", i, w.Code)
		}
	}
	// Token B still has full bucket — must pass.
	w := httptest.NewRecorder()
	r := withRateAuth(httptest.NewRequest("GET", "/x", nil), "tok-B")
	mw.ServeHTTP(w, r)
	if w.Code != http.StatusOK {
		t.Fatalf("B first req: got %d, want 200", w.Code)
	}
	// Token A's next request rejected.
	w = httptest.NewRecorder()
	r = withRateAuth(httptest.NewRequest("GET", "/x", nil), "tok-A")
	mw.ServeHTTP(w, r)
	if w.Code != http.StatusTooManyRequests {
		t.Fatalf("A 3rd req: got %d, want 429", w.Code)
	}
}

func TestRateLimit_RefillsOverTime(t *testing.T) {
	next := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	})
	cfg := RateLimitConfig{Burst: 1, RefillRate: 100, IdleGC: time.Minute} // refills fast
	mw := RateLimitMiddleware(cfg, nil)(next)

	// First request: pass.
	w := httptest.NewRecorder()
	r := withRateAuth(httptest.NewRequest("GET", "/x", nil), "tok-refill")
	mw.ServeHTTP(w, r)
	if w.Code != http.StatusOK {
		t.Fatalf("1st: got %d", w.Code)
	}
	// Immediate 2nd: rejected (bucket empty).
	w = httptest.NewRecorder()
	r = withRateAuth(httptest.NewRequest("GET", "/x", nil), "tok-refill")
	mw.ServeHTTP(w, r)
	if w.Code != http.StatusTooManyRequests {
		t.Fatalf("2nd: got %d, want 429", w.Code)
	}
	// Wait for refill (100 tokens/sec → 10ms is plenty).
	time.Sleep(50 * time.Millisecond)
	w = httptest.NewRecorder()
	r = withRateAuth(httptest.NewRequest("GET", "/x", nil), "tok-refill")
	mw.ServeHTTP(w, r)
	if w.Code != http.StatusOK {
		t.Fatalf("after refill: got %d, want 200", w.Code)
	}
}

func TestRateLimit_HealthExempt(t *testing.T) {
	next := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	})
	cfg := RateLimitConfig{Burst: 1, RefillRate: 0.001, IdleGC: time.Minute}
	mw := RateLimitMiddleware(cfg, nil)(next)

	// Many /admin/health requests should all pass; no AuthContext set.
	for i := 0; i < 5; i++ {
		w := httptest.NewRecorder()
		r := httptest.NewRequest("GET", "/admin/health", nil)
		mw.ServeHTTP(w, r)
		if w.Code != http.StatusOK {
			t.Fatalf("health req %d: got %d", i, w.Code)
		}
	}
}

func TestRateLimit_NoAuthFails(t *testing.T) {
	next := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	})
	cfg := RateLimitConfig{Burst: 5, RefillRate: 1, IdleGC: time.Minute}
	mw := RateLimitMiddleware(cfg, nil)(next)
	// Non-public path, no auth context — middleware should fail-closed
	// (it runs AFTER AuthMiddleware in production, so no auth = misconfig).
	w := httptest.NewRecorder()
	r := httptest.NewRequest("GET", "/admin/foo", nil)
	mw.ServeHTTP(w, r)
	if w.Code != http.StatusServiceUnavailable {
		t.Fatalf("got %d, want 503", w.Code)
	}
}

func TestRateLimit_DefaultsFallback(t *testing.T) {
	// Zero-value cfg should fall back to RateLimitDefaults.
	next := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	})
	mw := RateLimitMiddleware(RateLimitConfig{}, nil)(next)
	w := httptest.NewRecorder()
	r := withRateAuth(httptest.NewRequest("GET", "/x", nil), "tok-d")
	mw.ServeHTTP(w, r)
	if w.Code != http.StatusOK {
		t.Fatalf("got %d, want 200", w.Code)
	}
}

func TestClientIPFromRequest(t *testing.T) {
	r := httptest.NewRequest("GET", "/x", nil)
	r.RemoteAddr = "192.0.2.10:54321"
	if got := clientIPFromRequest(r); got != "192.0.2.10" {
		t.Errorf("tcp: got %q", got)
	}
	r.RemoteAddr = "[::1]:9000"
	if got := clientIPFromRequest(r); got != "::1" {
		t.Errorf("v6: got %q", got)
	}
	r.RemoteAddr = "@"
	if got := clientIPFromRequest(r); got != "" {
		t.Errorf("unix: got %q", got)
	}
	r.RemoteAddr = ""
	if got := clientIPFromRequest(r); got != "" {
		t.Errorf("empty: got %q", got)
	}
}

// silence unused-helper warning if writeRateLimitDebug isn't called by tests
var _ = writeRateLimitDebug
