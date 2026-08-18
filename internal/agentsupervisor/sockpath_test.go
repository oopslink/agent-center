package agentsupervisor

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// v2.7 #178 (acceptance FINDING-E): the supervisor socket must stay under the
// macOS AF_UNIX sun_path limit (104 bytes). The old path lived under the deeply
// nested agent home and overflowed. ScopedSockPath must be short and —
// critically — independent of agentID/home length (it hashes the inputs), and
// must live under the OS temp dir, NOT any agent home.
func TestSockPath_ShortAndAgentIDLengthIndependent(t *testing.T) {
	short := ScopedSockPath("a", "/tmp/home-a")
	// A pathological agent id far longer than any real ULID.
	longID := strings.Repeat("01ARZ3NDEKTSV4RRFFQ69G5FAV-", 8)
	longHome := filepath.Join(os.TempDir(), strings.Repeat("deep-home-", 12), "agent")
	long := ScopedSockPath(longID, longHome)

	if len(short) != len(long) {
		t.Fatalf("sock path length must not depend on agentID: %d (%q) vs %d (%q)",
			len(short), short, len(long), long)
	}
	// Hard upper bound: well under the 104-byte sun_path limit, with margin.
	if len(long) >= 100 {
		t.Fatalf("sock path %q len=%d must be < 100 (macOS sun_path 104, margin)", long, len(long))
	}
	// Lives under the OS temp dir, never under an agent home.
	if !strings.HasPrefix(long, os.TempDir()) {
		t.Fatalf("sock path %q must live under TempDir %q", long, os.TempDir())
	}
	// Fixed short filename: "acsv-" + 12 hex + ".sock".
	base := filepath.Base(long)
	if !strings.HasPrefix(base, "acsv-") || !strings.HasSuffix(base, ".sock") ||
		len(base) != len("acsv-")+12+len(".sock") {
		t.Fatalf("unexpected sock filename %q", base)
	}
	// Deterministic: same scoped inputs → same path (daemon and supervisor agree).
	if ScopedSockPath("agent-x", "/tmp/home-x") != ScopedSockPath("agent-x", "/tmp/home-x") {
		t.Fatal("ScopedSockPath must be deterministic for a given agentID+home")
	}
	if ScopedSockPath("agent-x", "/tmp/home-x") == ScopedSockPath("agent-y", "/tmp/home-x") {
		t.Fatal("ScopedSockPath must differ for different agentIDs")
	}
	if ScopedSockPath("agent-x", "/tmp/home-x") == ScopedSockPath("agent-x", "/tmp/home-y") {
		t.Fatal("ScopedSockPath must differ for different homes")
	}
}
