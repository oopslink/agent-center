package api

import (
	"context"
	"strings"

	"github.com/oopslink/agent-center/internal/agent"
	"github.com/oopslink/agent-center/internal/conversation"
	"github.com/oopslink/agent-center/internal/mention"
)

// T460 — @mention id/ref resolution + unresolved-mention reporting.
//
// The reported bug: post_message matched @mentions ONLY by exact display_name, so
// a handle written as an id/ref ("@agent-center-ba6bc42a", "agent:agent-ba6bc42a")
// or a mistyped display_name matched nobody and woke no one — SILENTLY (no error,
// no feedback to the sender, the target never woken). The wake-side fix lives in
// the WakeProjector (mentionsAgent now also matches id/ref, and structural
// mention_refs bypass the text gate). This file is the SEND-side minimal delivery
// check (③): it classifies a post's intended @mentions so the sender gets
// immediate, actionable feedback when one resolves to nobody.

// mentionsReport is the post_message "did your @mentions land?" result (T460 ③).
// resolved lists the @mentions that reach a real participant / known org agent;
// unresolved lists the ones that match nobody, each with a nearest-name "did you
// mean" suggestion. It is INFORMATION ONLY — the message is always sent regardless.
type mentionsReport struct {
	Resolved   []string            `json:"resolved"`
	Unresolved []unresolvedMention `json:"unresolved"`
}

// unresolvedMention is one @mention that matched no participant / known agent.
// Token is the handle as written (with leading "@" for a text token, or the raw
// "agent:<id>" for a structural ref); DidYouMean is the nearest known display_name
// ("@name"), omitted when nothing is close enough to suggest.
type unresolvedMention struct {
	Token      string `json:"token"`
	DidYouMean string `json:"did_you_mean,omitempty"`
}

// normalizeMentionRefs trims, de-dupes, and drops empties from the structural
// mention_refs a sender passed to post_message, preserving the "agent:<id>" form
// the WakeProjector matches on. Order is preserved (first occurrence wins).
func normalizeMentionRefs(refs []string) []string {
	if len(refs) == 0 {
		return nil
	}
	seen := map[string]bool{}
	var out []string
	for _, r := range refs {
		r = strings.TrimSpace(r)
		if r == "" || seen[r] {
			continue
		}
		seen[r] = true
		out = append(out, r)
	}
	return out
}

// buildMentionsReport classifies the intended @mentions of a post — the text
// @tokens plus the structural mention_refs — against the org agent roster and the
// conversation's participant display names. It returns nil when there is nothing
// to classify (no @tokens and no mention_refs) so the happy path stays free of a
// roster query. The caller surfaces the report only when it has unresolved
// entries (a fully-resolved post carries no mentions block).
func (s *Server) buildMentionsReport(ctx context.Context, d HandlerDeps, a *agent.Agent, conv *conversation.Conversation, content string, mentionRefs []string) *mentionsReport {
	tokens := mention.ExtractTokens(content)
	refs := normalizeMentionRefs(mentionRefs)
	if len(tokens) == 0 && len(refs) == 0 {
		return nil
	}
	if d.AgentSvc == nil {
		return nil // roster unavailable → cannot classify (degrade to silent, never block)
	}
	agents, err := d.AgentSvc.ListAgents(ctx, a.OrganizationID())
	if err != nil {
		return nil // a roster read error must not break the send (③: info only)
	}

	// nameSet maps a lowercased display_name → its canonical form, for exact-name
	// resolution and as the suggestion pool. It draws from BOTH the org agent roster
	// AND the conversation's live participants (so a valid HUMAN @mention resolves and
	// is not falsely flagged — humans resolve via the optional DisplayNameResolver).
	nameSet := map[string]string{}
	var names []string
	addName := func(n string) {
		n = strings.TrimSpace(n)
		if n == "" {
			return
		}
		if _, ok := nameSet[strings.ToLower(n)]; ok {
			return
		}
		nameSet[strings.ToLower(n)] = n
		names = append(names, n)
	}
	// idForms holds every agent id string a token may target (entity id + member id).
	var idForms []string
	for _, ag := range agents {
		addName(ag.Profile().Name)
		if id := string(ag.ID()); id != "" {
			idForms = append(idForms, id)
		}
		if mid := ag.IdentityMemberID(); mid != "" {
			idForms = append(idForms, mid)
		}
	}
	if d.DisplayNameResolver != nil {
		for _, part := range conv.Participants() {
			if !part.IsActive() {
				continue
			}
			if n, ok := d.DisplayNameResolver(ctx, string(part.IdentityID)); ok {
				addName(n)
			}
		}
	}

	var resolved []string
	var unresolved []unresolvedMention

	for _, tok := range tokens {
		if tok == mention.AllToken {
			resolved = append(resolved, "@"+tok) // broadcast addresses everyone
			continue
		}
		if canon, ok := nameSet[tok]; ok {
			resolved = append(resolved, "@"+canon)
			continue
		}
		matched := false
		for _, idf := range idForms {
			if mention.TokenMatchesID(tok, idf) {
				matched = true
				break
			}
		}
		if matched {
			resolved = append(resolved, "@"+tok)
			continue
		}
		unresolved = append(unresolved, unresolvedMention{
			Token:      "@" + tok,
			DidYouMean: suggestName(tok, names),
		})
	}

	// Structural mention_refs: resolved iff the ref names a known org agent (by id).
	for _, ref := range refs {
		bare := strings.TrimSpace(strings.TrimPrefix(ref, "agent:"))
		matched := false
		for _, idf := range idForms {
			if strings.EqualFold(bare, idf) || mention.TokenMatchesID(bare, idf) {
				matched = true
				break
			}
		}
		if matched {
			resolved = append(resolved, ref)
		} else {
			// A ref is an id, not a name — no name-typo suggestion makes sense.
			unresolved = append(unresolved, unresolvedMention{Token: ref})
		}
	}

	return &mentionsReport{Resolved: resolved, Unresolved: unresolved}
}

// suggestName returns the nearest known display_name ("@name") to an unresolved
// token, or "" when nothing is close enough (so a wildly-wrong token gets no
// misleading suggestion). "Close enough" scales with the token length so a 1-char
// typo on a long handle still suggests while an unrelated short token does not.
func suggestName(token string, names []string) string {
	token = strings.ToLower(strings.TrimSpace(token))
	if token == "" || len(names) == 0 {
		return ""
	}
	best, bestDist := "", -1
	for _, n := range names {
		d := levenshtein(token, strings.ToLower(n))
		if bestDist < 0 || d < bestDist {
			best, bestDist = n, d
		}
	}
	limit := len(token) / 2
	if limit < 3 {
		limit = 3
	}
	if best == "" || bestDist > limit {
		return ""
	}
	return "@" + best
}

// levenshtein is the classic edit distance (insertions/deletions/substitutions),
// used only for the small "did you mean" suggestion over a bounded roster.
func levenshtein(a, b string) int {
	la, lb := len(a), len(b)
	if la == 0 {
		return lb
	}
	if lb == 0 {
		return la
	}
	prev := make([]int, lb+1)
	curr := make([]int, lb+1)
	for j := 0; j <= lb; j++ {
		prev[j] = j
	}
	for i := 1; i <= la; i++ {
		curr[0] = i
		for j := 1; j <= lb; j++ {
			cost := 1
			if a[i-1] == b[j-1] {
				cost = 0
			}
			curr[j] = min3(prev[j]+1, curr[j-1]+1, prev[j-1]+cost)
		}
		prev, curr = curr, prev
	}
	return prev[lb]
}

func min3(a, b, c int) int {
	m := a
	if b < m {
		m = b
	}
	if c < m {
		m = c
	}
	return m
}
