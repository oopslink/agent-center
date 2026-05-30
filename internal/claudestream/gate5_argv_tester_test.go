package claudestream

// TESTER-OWNED GATE artifact (v2.7 acceptance, integration mechanism layer).
// GATE-5: argv static regression assertion + session-id UUID/stability, per
// §A.0 of docs/plans/v2.7-acceptance-plan.md. Assertions are tester-defined
// (independent); this exercises the production argv builder to capture evidence.
// Run: `go test ./internal/claudestream/ -run GATE5 -v`.

import (
	"regexp"
	"strings"
	"testing"
)

var gateUUIDRe = regexp.MustCompile(`^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$`)

func gateHas(ss []string, x string) bool {
	for _, s := range ss {
		if s == x {
			return true
		}
	}
	return false
}

func gateHasPair(ss []string, flag, want string) bool {
	for i := 0; i+1 < len(ss); i++ {
		if ss[i] == flag && ss[i+1] == want {
			return true
		}
	}
	return false
}

func TestGATE5_ArgvStaticAssertion(t *testing.T) {
	const agentID = "01KSVNCZEXAMPLEAGENTID0001"
	argv, err := BuildStreamingArgv(agentID, "claude", "/tmp/agents/01KS/mcp-config.json", 0, nil)
	if err != nil {
		t.Fatalf("BuildStreamingArgv error: %v", err)
	}
	t.Logf("GATE-5 actual argv: %v", argv)

	if len(argv) == 0 || argv[0] != "claude" {
		t.Errorf("FAIL: argv[0] should be binary 'claude', got %q", argv[0])
	}
	args := argv[1:]

	if !gateHas(args, "--print") {
		t.Errorf("FAIL: missing --print")
	}
	if !gateHasPair(args, "--input-format", "stream-json") {
		t.Errorf("FAIL: missing --input-format stream-json")
	}
	if !gateHasPair(args, "--output-format", "stream-json") {
		t.Errorf("FAIL: missing --output-format stream-json")
	}
	if !gateHas(args, "--verbose") {
		t.Errorf("FAIL: missing --verbose")
	}
	if gateHas(args, "-p") {
		t.Errorf("FAIL: -p must be dropped (long-lived, not one-shot)")
	}
	if gateHas(args, longLivedSentinelPrompt) {
		t.Errorf("FAIL: sentinel positional prompt %q leaked into argv", longLivedSentinelPrompt)
	}
	var sid string
	for i := 0; i+1 < len(args); i++ {
		if args[i] == "--session-id" {
			sid = args[i+1]
		}
	}
	if sid == "" {
		t.Errorf("FAIL: missing --session-id")
	} else if !gateUUIDRe.MatchString(sid) {
		t.Errorf("FAIL: --session-id %q is not a valid UUID format", sid)
	} else {
		t.Logf("GATE-5 session-id: %s (valid UUID)", sid)
	}
	if !gateHasPair(args, "--mcp-config", "/tmp/agents/01KS/mcp-config.json") {
		t.Errorf("FAIL: missing --mcp-config <path> when path supplied")
	}
	// Positional-arg guard (catches a stray positional prompt = the sentinel
	// leak). Skip any element that is the VALUE of a known value-taking flag —
	// incl. --setting-sources's empty value "" (A-isolation flag, @oopslink
	// 8de95e70): the empty string is a legal flag value, not a positional.
	valueFlag := map[string]bool{
		"--input-format": true, "--output-format": true, "--session-id": true,
		"--mcp-config": true, "--model": true, "--setting-sources": true,
	}
	for i, a := range args {
		if i > 0 && valueFlag[args[i-1]] {
			continue // a is the value of a known value-flag (incl. "" after --setting-sources)
		}
		if !strings.HasPrefix(a, "-") {
			t.Errorf("FAIL: unexpected positional arg in argv: %q", a)
		}
	}
}

func TestGATE5_SessionUUID_StableAndDistinct(t *testing.T) {
	const a1 = "01KSVNCZEXAMPLEAGENTID0001"
	const a2 = "01KSVNCZEXAMPLEAGENTID0002"

	if SessionUUID(a1, 0) != SessionUUID(a1, 0) {
		t.Errorf("FAIL: SessionUUID not deterministic for same (agent,epoch)")
	}
	for _, id := range []string{SessionUUID(a1, 0), SessionUUID(a1, 1), SessionUUID(a2, 0)} {
		if !gateUUIDRe.MatchString(id) {
			t.Errorf("FAIL: %q not a valid UUID", id)
		}
	}
	if SessionUUID(a1, 0) == SessionUUID(a1, 1) {
		t.Errorf("FAIL: epoch bump did not change session-id (reset would not be clean-slate)")
	}
	if SessionUUID(a1, 0) == SessionUUID(a2, 0) {
		t.Errorf("FAIL: distinct agents produced same session-id")
	}
	t.Logf("GATE-5 stability: a1/e0=%s a1/e1=%s a2/e0=%s", SessionUUID(a1, 0), SessionUUID(a1, 1), SessionUUID(a2, 0))
}
