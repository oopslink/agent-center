package claudestream

import (
	"crypto/sha1"
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"github.com/oopslink/agent-center/internal/agentadapter"
	"github.com/oopslink/agent-center/internal/agentadapter/claudecode"
)

// sessionNamespace is a fixed namespace for deriving claude --session-id UUIDs
// from agent ids (v2.7 D2-c). Arbitrary but stable 16 bytes ("agentcenter-v2.7").
var sessionNamespace = [16]byte{
	0x61, 0x67, 0x65, 0x6e, 0x74, 0x63, 0x65, 0x6e,
	0x74, 0x65, 0x72, 0x2d, 0x76, 0x32, 0x2e, 0x37,
}

// SessionUUID derives a DETERMINISTIC RFC 4122 v5 UUID from an agent id AND its
// reset epoch, for use as claude's --session-id. claude rejects a non-UUID
// session-id (validated against real claude 2.1.156: a raw ULID → "Invalid
// session ID. Must be a valid UUID.").
//
// EPOCH = clean-slate reset key (v2.7 D2-f s3b-2a). The id is deterministic in
// (agentID, epoch):
//   - SAME (agentID, epoch) → SAME uuid. This is what makes process-survival /
//     re-attach AND mode-B crash-RELAUNCH resume the SAME claude session: the
//     relaunch reads the agent's DURABLE current epoch (NOT 0) and re-derives the
//     identical session-id, so claude continues its conversation. (Relaunching at
//     a hardcoded 0 would mistake a crash for a reset and silently drop context —
//     the trap this epoch plumbing exists to avoid.)
//   - epoch++ → a NEW uuid → a clean-slate claude session. This is exactly what a
//     RESET wants: a fresh conversation with no carried context.
//
// epoch 0 is the initial/base epoch (a never-reset agent). distinct agent ids →
// distinct uuids (no "already in use" collisions). Uses crypto/sha1 (stdlib, no
// new dep).
func SessionUUID(agentID string, epoch int) string {
	h := sha1.New()
	h.Write(sessionNamespace[:])
	h.Write([]byte(agentID))
	// Mix the epoch after a 0x00 separator so the agent-id bytes and the epoch
	// bytes cannot alias across the boundary (e.g. agent "x\x00\x01" + epoch 0 vs
	// agent "x" + epoch 1 hash distinctly).
	var ep [9]byte
	ep[0] = 0x00
	binary.BigEndian.PutUint64(ep[1:], uint64(epoch))
	h.Write(ep[:])
	sum := h.Sum(nil)
	var u [16]byte
	copy(u[:], sum[:16])
	u[6] = (u[6] & 0x0f) | 0x50 // version 5
	u[8] = (u[8] & 0x3f) | 0x80 // RFC 4122 variant
	return fmt.Sprintf("%x-%x-%x-%x-%x", u[0:4], u[4:6], u[6:8], u[8:10], u[10:16])
}

// SessionUUIDGen derives the session-id for an agent at a given crash-relaunch
// fork GENERATION (v2.7 GATE-7 Mode-B fix). A hard-killed claude never releases
// its session-id lock, so a relaunch that re-used the same id hits "Session ID
// already in use" and fails to boot; each Mode-B relaunch instead forks the prior
// session into a NEW id derived at the next generation.
//
// BY CONSTRUCTION, generation 0 returns the EXACT pre-fix SessionUUID(agentID,
// epoch) — it literally delegates, with zero recomputation — so every existing /
// initial agent's session-id is byte-for-byte unchanged (no silent session
// migration). The generation is mixed in ONLY for generation > 0, after a second
// 0x00 separator so generation bytes cannot alias with the epoch bytes.
func SessionUUIDGen(agentID string, epoch, generation int) string {
	if generation == 0 {
		return SessionUUID(agentID, epoch) // delegate: gen 0 ≡ pre-fix id, byte-identical
	}
	h := sha1.New()
	h.Write(sessionNamespace[:])
	h.Write([]byte(agentID))
	var ep [9]byte
	ep[0] = 0x00
	binary.BigEndian.PutUint64(ep[1:], uint64(epoch))
	h.Write(ep[:])
	var gen [9]byte
	gen[0] = 0x00
	binary.BigEndian.PutUint64(gen[1:], uint64(generation))
	h.Write(gen[:])
	sum := h.Sum(nil)
	var u [16]byte
	copy(u[:], sum[:16])
	u[6] = (u[6] & 0x0f) | 0x50 // version 5
	u[8] = (u[8] & 0x3f) | 0x80 // RFC 4122 variant
	return fmt.Sprintf("%x-%x-%x-%x-%x", u[0:4], u[4:6], u[6:8], u[8:10], u[10:16])
}

// BuildStreamingArgv assembles the VALIDATED long-lived streaming-input claude
// argv for an agent, reusing the SAME pipeline as the daemon's execLauncher:
//
//	claudecode.New(binary).BuildCommand(SpawnRequest{ExecutionID: SessionUUID(agentID), ...})
//	→ rewriteForStreamingInput(...)            (--print/--input-format/--output-format/--verbose
//	                                            + v2.7 security flags: --setting-sources user,project
//	                                            and the non-interactive permission group)
//	→ + BuildMCPConfigArg(mcpConfigPath)       (--mcp-config <path> + --strict-mcp-config),
//	                                            when non-empty
//
// It returns the FULL argv ([binary, args...]) ready for exec.Command AND the
// assembled system prompt string (for the caller to persist to SYSTEM.md). This is
// the single source of truth for the streaming claude invocation; the v2.7
// agent-supervisor subcommand calls it instead of duplicating the flag rewrite.
// binary empty → adapter default ("claude" on PATH). The supervisor receives only
// the mcp-config FILE PATH — never the worker token (the daemon generates the
// config; minimal key surface).
//
// epoch + generation derive the --session-id via SessionUUIDGen(agentID, epoch,
// generation): the supervisor subcommand forwards its --reset-epoch / --generation
// flags here so a clean-slate reset spawns a fresh claude session while a
// crash-relaunch resumes/forks. generation 0 == the pre-fix SessionUUID(agentID,
// epoch) (byte-identical), so initial/normal starts are unchanged.
//
// resumeFromSessionID is the v2.7 GATE-7 Mode-B FORK input: when non-empty, the
// argv adds `--resume <resumeFromSessionID> --fork-session`, so claude forks that
// (possibly still-locked) prior session's conversation into the NEW --session-id —
// the lock-sidestepping relaunch. Empty ⇒ a plain start/resume of the --session-id
// (no fork), the unchanged initial/normal-start path.
// extraSystemPrompt (W2 memory): when non-empty it is appended after the
// work-queue operating instructions in the SAME --append-system-prompt value, so
// the agent's scoped memory harness context (layout guide + global/supervisor
// memory; see cognition/memory.Engine.HarnessContext) rides the same idempotent,
// re-applied-every-launch channel as the work-queue prompt. Empty ⇒ byte-for-byte
// the pre-memory argv (every existing call site passes "").
func BuildStreamingArgv(agentID, binary, mcpConfigPath string, epoch, generation int, resumeFromSessionID string, env map[string]string, extraSystemPrompt string, concurrencyEnabled bool) ([]string, string, error) {
	if agentID == "" {
		return nil, "", errors.New("claudestream: agent_id required")
	}
	sysPrompt := AgentWorkQueueSystemPrompt
	if concurrencyEnabled {
		sysPrompt = OrchestratorSystemPrompt
	}
	if strings.TrimSpace(extraSystemPrompt) != "" {
		sysPrompt = sysPrompt + "\n\n" + extraSystemPrompt
	}
	adapter := claudecode.New(binary)
	req := agentadapter.SpawnRequest{
		ExecutionID: SessionUUIDGen(agentID, epoch, generation),
		Prompt:      longLivedSentinelPrompt,
		// v2.8.1 #278 D PR4a: the pull-model work-queue operating instructions as a
		// persistent --append-system-prompt — re-applied every launch, idempotent
		// (not conversation history). See agent_system_prompt.go. W2 appends the
		// memory harness context (extraSystemPrompt) to this same value.
		SystemPrompt: sysPrompt,
		Env:          env,
	}
	cmdSpec, err := adapter.BuildCommand(req)
	if err != nil {
		return nil, "", fmt.Errorf("claudestream: build command: %w", err)
	}
	args := rewriteForStreamingInput(cmdSpec.Args)
	if mcpConfigPath != "" {
		mcp, err := adapter.BuildMCPConfigArg(mcpConfigPath)
		if err != nil {
			return nil, "", fmt.Errorf("claudestream: mcp-config arg: %w", err)
		}
		args = append(args, mcp.Args...)
		// v2.7 security: lock MCP discovery to ONLY this --mcp-config document — claude
		// must NOT also load operator/workspace MCP sources (.mcp.json, ~/.claude.json
		// mcpServers). Under bypassPermissions this is LOAD-BEARING (PD ruling 4c405a91):
		// without it, bypass would auto-allow un-vetted operator MCP tools, collapsing
		// the "MCP surface = our OQ4 config only" boundary. This MCP pinning is now the
		// SOLE remaining isolation lever for MCP servers, since `--setting-sources` was
		// changed from "" to user,project (#182) and therefore no longer suppresses the
		// operator's settings. Added ONLY alongside --mcp-config (strict with no servers
		// = zero MCP tools).
		args = append(args, "--strict-mcp-config")
	}
	// Mode-B fork: resume the prior (possibly lock-held) session's conversation into
	// the fresh --session-id above. claude honors `--session-id NEW --resume OLD
	// --fork-session` by adopting NEW exactly while seeding it from OLD's history
	// (validated against real claude 2.1.156).
	if resumeFromSessionID != "" {
		args = append(args, "--resume", resumeFromSessionID, "--fork-session")
	}
	return append([]string{cmdSpec.Binary}, args...), sysPrompt, nil
}

// longLivedSentinelPrompt satisfies BuildCommand's non-empty-prompt validation;
// rewriteForStreamingInput strips the resulting `-p <sentinel>` pair.
const longLivedSentinelPrompt = "__ac_streaming_input__"

// rewriteForStreamingInput converts the one-shot argv produced by
// adapter.BuildCommand (`... -p <prompt>`) into the long-lived streaming-input
// argv VALIDATED against real claude 2.1.156:
//
//	--print --input-format stream-json --output-format stream-json --verbose
//	--session-id <agentID>   (+ --mcp-config <path>, appended by the caller)
//
// It also ensures (once) the v2.7 security/launch flags: `--setting-sources user,project`
// (#182: auth via the user source + the agent's own project source) and the non-interactive permission group
// (`--allow-dangerously-skip-permissions --dangerously-skip-permissions
// --permission-mode bypassPermissions`). See the ensure-once tail for rationale.
//
// It KEEPS `--print`/`-p` as a FLAG (rewriting `-p` → the canonical `--print`)
// but DROPS the positional prompt value (the sentinel), then ENSURES
// `--input-format stream-json`, `--output-format stream-json`, and `--verbose`
// are all present exactly once (adding any that are missing; never
// duplicating). `--session-id <id>` and other flags pass through unchanged.
func rewriteForStreamingInput(in []string) []string {
	out := make([]string, 0, len(in)+4)
	hasPrint := false
	hasInputFormat := false
	hasOutputFormat := false
	hasVerbose := false

	for i := 0; i < len(in); i++ {
		switch in[i] {
		case "-p", "--print":
			// Keep the flag (canonicalised to --print) but DROP the positional
			// prompt value that follows `-p`.
			if in[i] == "-p" && i+1 < len(in) {
				i++ // skip the sentinel prompt value
			}
			if !hasPrint {
				out = append(out, "--print")
				hasPrint = true
			}
		case "--input-format":
			out = append(out, in[i])
			if i+1 < len(in) {
				i++
				out = append(out, in[i])
			}
			hasInputFormat = true
		case "--output-format":
			out = append(out, in[i])
			if i+1 < len(in) {
				i++
				out = append(out, in[i])
			}
			hasOutputFormat = true
		case "--verbose":
			if !hasVerbose {
				out = append(out, in[i])
				hasVerbose = true
			}
		default:
			out = append(out, in[i])
		}
	}

	if !hasPrint {
		out = append(out, "--print")
	}
	if !hasInputFormat {
		out = append(out, "--input-format", "stream-json")
	}
	if !hasOutputFormat {
		out = append(out, "--output-format", "stream-json")
	}
	if !hasVerbose {
		out = append(out, "--verbose")
	}
	// v2.7 security/launch flags (ensure-once; appended if the adapter did not
	// already supply them):
	//   --setting-sources user,project
	//                          v2.7 #182 (acceptance FINDING-G): claude's keychain
	//                          /login credential is loaded via the "user" setting
	//                          source. The previous value "" loaded NO sources →
	//                          suppressed auth → every agent turn failed with
	//                          `403 Request not allowed` (empirically isolated:
	//                          `--setting-sources ""` 403, `user` OK; CLAUDE_CONFIG_DIR
	//                          override also breaks auth — the cred is bound to the
	//                          default ~/.claude). So we load "user" (auth + the
	//                          operator's user-level settings) PLUS "project" so the
	//                          agent can carry its OWN config in <workspace>/.claude.
	//                          TRADEOFF (@oopslink/PD ruling): this loads the operator's
	//                          USER-level ~/.claude settings (hooks/env/plugins) in the
	//                          agent — user-level isolation is NOT solved by setting-
	//                          sources (auth + operator settings share the user source);
	//                          full isolation is deferred to v2.8 via a setup-token
	//                          (CLAUDE_CODE_OAUTH_TOKEN) path. `--strict-mcp-config`
	//                          (added alongside --mcp-config) still pins MCP servers.
	//   --allow-dangerously-skip-permissions / --dangerously-skip-permissions /
	//   --permission-mode bypassPermissions
	//                          DETERMINISTIC non-interactive tool permission (PD ruling
	//                          09394dbd): claude's permission prompt cannot be shown
	//                          headless and is NOT our security boundary — the real
	//                          boundaries are the restricted worker OS user + workspace
	//                          cwd (local tools), per-call AppService MCP authz, and the
	//                          layer-1 env allowlist. This is the slock-verified group.
	if !hasArg(out, "--setting-sources") {
		out = append(out, "--setting-sources", "user,project")
	}
	if !hasArg(out, "--allow-dangerously-skip-permissions") {
		out = append(out, "--allow-dangerously-skip-permissions")
	}
	if !hasArg(out, "--dangerously-skip-permissions") {
		out = append(out, "--dangerously-skip-permissions")
	}
	if !hasArg(out, "--permission-mode") {
		out = append(out, "--permission-mode", "bypassPermissions")
	}
	return out
}

// hasArg reports whether args contains the exact token x.
func hasArg(args []string, x string) bool {
	for _, a := range args {
		if a == x {
			return true
		}
	}
	return false
}

// EncodeUserMessage encodes a plain user message as one newline-terminated
// stream-json line for claude's `--input-format stream-json`.
//
// FLAG (D2-g): this schema is a documented BEST GUESS — no stream-json INPUT
// encoding exists in-repo (the adapter only encodes OUTPUT). It mirrors the
// Anthropic Messages content-block shape:
//
//	{"type":"user","message":{"role":"user","content":[{"type":"text","text":"<msg>"}]}}\n
//
// It is deliberately isolated in this one function so D2-g can correct it
// against the real claude binary without touching the session lifecycle.
func EncodeUserMessage(msg string) ([]byte, error) {
	type textBlock struct {
		Type string `json:"type"`
		Text string `json:"text"`
	}
	type innerMessage struct {
		Role    string      `json:"role"`
		Content []textBlock `json:"content"`
	}
	type userEnvelope struct {
		Type    string       `json:"type"`
		Message innerMessage `json:"message"`
	}
	env := userEnvelope{
		Type: "user",
		Message: innerMessage{
			Role:    "user",
			Content: []textBlock{{Type: "text", Text: msg}},
		},
	}
	b, err := json.Marshal(env)
	if err != nil {
		return nil, fmt.Errorf("claudestream: encode user message: %w", err)
	}
	return append(b, '\n'), nil
}
