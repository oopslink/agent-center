package inbound

import (
	"fmt"
	"strings"
)

// SlashCommandParser is a pure function that recognises the v1 slash
// command vocabulary (plan-7 § 3.2 + bridge/01 § 9.1):
//
//   - /track <task_id>
//   - /answer <input_request_id> <choice ...>
//   - /dispatch ... (v1 stub; rejected at Parse time via
//     ErrSlashFeatureDeferred so the router can emit a structured
//     reject without a downstream service call)
//
// Inputs:
//
//   - text: the raw message body. The caller must NOT pre-strip
//     leading whitespace; the parser handles it. Mentions like
//     "@bot" on feishu are normalised away by the SDK before they
//     reach us, so they are not stripped here.
//
// Outputs:
//
//   - non-slash input (does NOT start with "/" once trimmed):
//     returns (nil, nil) → caller routes via @bot path
//   - slash input that parses: returns (&SlashCommand{...}, nil)
//   - parseable verb but disabled in v1 (/dispatch):
//     returns (cmd, ErrSlashFeatureDeferred). Caller can still log
//     the verb + raw via cmd.
//   - unrecognised verb: returns (nil, ErrSlashUnknownVerb wrapped)
//   - too few args: returns (cmd partially populated, wrapped
//     ErrSlashInsufficientArgs) so the router can include the verb
//     + raw in its reject message.
//   - empty body after the slash: (nil, ErrSlashEmpty wrapped).
//
// The function never panics. It does not allocate maps or heavy
// structures; the caller controls all subsequent IO.
type SlashCommandParser struct{}

// NewSlashCommandParser returns a stateless parser instance.
func NewSlashCommandParser() *SlashCommandParser { return &SlashCommandParser{} }

// Parse maps the message body to a SlashCommand.
func (SlashCommandParser) Parse(text string) (*SlashCommand, error) {
	trimmed := strings.TrimSpace(text)
	if !strings.HasPrefix(trimmed, "/") {
		return nil, nil
	}
	body := strings.TrimSpace(trimmed[1:])
	if body == "" {
		return nil, fmt.Errorf("%w: empty body after '/'", ErrSlashEmpty)
	}
	// Split into tokens by whitespace. We split with strings.Fields so
	// runs of whitespace (incl. tabs / CJK full-width via the underlying
	// implementation rules) collapse predictably.
	tokens := strings.Fields(body)
	if len(tokens) == 0 {
		return nil, fmt.Errorf("%w: empty body after '/'", ErrSlashEmpty)
	}
	verbStr := strings.ToLower(tokens[0])
	args := tokens[1:]
	cmd := &SlashCommand{Verb: SlashVerb(verbStr), Args: args, Raw: trimmed}
	switch verbStr {
	case string(SlashVerbTrack):
		if len(args) < 1 {
			return cmd, fmt.Errorf("%w: usage: /track <task_id>", ErrSlashInsufficientArgs)
		}
		// Trim trailing args; only first is meaningful but keep the
		// rest in Args for audit.
		return cmd, nil
	case string(SlashVerbAnswer):
		if len(args) < 2 {
			return cmd, fmt.Errorf("%w: usage: /answer <input_request_id> <choice>", ErrSlashInsufficientArgs)
		}
		// /answer <ireq> <choice...> — anything past arg 1 joins as the
		// choice; we keep both Args[0] and a joined Args[1:].
		return cmd, nil
	case string(SlashVerbDispatch):
		// v1 stub: reject right at parse-time. We still return cmd so
		// the router can log the raw verb + args for audit.
		return cmd, fmt.Errorf("%w: /dispatch is reserved for v2; use @bot to escalate", ErrSlashFeatureDeferred)
	default:
		return nil, fmt.Errorf("%w: %q (allowed: /track /answer /dispatch)", ErrSlashUnknownVerb, verbStr)
	}
}

// AnswerChoice extracts the choice portion from a parsed `/answer` command.
// It joins Args[1:] with a single space. Panics never — returns "" when
// the command shape is wrong (caller already validated via the parser
// error).
func (c *SlashCommand) AnswerChoice() string {
	if c == nil || c.Verb != SlashVerbAnswer || len(c.Args) < 2 {
		return ""
	}
	return strings.Join(c.Args[1:], " ")
}

// TaskID extracts the task_id from a parsed `/track` command. Returns
// "" when shape is wrong.
func (c *SlashCommand) TaskID() string {
	if c == nil || c.Verb != SlashVerbTrack || len(c.Args) < 1 {
		return ""
	}
	return c.Args[0]
}

// InputRequestID extracts the input_request_id from a parsed `/answer`
// command. Returns "" when shape is wrong.
func (c *SlashCommand) InputRequestID() string {
	if c == nil || c.Verb != SlashVerbAnswer || len(c.Args) < 1 {
		return ""
	}
	return c.Args[0]
}
