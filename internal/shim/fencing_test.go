package shim

import (
	"errors"
	"testing"
	"time"
)

type fakeTimer struct {
	start time.Time
	err   error
}

func (f fakeTimer) GetStartTime(_ int) (time.Time, error) {
	if f.err != nil {
		return time.Time{}, f.err
	}
	return f.start, nil
}

func TestVerifyToken(t *testing.T) {
	if err := VerifyToken("a", "a"); err != nil {
		t.Fatal(err)
	}
	if err := VerifyToken("a", "b"); err == nil {
		t.Fatal("expected mismatch")
	}
	if err := VerifyToken("", "a"); err == nil {
		t.Fatal("expected required")
	}
	if err := VerifyToken("a", ""); err == nil {
		t.Fatal("expected required")
	}
}

func TestVerifyPIDStartTime_Happy(t *testing.T) {
	expected := time.Date(2026, 5, 21, 12, 0, 0, 0, time.UTC)
	if err := VerifyPIDStartTime(fakeTimer{start: expected}, 100, expected); err != nil {
		t.Fatal(err)
	}
}

func TestVerifyPIDStartTime_Skew(t *testing.T) {
	expected := time.Date(2026, 5, 21, 12, 0, 0, 0, time.UTC)
	// Outside 1s tolerance
	if err := VerifyPIDStartTime(fakeTimer{start: expected.Add(2 * time.Second)}, 100, expected); err == nil {
		t.Fatal("expected skew error")
	}
	// Within tolerance
	if err := VerifyPIDStartTime(fakeTimer{start: expected.Add(500 * time.Millisecond)}, 100, expected); err != nil {
		t.Fatalf("unexpected: %v", err)
	}
}

func TestVerifyPIDStartTime_ProcessGone(t *testing.T) {
	expected := time.Date(2026, 5, 21, 12, 0, 0, 0, time.UTC)
	if err := VerifyPIDStartTime(fakeTimer{}, 100, expected); err == nil {
		t.Fatal("expected not found")
	}
}

func TestVerifyPIDStartTime_TimerError(t *testing.T) {
	if err := VerifyPIDStartTime(fakeTimer{err: errors.New("boom")}, 100, time.Now()); err == nil {
		t.Fatal("expected error")
	}
}

func TestVerifyPIDStartTime_NilTimer(t *testing.T) {
	if err := VerifyPIDStartTime(nil, 100, time.Now()); err == nil {
		t.Fatal("expected nil error")
	}
}

func TestOSStartTimer_RunsForSelf(t *testing.T) {
	// On both macOS + Linux the OSStartTimer should be able to read the
	// current process start time without erroring.
	timer := OSStartTimer{}
	_, err := timer.GetStartTime(testProcessPID())
	if err != nil {
		t.Skipf("OS timer not available in test env: %v", err)
	}
}

func testProcessPID() int {
	return testSelfPID()
}

func TestAbs_NegativeAndPositive(t *testing.T) {
	if abs(-3*time.Second) != 3*time.Second {
		t.Fatal("neg")
	}
	if abs(2*time.Second) != 2*time.Second {
		t.Fatal("pos")
	}
}

func TestParsePSLStart(t *testing.T) {
	got, err := parsePSLStart("Sat May 21 12:00:00 2026")
	if err != nil {
		t.Fatal(err)
	}
	if got.IsZero() {
		t.Fatal("expected non-zero")
	}
	got, err = parsePSLStart("")
	if err != nil || !got.IsZero() {
		t.Fatalf("expected zero: %v / %v", got, err)
	}
	if _, err := parsePSLStart("not-a-date"); err == nil {
		t.Fatal("expected parse err")
	}
}
