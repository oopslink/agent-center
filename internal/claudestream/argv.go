package claudestream

import (
	"crypto/sha1"
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"

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

// BuildStreamingArgv assembles the VALIDATED long-lived streaming-input claude
// argv for an agent, reusing the SAME pipeline as the daemon's execLauncher:
//
//	claudecode.New(binary).BuildCommand(SpawnRequest{ExecutionID: SessionUUID(agentID), ...})
//	→ rewriteForStreamingInput(...)            (--print/--input-format/--output-format/--verbose)
//	→ + BuildMCPConfigArg(mcpConfigPath)       (--mcp-config <path>), when non-empty
//
// It returns the FULL argv ([binary, args...]) ready for exec.Command. This is
// the single source of truth for the streaming claude invocation; the v2.7
// agent-supervisor subcommand calls it instead of duplicating the flag rewrite.
// binary empty → adapter default ("claude" on PATH). The supervisor receives only
// the mcp-config FILE PATH — never the worker token (the daemon generates the
// config; minimal key surface).
//
// epoch derives the --session-id via SessionUUID(agentID, epoch): the supervisor
// subcommand forwards its --reset-epoch flag here so a clean-slate reset spawns a
// fresh claude session while a crash-relaunch (same epoch) resumes the same one.
func BuildStreamingArgv(agentID, binary, mcpConfigPath string, epoch int, env map[string]string) ([]string, error) {
	if agentID == "" {
		return nil, errors.New("claudestream: agent_id required")
	}
	adapter := claudecode.New(binary)
	req := agentadapter.SpawnRequest{
		ExecutionID: SessionUUID(agentID, epoch),
		Prompt:      longLivedSentinelPrompt,
		Env:         env,
	}
	cmdSpec, err := adapter.BuildCommand(req)
	if err != nil {
		return nil, fmt.Errorf("claudestream: build command: %w", err)
	}
	args := rewriteForStreamingInput(cmdSpec.Args)
	if mcpConfigPath != "" {
		mcp, err := adapter.BuildMCPConfigArg(mcpConfigPath)
		if err != nil {
			return nil, fmt.Errorf("claudestream: mcp-config arg: %w", err)
		}
		args = append(args, mcp.Args...)
	}
	return append([]string{cmdSpec.Binary}, args...), nil
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
	return out
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
