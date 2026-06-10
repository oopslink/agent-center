package api

import (
	"context"
	"net/http"
	"strings"
	"time"

	"github.com/oopslink/agent-center/internal/conversation"
)

// previewMaxLen caps the plain-text message/activity preview the list-enrich DTOs
// carry (v2.8.1 #278). ~140 chars is a tweet-length one-liner — enough for the
// sidebar/last-activity glance, short enough to keep the list payload lean.
const previewMaxLen = 140

// participantsCap bounds the inline participant members list on a channel row
// (v2.8.1 #278). The full roster is fetched via the participants endpoint; the
// list row shows only the first few for an avatar-stack glance. `count` is always
// the true active total so the UI can render "+N".
const participantsCap = 5

// recentMessagesCap bounds the per-row recent-messages preview (v2.8.1 #278).
const recentMessagesCap = 3

// plainTextPreview reduces a (possibly markdown) message body to a single-line,
// truncated plain-text preview for the list DTOs (v2.8.1 #278). It drops ``` fenced
// code blocks wholesale, collapses all runs of whitespace to single spaces, and
// cuts to previewMaxLen runes (appending an ellipsis when it had to cut). It never
// panics on odd input (unterminated fence → drop to end). Empty/whitespace-only →
// "".
func plainTextPreview(s string) string {
	s = stripFencedCode(s)
	// Collapse all whitespace (incl. newlines) to single spaces.
	s = strings.Join(strings.Fields(s), " ")
	if s == "" {
		return ""
	}
	r := []rune(s)
	if len(r) <= previewMaxLen {
		return s
	}
	return strings.TrimRight(string(r[:previewMaxLen]), " ") + "…"
}

// stripFencedCode removes ``` fenced blocks (keeping any text outside them). An
// unterminated opening fence drops everything from the fence to the end (the
// common "code dump with no closing fence" case) rather than leaking raw backticks.
func stripFencedCode(s string) string {
	if !strings.Contains(s, "```") {
		return s
	}
	var b strings.Builder
	rest := s
	for {
		i := strings.Index(rest, "```")
		if i < 0 {
			b.WriteString(rest)
			break
		}
		b.WriteString(rest[:i])
		after := rest[i+3:]
		j := strings.Index(after, "```")
		if j < 0 {
			// Unterminated fence → drop to end.
			break
		}
		rest = after[j+3:]
	}
	return b.String()
}

// refKindLabel returns "agent" for an "agent:"-prefixed ref, else "human" (v2.8.1
// #278 participant/sender kind). `system` and bare refs fall through to "human"
// (the safe default for a name-bearing sender row).
func refKindLabel(ref conversation.IdentityRef) string {
	if strings.HasPrefix(string(ref), "agent:") {
		return "agent"
	}
	return "human"
}

// nameResolver resolves identity refs → display names with a per-call cache so an
// enrich pass over a whole list never re-queries the same member (soft-ref +
// N+1-tolerant). A ref whose member row is gone (deleted member, retained soft
// string ref) degrades to a friendly cleaned handle — never a crash, never a
// blank. The handler builds ONE resolver per request and reuses it across rows.
type nameResolver struct {
	r     *http.Request
	d     HandlerDeps
	cache map[string]string
}

func newNameResolver(r *http.Request, d HandlerDeps) *nameResolver {
	return &nameResolver{r: r, d: d, cache: map[string]string{}}
}

// resolve returns a human-friendly display name for ref, never empty. Resolution
// order: identity repo display name → cleaned bare handle (soft-ref orphan
// tolerance). "system" resolves to "System".
func (nr *nameResolver) resolve(ctx context.Context, ref conversation.IdentityRef) string {
	key := string(ref)
	if v, ok := nr.cache[key]; ok {
		return v
	}
	name := nr.lookup(ctx, ref)
	nr.cache[key] = name
	return name
}

func (nr *nameResolver) lookup(ctx context.Context, ref conversation.IdentityRef) string {
	if ref == "system" {
		return "System"
	}
	bare := refBareID(ref)
	if nr.d.IdentityRepo != nil && bare != "" {
		if ident, err := nr.d.IdentityRepo.GetByID(ctx, bare); err == nil && ident != nil {
			if dn := strings.TrimSpace(ident.DisplayName()); dn != "" {
				return dn
			}
		}
	}
	// Soft-ref orphan tolerance: member row gone or unresolvable → friendly handle.
	return friendlyHandle(ref)
}

// friendlyHandle derives a clean, never-empty display fallback from a raw ref when
// the member can't be resolved (deleted member / soft string ref). It strips the
// kind prefix and, if nothing usable remains, falls back to the raw ref so the row
// always renders a name instead of crashing or going blank.
func friendlyHandle(ref conversation.IdentityRef) string {
	bare := strings.TrimSpace(refBareID(ref))
	if bare != "" {
		return bare
	}
	if s := strings.TrimSpace(string(ref)); s != "" {
		return s
	}
	return "Unknown"
}

// buildParticipants renders the channel-row participants DTO (v2.8.1 #278): the
// true active-participant count + the first participantsCap members as
// {identity_ref, display_name, kind}. Reuses the same name resolver as the recent-
// messages enrich so a deleted member degrades to a friendly handle, never a crash.
// v2.8.1 #278 (contract lock w/ PD/Dev2): the list row exposes `participants` as a
// ≤5 PREVIEW array (NOT the full set — the detail endpoint keeps the full list) plus
// a separate `participant_count` (the true active total). Returning them split avoids
// the cross-surface footgun of code assuming `participants` is complete.
func buildParticipants(ctx context.Context, nr *nameResolver, c *conversation.Conversation) (preview []map[string]any, count int) {
	preview = make([]map[string]any, 0, participantsCap)
	for _, p := range c.Participants() {
		if !p.IsActive() {
			continue
		}
		count++
		if len(preview) < participantsCap {
			preview = append(preview, map[string]any{
				"identity_id":  string(p.IdentityID),
				"display_name": nr.resolve(ctx, p.IdentityID),
				"kind":         refKindLabel(p.IdentityID),
			})
		}
	}
	return preview, count
}

// buildRecentMessages renders the channel-row recent_messages DTO (v2.8.1 #278):
// up to recentMessagesCap messages newest-first as {id, sender_identity_ref,
// sender_display_name, preview, posted_at}. msgs is expected newest-first (the
// repo's RecentByConversations contract); this caps + maps it. An empty channel
// yields a non-nil empty slice so the JSON is [] not null.
func buildRecentMessages(ctx context.Context, nr *nameResolver, msgs []*conversation.Message) []map[string]any {
	out := make([]map[string]any, 0, recentMessagesCap)
	for _, m := range msgs {
		if len(out) >= recentMessagesCap {
			break
		}
		out = append(out, map[string]any{
			"id":                  string(m.ID()),
			"sender_identity_id":  string(m.SenderIdentityID()),
			"sender_display_name": nr.resolve(ctx, m.SenderIdentityID()),
			"content":             plainTextPreview(m.Content()),
			"posted_at":           m.PostedAt().UTC().Format(time.RFC3339Nano),
		})
	}
	return out
}
