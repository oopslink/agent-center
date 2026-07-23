package claudestream

import (
	"regexp"
	"strings"
	"testing"
)

func TestRewriteForStreamingInput(t *testing.T) {
	// Mirror what claudecode.Adapter.BuildCommand emits for a long-lived spawn:
	// --output-format stream-json --session-id <id> -p <sentinel-prompt>.
	in := []string{"--output-format", "stream-json", "--session-id", "agent-1", "-p", longLivedSentinelPrompt}
	out := rewriteForStreamingInput(in)
	joined := strings.Join(out, " ")

	// --print PRESENT as a flag (the -p was canonicalised to --print).
	if !contains(out, "--print") {
		t.Fatalf("--print not present: %v", out)
	}
	// The sentinel/positional prompt must be gone, and no bare -p left.
	for _, a := range out {
		if a == "-p" || a == longLivedSentinelPrompt {
			t.Fatalf("sentinel prompt / -p not stripped: %v", out)
		}
	}
	// The three validated stream flags all present.
	if !strings.Contains(joined, "--input-format stream-json") {
		t.Fatalf("missing --input-format stream-json: %v", out)
	}
	if !strings.Contains(joined, "--output-format stream-json") {
		t.Fatalf("missing --output-format stream-json: %v", out)
	}
	if !contains(out, "--verbose") {
		t.Fatalf("missing --verbose: %v", out)
	}
	// session-id preserved.
	if !strings.Contains(joined, "--session-id agent-1") {
		t.Fatalf("session-id dropped: %v", out)
	}
	// No duplicate flags introduced.
	if n := count(out, "--print"); n != 1 {
		t.Fatalf("--print appears %d times: %v", n, out)
	}
	if n := count(out, "--input-format"); n != 1 {
		t.Fatalf("--input-format appears %d times: %v", n, out)
	}
	if n := count(out, "--output-format"); n != 1 {
		t.Fatalf("--output-format appears %d times: %v", n, out)
	}
	if n := count(out, "--verbose"); n != 1 {
		t.Fatalf("--verbose appears %d times: %v", n, out)
	}
}

func contains(ss []string, want string) bool {
	for _, s := range ss {
		if s == want {
			return true
		}
	}
	return false
}

func count(ss []string, want string) int {
	n := 0
	for _, s := range ss {
		if s == want {
			n++
		}
	}
	return n
}

// TestSessionUUID validates the agent-id → claude --session-id derivation.
// claude REQUIRES a valid UUID (real claude 2.1.156 rejects a raw ULID), and
// AgentCenter mints agent ids as ULIDs, so the launcher must derive one.
func TestSessionUUID(t *testing.T) {
	// A realistic ULID agent id (what s.idgen.NewULID() produces).
	const ulid = "01J9ZK7QW8X2YB3C4D5E6F7G8H"
	got := SessionUUID(ulid, 0)

	// Must be a syntactically valid RFC 4122 UUID: 8-4-4-4-12 lowercase hex,
	// version nibble 5, variant high bits 10.
	re := regexp.MustCompile(`^[0-9a-f]{8}-[0-9a-f]{4}-5[0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}$`)
	if !re.MatchString(got) {
		t.Fatalf("SessionUUID(%q, 0) = %q — not a valid v5 UUID", ulid, got)
	}
	// Deterministic in (agentID, epoch): same agent + same epoch → same session id.
	// This is what lets re-attach AND mode-B crash-relaunch (same durable epoch)
	// resume the SAME claude session.
	if again := SessionUUID(ulid, 0); again != got {
		t.Fatalf("not deterministic: %q != %q", got, again)
	}
	// Distinct agents → distinct session ids (no "already in use" collisions).
	if other := SessionUUID("01J9ZK7QW8X2YB3C4D5E6F7G8J", 0); other == got {
		t.Fatalf("distinct agent ids collided on session id %q", got)
	}
	// epoch++ → a NEW session id (a clean-slate RESET): same agent, next epoch must
	// derive a DIFFERENT uuid, and it must still be a valid v5 UUID.
	reset := SessionUUID(ulid, 1)
	if reset == got {
		t.Fatalf("epoch bump did not change session id (reset would not get a clean slate): still %q", got)
	}
	if !re.MatchString(reset) {
		t.Fatalf("SessionUUID(%q, 1) = %q — not a valid v5 UUID", ulid, reset)
	}
	// And the bumped epoch is itself deterministic (relaunch at the same epoch
	// resumes the same reset session).
	if again := SessionUUID(ulid, 1); again != reset {
		t.Fatalf("epoch-1 not deterministic: %q != %q", reset, again)
	}
}

// TestBuildStreamingArgv proves the full argv pipeline produces the validated
// long-lived streaming flag set with the derived session-id + mcp-config, and
// rejects an empty agent id.
func TestBuildStreamingArgv(t *testing.T) {
	if _, _, err := BuildStreamingArgv("", "", "", 0, 0, "", nil, "", false); err == nil {
		t.Fatal("empty agent id must error")
	}

	argv, _, err := BuildStreamingArgv("01J9ZK7QW8X2YB3C4D5E6F7G8H", "/usr/local/bin/claude", "/home/agent/mcp.json", 0, 0, "", nil, "", false)
	if err != nil {
		t.Fatalf("BuildStreamingArgv: %v", err)
	}
	if len(argv) == 0 {
		t.Fatal("empty argv")
	}
	if argv[0] != "/usr/local/bin/claude" {
		t.Fatalf("argv[0] = %q, want claude binary", argv[0])
	}
	joined := strings.Join(argv, " ")
	want := []string{
		"--print",
		"--input-format stream-json",
		"--output-format stream-json",
		"--verbose",
		"--session-id " + SessionUUID("01J9ZK7QW8X2YB3C4D5E6F7G8H", 0),
		"--mcp-config /home/agent/mcp.json",
	}
	for _, w := range want {
		if !strings.Contains(joined, w) {
			t.Fatalf("argv missing %q: %v", w, argv)
		}
	}
	// The sentinel prompt must NOT survive into the argv.
	if strings.Contains(joined, longLivedSentinelPrompt) {
		t.Fatalf("sentinel prompt leaked into argv: %v", argv)
	}
}

// TestBuildStreamingArgv_DeferredToolDiscoverabilityGuidance pins T463 (issue
// d8c8c9b8 (b)): the persistent --append-system-prompt the agent actually
// receives must carry the "discoverability ≠ absence" hard rule, while issue
// lifecycle tools are named as core so an agent can follow owner-review nudges
// such as close_issue directly. Asserting on the real BuildStreamingArgv output
// (not the const directly) proves the text survives into the delivered prompt.
func TestBuildStreamingArgv_DeferredToolDiscoverabilityGuidance(t *testing.T) {
	argv, _, err := BuildStreamingArgv("01J9ZK7QW8X2YB3C4D5E6F7G8H", "claude", "/home/agent/mcp.json", 0, 0, "", nil, "", false)
	if err != nil {
		t.Fatalf("BuildStreamingArgv: %v", err)
	}
	// The system prompt rides --append-system-prompt; find its value.
	var sysPrompt string
	for i, a := range argv {
		if a == "--append-system-prompt" && i+1 < len(argv) {
			sysPrompt = argv[i+1]
			break
		}
	}
	if sysPrompt == "" {
		t.Fatalf("no --append-system-prompt in argv: %v", argv)
	}
	// The hard rule (search_tools before concluding a deferred tool is missing)
	// and the directly named issue lifecycle tools must both be present in the
	// delivered prompt.
	want := []string{
		"search_tools",
		"before you conclude that you lack a tool",
		"get_issue",
		"close_issue",
		"get_plan",
		"discoverability ≠ absence",
	}
	for _, w := range want {
		if !strings.Contains(sysPrompt, w) {
			t.Fatalf("delivered system prompt missing %q (T463 guidance)", w)
		}
	}
}

// TestSessionUUIDGen_Gen0ByteIdenticalToLegacy is PM's GATE-7 review checkpoint #3:
// SessionUUIDGen(id, epoch, 0) MUST be byte-for-byte identical to the pre-fix
// SessionUUID(id, epoch). A single-byte drift would silently re-derive EVERY
// existing agent's initial session-id → next interaction can't resume its own
// session (or unexpectedly forks) = a hidden migration trap. The equivalence is
// by-construction (gen==0 delegates to SessionUUID), and this test pins it so any
// future change that breaks the delegation goes red.
func TestSessionUUIDGen_Gen0ByteIdenticalToLegacy(t *testing.T) {
	cases := []struct {
		agentID string
		epoch   int
	}{
		{"01J9ZK7QW8X2YB3C4D5E6F7G8H", 0},
		{"01J9ZK7QW8X2YB3C4D5E6F7G8H", 1},
		{"01J9ZK7QW8X2YB3C4D5E6F7G8H", 42},
		{"another-agent-id", 0},
		{"another-agent-id", 7},
		{"", 0}, // derivation is total; gen0 must still match legacy
	}
	for _, c := range cases {
		legacy := SessionUUID(c.agentID, c.epoch)
		gen0 := SessionUUIDGen(c.agentID, c.epoch, 0)
		if gen0 != legacy {
			t.Fatalf("SessionUUIDGen(%q,%d,0)=%q != SessionUUID(%q,%d)=%q (gen0 must be byte-identical)",
				c.agentID, c.epoch, gen0, c.agentID, c.epoch, legacy)
		}
	}
}

// TestSessionUUIDGen_GenDistinctAndValid pins that generation > 0 yields a
// DIFFERENT (and still valid v5) uuid from generation 0 and from other generations
// — the property the Mode-B fork relies on (each relaunch lands on a fresh,
// never-colliding session-id).
func TestSessionUUIDGen_GenDistinctAndValid(t *testing.T) {
	const id, ep = "01J9ZK7QW8X2YB3C4D5E6F7G8H", 3
	seen := map[string]int{}
	for gen := 0; gen <= 4; gen++ {
		u := SessionUUIDGen(id, ep, gen)
		if len(u) != 36 || strings.Count(u, "-") != 4 {
			t.Fatalf("generation %d → %q is not a UUID", gen, u)
		}
		if prev, dup := seen[u]; dup {
			t.Fatalf("generation %d collided with generation %d (%q)", gen, prev, u)
		}
		seen[u] = gen
	}
}

// TestBuildStreamingArgv_ModeBFork pins the v2.7 GATE-7 Mode-B fork argv: a
// non-empty resumeFromSessionID adds `--resume <prev> --fork-session` and the
// --session-id is the next-generation id; an empty one adds neither (plain start).
func TestBuildStreamingArgv_ModeBFork(t *testing.T) {
	const id = "01J9ZK7QW8X2YB3C4D5E6F7G8H"
	prev := SessionUUIDGen(id, 0, 0)

	// Fork relaunch: generation 1, resume from the gen-0 id.
	forked, _, err := BuildStreamingArgv(id, "claude", "", 0, 1, prev, nil, "", false)
	if err != nil {
		t.Fatalf("fork argv: %v", err)
	}
	joined := strings.Join(forked, " ")
	if !strings.Contains(joined, "--session-id "+SessionUUIDGen(id, 0, 1)) {
		t.Fatalf("fork argv must use the gen-1 session-id: %v", forked)
	}
	if !strings.Contains(joined, "--resume "+prev) || !strings.Contains(joined, "--fork-session") {
		t.Fatalf("fork argv must `--resume <prev> --fork-session`: %v", forked)
	}

	// Plain start (no fork): no --resume / --fork-session.
	plain, _, err := BuildStreamingArgv(id, "claude", "", 0, 0, "", nil, "", false)
	if err != nil {
		t.Fatalf("plain argv: %v", err)
	}
	pj := strings.Join(plain, " ")
	if strings.Contains(pj, "--fork-session") || strings.Contains(pj, "--resume") {
		t.Fatalf("plain start must NOT fork/resume: %v", plain)
	}
}

// TestBuildStreamingArgv_FINDING3_IdleRelaunchFresh pins the FINDING-3 (#117 part B)
// argv contract for the Mode-B relaunch: the generation is ALWAYS bumped (fresh
// never-locked id, lock-avoidance), but `--resume <prev> --fork-session` is emitted
// ONLY when there is in-flight work (hadWork) — which BuildStreamingArgv expresses as
// a non-empty resumeFromSessionID.
//
//   - hadWork==true  → caller passes resumeFromSessionID=<prevGenId> → fresh gen-id
//     id + `--resume <prevGenId> --fork-session` (BYTE-IDENTICAL to the A-seg verified
//     mid-turn-resume).
//   - hadWork==false → caller passes resumeFromSessionID="" → ONLY `--session-id
//     <newGenId>` on the fresh gen-id, NO --resume / --fork-session → claude starts
//     fresh (no error_during_execution on a no-completed-turn session).
//
// Both cases use the SAME bumped generation; only the resume pair differs.
func TestBuildStreamingArgv_FINDING3_IdleRelaunchFresh(t *testing.T) {
	const id = "01J9ZK7QW8X2YB3C4D5E6F7G8H"
	prevGen := SessionUUIDGen(id, 0, 0) // the killed (gen-0) session-id
	newGen := SessionUUIDGen(id, 0, 1)  // the fresh bumped (gen-1) session-id

	// hadWork==true relaunch: fresh gen-1 id + --resume <prevGen> --fork-session.
	work, _, err := BuildStreamingArgv(id, "claude", "", 0, 1, prevGen, nil, "", false)
	if err != nil {
		t.Fatalf("with-work relaunch argv: %v", err)
	}
	wj := strings.Join(work, " ")
	if !strings.Contains(wj, "--session-id "+newGen) {
		t.Fatalf("with-work relaunch must spawn the fresh gen-1 id %q: %v", newGen, work)
	}
	if !strings.Contains(wj, "--resume "+prevGen) || !strings.Contains(wj, "--fork-session") {
		t.Fatalf("with-work relaunch MUST `--resume %s --fork-session` (A-seg unchanged): %v", prevGen, work)
	}

	// hadWork==false (idle) relaunch: fresh gen-1 id, NO --resume / --fork-session.
	idle, _, err := BuildStreamingArgv(id, "claude", "", 0, 1, "" /*resumeFromSessionID empty = idle*/, nil, "", false)
	if err != nil {
		t.Fatalf("idle relaunch argv: %v", err)
	}
	ij := strings.Join(idle, " ")
	if !strings.Contains(ij, "--session-id "+newGen) {
		t.Fatalf("idle relaunch must STILL spawn the fresh gen-1 id %q (lock-avoidance unchanged): %v", newGen, idle)
	}
	if strings.Contains(ij, "--resume") || strings.Contains(ij, "--fork-session") {
		t.Fatalf("idle relaunch must NOT --resume/--fork-session (avoids error_during_execution): %v", idle)
	}
}

// countArg counts exact-token occurrences in argv (drift-guard helper).
func countArg(argv []string, x string) int {
	n := 0
	for _, a := range argv {
		if a == x {
			n++
		}
	}
	return n
}

// TestBuildStreamingArgv_SecurityFlags pins the v2.7 ① security/launch flags into
// the streaming argv and is the DEV-side drift guard (the unit half of the
// "isolation hangs on a single flag" invariant — PM patch 1 / Tester §2.6): if a
// refactor drops or duplicates `--setting-sources user,project`, this goes red.
// v2.7 #182 (FINDING-G): the value is `user,project` (NOT "") — the empty value
// suppressed the keychain /login credential (loaded via the user source) → 403.
func TestBuildStreamingArgv_SecurityFlags(t *testing.T) {
	argv, _, err := BuildStreamingArgv("01J9ZK7QW8X2YB3C4D5E6F7G8H", "claude", "/home/agent/mcp.json", 0, 0, "", nil, "", false)
	if err != nil {
		t.Fatalf("BuildStreamingArgv: %v", err)
	}

	// #182: --setting-sources present EXACTLY once with value "user,project"
	// (user = keychain /login auth, project = the agent's own <workspace>/.claude).
	if c := countArg(argv, "--setting-sources"); c != 1 {
		t.Fatalf("--setting-sources count = %d, want exactly 1 (drift guard): %v", c, argv)
	}
	sawValue := false
	for i := 0; i+1 < len(argv); i++ {
		if argv[i] == "--setting-sources" {
			if argv[i+1] != "user,project" {
				t.Fatalf("--setting-sources value = %q, want \"user,project\" (#182): %v", argv[i+1], argv)
			}
			sawValue = true
		}
	}
	if !sawValue {
		t.Fatalf("--setting-sources has no following value element: %v", argv)
	}

	// Deterministic non-interactive permission group (PD ruling 09394dbd).
	for _, f := range []string{"--allow-dangerously-skip-permissions", "--dangerously-skip-permissions"} {
		if !hasArg(argv, f) {
			t.Fatalf("missing %s: %v", f, argv)
		}
	}
	permOK := false
	for i := 0; i+1 < len(argv); i++ {
		if argv[i] == "--permission-mode" && argv[i+1] == "bypassPermissions" {
			permOK = true
		}
	}
	if !permOK {
		t.Fatalf("missing --permission-mode bypassPermissions: %v", argv)
	}

	// MCP locked to our config: --strict-mcp-config present WHEN --mcp-config is
	// (load-bearing under bypassPermissions — PD 4c405a91).
	if !hasArg(argv, "--strict-mcp-config") {
		t.Fatalf("missing --strict-mcp-config when --mcp-config supplied: %v", argv)
	}

	// Negative: with NO --mcp-config, --strict-mcp-config must be ABSENT (strict with
	// zero servers = zero MCP tools — the wrong outcome).
	noMCP, _, err := BuildStreamingArgv("01J9ZK7QW8X2YB3C4D5E6F7G8H", "claude", "", 0, 0, "", nil, "", false)
	if err != nil {
		t.Fatalf("BuildStreamingArgv (no mcp): %v", err)
	}
	if hasArg(noMCP, "--strict-mcp-config") {
		t.Fatalf("--strict-mcp-config present without --mcp-config (would zero out MCP tools): %v", noMCP)
	}
}

func TestEncodeUserMessage(t *testing.T) {
	b, err := EncodeUserMessage("do the thing")
	if err != nil {
		t.Fatalf("EncodeUserMessage: %v", err)
	}
	s := string(b)
	if !strings.HasSuffix(s, "\n") {
		t.Fatalf("not newline-terminated: %q", s)
	}
	if !strings.Contains(s, `"type":"user"`) {
		t.Fatalf("missing user envelope: %q", s)
	}
	if !strings.Contains(s, `"role":"user"`) {
		t.Fatalf("missing role: %q", s)
	}
	if !strings.Contains(s, "do the thing") {
		t.Fatalf("missing message text: %q", s)
	}
}

func TestBuildStreamingArgv_ConcurrencyEnabled(t *testing.T) {
	argv, sysPrompt, err := BuildStreamingArgv("agent-concurrent-test", "claude", "", 0, 0, "", nil, "", true)
	if err != nil {
		t.Fatalf("BuildStreamingArgv: %v", err)
	}
	_ = argv
	for _, want := range []string{
		"SUPERVISOR control plane",
		"EXECUTORS are isolated execution units that this same Agent forks",
		"not outside contractors",
		"does not move responsibility away from you",
		"Do not say or imply that an \"external executor\" failed to deliver",
	} {
		if !strings.Contains(sysPrompt, want) {
			t.Fatalf("concurrent prompt missing unified identity contract %q:\n%s", want, sysPrompt)
		}
	}
	if strings.Contains(sysPrompt, "Only ONE task runs at a time") {
		t.Fatal("concurrent mode must NOT contain single-task instruction")
	}

	// Single-task mode: must use the original prompt.
	_, stPrompt, err := BuildStreamingArgv("agent-single-test", "claude", "", 0, 0, "", nil, "", false)
	if err != nil {
		t.Fatalf("BuildStreamingArgv: %v", err)
	}
	if !strings.Contains(stPrompt, "Only ONE task runs at a time") {
		t.Fatal("single-task mode must contain single-task instruction")
	}
	if strings.Contains(stPrompt, "SUPERVISOR control plane") {
		t.Fatal("single-task mode must NOT contain concurrent supervisor/executor instruction")
	}
}
