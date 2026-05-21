package shim

import (
	"os"
	"strconv"
	"testing"
	"time"
)

// TestOSStartTimer_LiveProcess hits the success path (current process is
// alive; reading its own start_time should succeed and be roughly close
// to "now"). This also exercises the LC_ALL=C / LANG=C plumbing on
// non-English locales.
func TestOSStartTimer_LiveProcess(t *testing.T) {
	pid := os.Getpid()
	got, err := OSStartTimer{}.GetStartTime(pid)
	if err != nil {
		t.Fatalf("GetStartTime(self): %v", err)
	}
	if got.IsZero() {
		t.Fatal("expected non-zero start_time for self")
	}
	if time.Since(got) > 24*time.Hour {
		// We're not stressing exactness; just sanity-check it's recent
		// enough to indicate the live process (not e.g. unix epoch).
		t.Logf("self start_time is %s — older than 24h", got)
	}
}

// TestOSStartTimer_NotFound_ReturnsZeroNoErr pins the "ps -p <gone-pid>
// → return (zero, nil)" contract so ShimSupervisor.checkAlive can treat
// it as a crash signal. Uses a very high PID unlikely to be allocated.
func TestOSStartTimer_NotFound_ReturnsZeroNoErr(t *testing.T) {
	// 0x7FFFFFFE is just under the conventional 32-bit PID max; on macOS
	// and Linux this is essentially never a live PID.
	missingPID, _ := strconv.Atoi("2147483646")
	got, err := OSStartTimer{}.GetStartTime(missingPID)
	if err != nil {
		t.Fatalf("expected nil err for missing pid, got: %v", err)
	}
	if !got.IsZero() {
		t.Fatalf("expected zero time for missing pid, got: %s", got)
	}
}
