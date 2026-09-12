package persistence

import (
	"sync"
	"time"
)

// DBHealthEventKind is the normalized class of a database health signal.
type DBHealthEventKind string

const (
	DBHealthSQLiteInterrupt         DBHealthEventKind = "sqlite_interrupt"
	DBHealthSQLiteBusy              DBHealthEventKind = "sqlite_busy"
	DBHealthAuthUnavailable         DBHealthEventKind = "auth_unavailable"
	defaultDBHealthWindow                             = 30 * time.Second
	defaultDBHealthCooldown                           = 2 * time.Minute
	defaultInterruptThreshold                         = 3
	defaultBusyThreshold                              = 8
	defaultAuthUnavailableThreshold                   = 3
)

// DBHealthSnapshot is safe to expose on diagnostic endpoints.
type DBHealthSnapshot struct {
	Status                   string    `json:"status"`
	Window                   string    `json:"window"`
	Cooldown                 string    `json:"cooldown"`
	SQLiteInterruptCount     int       `json:"sqlite_interrupt_count"`
	SQLiteBusyCount          int       `json:"sqlite_busy_count"`
	AuthUnavailableCount     int       `json:"auth_unavailable_count"`
	LastEventAt              time.Time `json:"last_event_at,omitempty"`
	LastRecoveryAt           time.Time `json:"last_recovery_at,omitempty"`
	LastRecoveryReason       string    `json:"last_recovery_reason,omitempty"`
	RecoverySuppressed       bool      `json:"recovery_suppressed"`
	InterruptThreshold       int       `json:"interrupt_threshold"`
	BusyThreshold            int       `json:"busy_threshold"`
	AuthUnavailableThreshold int       `json:"auth_unavailable_threshold"`
}

// DBHealthMonitor observes DB-adjacent failures and invokes Recover when an
// error storm crosses a bounded threshold. It does not own the DB handle; server
// mode uses it to trigger a controlled process restart so launchd reopens SQLite.
type DBHealthMonitor struct {
	mu sync.Mutex

	window   time.Duration
	cooldown time.Duration
	now      func() time.Time
	recover  func(DBHealthSnapshot)

	interruptThreshold       int
	busyThreshold            int
	authUnavailableThreshold int

	events             []dbHealthEvent
	lastRecoveryAt     time.Time
	lastRecoveryReason string
}

type dbHealthEvent struct {
	kind   DBHealthEventKind
	source string
	at     time.Time
}

// DBHealthMonitorConfig configures NewDBHealthMonitor. Zero values use
// production defaults.
type DBHealthMonitorConfig struct {
	Window                   time.Duration
	Cooldown                 time.Duration
	InterruptThreshold       int
	BusyThreshold            int
	AuthUnavailableThreshold int
	Now                      func() time.Time
	Recover                  func(DBHealthSnapshot)
}

func NewDBHealthMonitor(cfg DBHealthMonitorConfig) *DBHealthMonitor {
	m := &DBHealthMonitor{
		window:                   cfg.Window,
		cooldown:                 cfg.Cooldown,
		now:                      cfg.Now,
		recover:                  cfg.Recover,
		interruptThreshold:       cfg.InterruptThreshold,
		busyThreshold:            cfg.BusyThreshold,
		authUnavailableThreshold: cfg.AuthUnavailableThreshold,
	}
	if m.window <= 0 {
		m.window = defaultDBHealthWindow
	}
	if m.cooldown <= 0 {
		m.cooldown = defaultDBHealthCooldown
	}
	if m.now == nil {
		m.now = time.Now
	}
	if m.interruptThreshold <= 0 {
		m.interruptThreshold = defaultInterruptThreshold
	}
	if m.busyThreshold <= 0 {
		m.busyThreshold = defaultBusyThreshold
	}
	if m.authUnavailableThreshold <= 0 {
		m.authUnavailableThreshold = defaultAuthUnavailableThreshold
	}
	return m
}

// RecordError records err when it is a SQLite health signal. Unknown errors are
// ignored so ordinary business failures do not affect recovery.
func (m *DBHealthMonitor) RecordError(source string, err error) {
	if m == nil || err == nil {
		return
	}
	switch {
	case IsSQLiteInterrupt(err):
		m.record(DBHealthSQLiteInterrupt, source)
	case IsSQLiteBusy(err):
		m.record(DBHealthSQLiteBusy, source)
	}
}

func (m *DBHealthMonitor) RecordAuthUnavailable(source string) {
	if m == nil {
		return
	}
	m.record(DBHealthAuthUnavailable, source)
}

func (m *DBHealthMonitor) Snapshot() DBHealthSnapshot {
	if m == nil {
		return DBHealthSnapshot{Status: "disabled"}
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	now := m.now()
	m.pruneLocked(now)
	return m.snapshotLocked(now, "")
}

func (m *DBHealthMonitor) record(kind DBHealthEventKind, source string) {
	now := m.now()
	var recovery func(DBHealthSnapshot)
	var snap DBHealthSnapshot
	m.mu.Lock()
	m.pruneLocked(now)
	m.events = append(m.events, dbHealthEvent{kind: kind, source: source, at: now})
	reason := m.recoveryReasonLocked()
	if reason != "" && (m.lastRecoveryAt.IsZero() || now.Sub(m.lastRecoveryAt) >= m.cooldown) {
		m.lastRecoveryAt = now
		m.lastRecoveryReason = reason
		snap = m.snapshotLocked(now, reason)
		snap.RecoverySuppressed = false
		recovery = m.recover
	}
	m.mu.Unlock()
	if recovery != nil {
		recovery(snap)
	}
}

func (m *DBHealthMonitor) pruneLocked(now time.Time) {
	cutoff := now.Add(-m.window)
	keep := 0
	for _, ev := range m.events {
		if ev.at.After(cutoff) || ev.at.Equal(cutoff) {
			m.events[keep] = ev
			keep++
		}
	}
	for i := keep; i < len(m.events); i++ {
		m.events[i] = dbHealthEvent{}
	}
	m.events = m.events[:keep]
}

func (m *DBHealthMonitor) recoveryReasonLocked() string {
	interrupts, busy, authUnavailable, _ := m.countsLocked()
	switch {
	case interrupts >= m.interruptThreshold:
		return string(DBHealthSQLiteInterrupt)
	case authUnavailable >= m.authUnavailableThreshold:
		return string(DBHealthAuthUnavailable)
	case busy >= m.busyThreshold:
		return string(DBHealthSQLiteBusy)
	default:
		return ""
	}
}

func (m *DBHealthMonitor) snapshotLocked(now time.Time, reason string) DBHealthSnapshot {
	interrupts, busy, authUnavailable, lastEventAt := m.countsLocked()
	if reason == "" {
		reason = m.recoveryReasonLocked()
	}
	status := "ok"
	if reason != "" {
		status = "recovering"
	}
	suppressed := false
	if reason != "" && !m.lastRecoveryAt.IsZero() && now.Sub(m.lastRecoveryAt) < m.cooldown {
		suppressed = true
	}
	return DBHealthSnapshot{
		Status:                   status,
		Window:                   m.window.String(),
		Cooldown:                 m.cooldown.String(),
		SQLiteInterruptCount:     interrupts,
		SQLiteBusyCount:          busy,
		AuthUnavailableCount:     authUnavailable,
		LastEventAt:              lastEventAt,
		LastRecoveryAt:           m.lastRecoveryAt,
		LastRecoveryReason:       m.lastRecoveryReason,
		RecoverySuppressed:       suppressed,
		InterruptThreshold:       m.interruptThreshold,
		BusyThreshold:            m.busyThreshold,
		AuthUnavailableThreshold: m.authUnavailableThreshold,
	}
}

func (m *DBHealthMonitor) countsLocked() (interrupts, busy, authUnavailable int, lastEventAt time.Time) {
	for _, ev := range m.events {
		if ev.at.After(lastEventAt) {
			lastEventAt = ev.at
		}
		switch ev.kind {
		case DBHealthSQLiteInterrupt:
			interrupts++
		case DBHealthSQLiteBusy:
			busy++
		case DBHealthAuthUnavailable:
			authUnavailable++
		}
	}
	return interrupts, busy, authUnavailable, lastEventAt
}
