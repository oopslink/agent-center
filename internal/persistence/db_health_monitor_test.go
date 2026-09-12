package persistence

import (
	"errors"
	"testing"
	"time"
)

func TestDBHealthMonitorTriggersOnSQLiteInterruptStorm(t *testing.T) {
	now := time.Date(2026, 9, 12, 10, 0, 0, 0, time.UTC)
	var recoveries []DBHealthSnapshot
	m := NewDBHealthMonitor(DBHealthMonitorConfig{
		Window:             time.Minute,
		Cooldown:           time.Minute,
		InterruptThreshold: 3,
		Now:                func() time.Time { return now },
		Recover:            func(s DBHealthSnapshot) { recoveries = append(recoveries, s) },
	})

	m.RecordError("auth", errors.New("SQL logic error: interrupted (9)"))
	m.RecordError("auth", errors.New("SQL logic error: interrupted (9)"))
	if len(recoveries) != 0 {
		t.Fatalf("recoveries before threshold=%d, want 0", len(recoveries))
	}
	m.RecordError("auth", errors.New("SQL logic error: interrupted (9)"))
	if len(recoveries) != 1 {
		t.Fatalf("recoveries=%d, want 1", len(recoveries))
	}
	if recoveries[0].LastRecoveryReason != string(DBHealthSQLiteInterrupt) {
		t.Fatalf("reason=%q, want %q", recoveries[0].LastRecoveryReason, DBHealthSQLiteInterrupt)
	}
}

func TestDBHealthMonitorCooldownSuppressesRepeatedRecovery(t *testing.T) {
	now := time.Date(2026, 9, 12, 10, 0, 0, 0, time.UTC)
	recoveries := 0
	m := NewDBHealthMonitor(DBHealthMonitorConfig{
		Window:             time.Minute,
		Cooldown:           time.Minute,
		InterruptThreshold: 2,
		Now:                func() time.Time { return now },
		Recover:            func(DBHealthSnapshot) { recoveries++ },
	})

	m.RecordError("auth", errors.New("interrupted (9)"))
	m.RecordError("auth", errors.New("interrupted (9)"))
	m.RecordError("auth", errors.New("interrupted (9)"))
	if recoveries != 1 {
		t.Fatalf("recoveries during cooldown=%d, want 1", recoveries)
	}

	now = now.Add(time.Minute + time.Second)
	m.RecordError("auth", errors.New("interrupted (9)"))
	m.RecordError("auth", errors.New("interrupted (9)"))
	if recoveries != 2 {
		t.Fatalf("recoveries after cooldown=%d, want 2", recoveries)
	}
}

func TestDBHealthMonitorTriggersOnAuthUnavailableStorm(t *testing.T) {
	now := time.Date(2026, 9, 12, 10, 0, 0, 0, time.UTC)
	var got DBHealthSnapshot
	m := NewDBHealthMonitor(DBHealthMonitorConfig{
		Window:                   time.Minute,
		Cooldown:                 time.Minute,
		AuthUnavailableThreshold: 2,
		Now:                      func() time.Time { return now },
		Recover:                  func(s DBHealthSnapshot) { got = s },
	})

	m.RecordAuthUnavailable("web.auth")
	m.RecordAuthUnavailable("web.auth")
	if got.LastRecoveryReason != string(DBHealthAuthUnavailable) {
		t.Fatalf("reason=%q, want %q", got.LastRecoveryReason, DBHealthAuthUnavailable)
	}
	if got.AuthUnavailableCount != 2 {
		t.Fatalf("auth count=%d, want 2", got.AuthUnavailableCount)
	}
}

func TestDBHealthMonitorIgnoresUnknownErrors(t *testing.T) {
	recoveries := 0
	m := NewDBHealthMonitor(DBHealthMonitorConfig{
		InterruptThreshold: 1,
		BusyThreshold:      1,
		Recover:            func(DBHealthSnapshot) { recoveries++ },
	})

	m.RecordError("business", errors.New("validation failed"))
	if recoveries != 0 {
		t.Fatalf("recoveries=%d, want 0", recoveries)
	}
}

func TestIsSQLiteBusyMatchesFlattenedDriverMessages(t *testing.T) {
	for _, err := range []error{
		errors.New("database is locked"),
		errors.New("SQLITE_BUSY"),
		errors.New("SQLITE_BUSY_SNAPSHOT"),
	} {
		if !IsSQLiteBusy(err) {
			t.Fatalf("IsSQLiteBusy(%q)=false, want true", err)
		}
	}
}
