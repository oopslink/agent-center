// Package escalator implements the Observability BC UnknownEventEscalator:
// periodic scan of agent_adapter.unknown_event_seen events; threshold
// reached → emit observability.unknown_event_escalated for supervisor to
// pick up (plan-4 § 3.8 + 05-agent-adapters § 3.1 step 5).
package escalator

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"time"

	"github.com/oopslink/agent-center/internal/clock"
	"github.com/oopslink/agent-center/internal/observability"
)

const (
	// DefaultThreshold is the count-in-window above which we escalate.
	DefaultThreshold = 10
	// DefaultWindow is the look-back window.
	DefaultWindow = 24 * time.Hour
	// DefaultInterval is how often `Run` scans (when ticker-driven).
	DefaultInterval = 1 * time.Hour
)

// EventTypeUnknownSeen is the worker-daemon emitted source event.
const EventTypeUnknownSeen observability.EventType = "agent_adapter.unknown_event_seen"

// EventTypeEscalated is the Observability-emitted derived event.
const EventTypeEscalated observability.EventType = "observability.unknown_event_escalated"

// Config knobs.
type Config struct {
	Threshold int
	Window    time.Duration
	Interval  time.Duration
}

// Apply returns a Config with zero fields replaced by defaults.
func (c Config) Apply() Config {
	if c.Threshold <= 0 {
		c.Threshold = DefaultThreshold
	}
	if c.Window <= 0 {
		c.Window = DefaultWindow
	}
	if c.Interval <= 0 {
		c.Interval = DefaultInterval
	}
	return c
}

// Service is the UnknownEventEscalator. Scan is the unit-testable verb;
// Run wraps it with a ticker.
type Service struct {
	events observability.EventRepository
	sink   *observability.EventSink
	clk    clock.Clock
	cfg    Config

	mu          sync.Mutex
	escalations map[dedupKey]time.Time // (adapter_name, cli_type_field, day_bucket) → escalated_at
}

type dedupKey struct {
	adapterName    string
	cliTypeField   string
	dayBucket      string
}

// NewService wires the escalator. Defaults applied to cfg.
func NewService(events observability.EventRepository, sink *observability.EventSink, clk clock.Clock, cfg Config) *Service {
	if clk == nil {
		clk = clock.SystemClock{}
	}
	return &Service{
		events:      events,
		sink:        sink,
		clk:         clk,
		cfg:         cfg.Apply(),
		escalations: map[dedupKey]time.Time{},
	}
}

// ScanResult summarises one scan pass.
type ScanResult struct {
	Scanned   int
	Groups    int
	Triggered int
	Skipped   int
}

// Scan walks events in [now-window, now], groups by (adapter_name,
// cli_type_field), and emits observability.unknown_event_escalated for any
// group at or above the threshold whose dedup key has not fired in the
// past 24 h.
func (s *Service) Scan(ctx context.Context) (ScanResult, error) {
	if s.events == nil || s.sink == nil {
		return ScanResult{}, errors.New("escalator: missing deps")
	}
	now := s.clk.Now().UTC()
	since := now.Add(-s.cfg.Window)
	et := EventTypeUnknownSeen
	filter := observability.EventQueryFilter{
		EventType: &et,
		Since:     &since,
		Limit:     observability.MaxEventQueryLimit,
	}
	evs, err := s.events.Find(ctx, filter)
	if err != nil {
		return ScanResult{}, err
	}
	res := ScanResult{Scanned: len(evs)}
	// Group
	type gKey struct{ adapter, cli string }
	groups := map[gKey]int{}
	sampleRaw := map[gKey]any{}
	for _, e := range evs {
		p := e.Payload()
		adapter := strOf(p["adapter_name"])
		cli := strOf(p["cli_type_field"])
		if adapter == "" || cli == "" {
			continue
		}
		k := gKey{adapter, cli}
		groups[k]++
		if _, ok := sampleRaw[k]; !ok {
			sampleRaw[k] = p["sample_raw"]
		}
	}
	res.Groups = len(groups)
	// Emit
	for k, n := range groups {
		if n < s.cfg.Threshold {
			continue
		}
		key := dedupKey{adapterName: k.adapter, cliTypeField: k.cli, dayBucket: now.Format("2006-01-02")}
		if s.recentlyEscalated(key, now) {
			res.Skipped++
			continue
		}
		_, err := s.sink.Emit(ctx, observability.EmitCommand{
			EventType: EventTypeEscalated,
			Actor:     observability.Actor("system"),
			Payload: map[string]any{
				"adapter_name":    k.adapter,
				"cli_type_field":  k.cli,
				"count_in_window": n,
				"window_hours":    int(s.cfg.Window / time.Hour),
				"sample_raw":      sampleRaw[k],
				"reason":          "threshold_reached",
				"message":         fmt.Sprintf("adapter=%s cli_type=%s saw %d unknown events in %dh window (threshold=%d)", k.adapter, k.cli, n, int(s.cfg.Window/time.Hour), s.cfg.Threshold),
			},
		})
		if err != nil {
			return res, err
		}
		s.markEscalated(key, now)
		res.Triggered++
	}
	return res, nil
}

func (s *Service) recentlyEscalated(k dedupKey, now time.Time) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	t, ok := s.escalations[k]
	if !ok {
		return false
	}
	return now.Sub(t) < 24*time.Hour
}

func (s *Service) markEscalated(k dedupKey, now time.Time) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.escalations[k] = now
}

func strOf(v any) string {
	if v == nil {
		return ""
	}
	if s, ok := v.(string); ok {
		return s
	}
	return ""
}

// Run drives Scan on a ticker. Returns when ctx is cancelled. Errors from
// Scan are not silently dropped — they go to the supplied logger callback
// so an `agent-center server` operator can see them (§ 17).
func (s *Service) Run(ctx context.Context, onError func(error)) {
	t := time.NewTicker(s.cfg.Interval)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			if _, err := s.Scan(ctx); err != nil && onError != nil {
				onError(err)
			}
		}
	}
}
