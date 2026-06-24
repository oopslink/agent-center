// Package mention holds the single-source @mention text matcher shared by
// the wake projector (who gets woken) and the v2.8 #268 unread badge model
// (mention_count). Keeping one implementation guarantees a mention badge
// counts exactly the messages that would actually wake/notify the user —
// no "phantom mention" that never woke anyone (Tester2 §2.3 wake-consistency).
package mention

import "strings"

// AllToken is the broadcast @mention ("@all") that addresses EVERY participant
// of a group conversation at once. Per @oopslink: it is effective ONLY when the
// sender is a human — the wake projector and the unread mention badge both gate
// the broadcast on a human sender, so an agent writing @all never triggers it
// (no agent-driven broadcast storm).
const AllToken = "all"

// MentionsAll reports whether text contains the broadcast @all token,
// case-insensitive and token-bounded (so @all matches but @allies does not). It
// is the single source of truth for @all detection, shared by the wake projector
// (who gets woken) and the #268 mention badge (mention_count), exactly like
// Present — so a broadcast badge counts the messages that would actually wake.
func MentionsAll(text string) bool {
	return TokenPresent(strings.ToLower(text), "@"+AllToken)
}

// Present reports whether text contains an @mention of name, case-insensitive
// and token-bounded so @Bot does not match @Bottom. Surrounding whitespace on
// name is trimmed; an empty name is never present.
func Present(text, name string) bool {
	name = strings.TrimSpace(name)
	if name == "" {
		return false
	}
	return TokenPresent(strings.ToLower(text), "@"+strings.ToLower(name))
}

// TokenPresent reports whether needle ("@name", already lowercased) appears
// in text (already lowercased) bounded by a non-word character on the right
// so @Bot ≠ @Bottom. It is the low-level primitive behind Present.
func TokenPresent(text, needle string) bool {
	from := 0
	for {
		i := strings.Index(text[from:], needle)
		if i < 0 {
			return false
		}
		end := from + i + len(needle)
		if end >= len(text) || !isWordChar(text[end]) {
			return true
		}
		from = from + i + 1
	}
}

func isWordChar(b byte) bool {
	return (b >= 'a' && b <= 'z') || (b >= '0' && b <= '9') || b == '_' || b == '-'
}

// minIDFragmentLen is the shortest id-fragment we accept for a contains-match
// when resolving an @mention written as an agent id/ref rather than a
// display_name (T460). Short enough to catch a real short-id ("ba6bc42a", 8
// chars) but long enough that an accidental substring of a longer token does not
// silently wake the wrong agent.
const minIDFragmentLen = 6

// ExtractTokens returns the distinct @mention tokens in text, lowercased and
// stripped of the leading "@", in first-appearance order. A token starts at an
// "@" that is at the start of text OR preceded by a non-word char (so an email's
// "user@host" is NOT a mention) and runs over word chars [a-z0-9_-] (so it stops
// at ":" — the "agent:<id>" colon-ref form is matched separately by ContainsRef).
// The broadcast "@all" is returned like any other token; callers decide meaning.
//
// T460 (id/ref resolution + unresolved-mention reporting) shares this single
// tokenizer with the wake projector and the post_message report so a mention the
// report flags as unresolved is exactly one the wake path would not have woken.
func ExtractTokens(text string) []string {
	lower := strings.ToLower(text)
	var out []string
	seen := map[string]bool{}
	for i := 0; i < len(lower); i++ {
		if lower[i] != '@' {
			continue
		}
		if i > 0 && isWordChar(lower[i-1]) {
			continue // mid-token "@" (e.g. an email) is not a mention
		}
		j := i + 1
		for j < len(lower) && isWordChar(lower[j]) {
			j++
		}
		tok := lower[i+1 : j]
		if tok == "" || seen[tok] {
			continue
		}
		seen[tok] = true
		out = append(out, tok)
	}
	return out
}

// ContainsRef reports whether the literal ref (e.g. "agent:agent-ba6bc42a")
// appears in text, case-insensitive and bounded on BOTH sides by a non-word char
// (or string edge) so "agent:abc" does not match inside "xagent:abcd". Unlike
// Present/TokenPresent (whose needle starts with "@", its own left boundary), a
// bare colon-ref needs the left boundary checked too. Used to resolve the
// "agent:<id>" ref form that may appear with or without a leading "@" (T460 ②).
func ContainsRef(text, ref string) bool {
	ref = strings.ToLower(strings.TrimSpace(ref))
	if ref == "" {
		return false
	}
	lower := strings.ToLower(text)
	from := 0
	for {
		i := strings.Index(lower[from:], ref)
		if i < 0 {
			return false
		}
		start := from + i
		end := start + len(ref)
		leftOK := start == 0 || !isWordChar(lower[start-1])
		rightOK := end >= len(lower) || !isWordChar(lower[end])
		if leftOK && rightOK {
			return true
		}
		from = start + 1
	}
}

// TokenMatchesID reports whether an @mention token (lowercased, no "@") targets
// an agent identified by idForm — either by exact match OR by containing idForm's
// unique id-fragment (the id with a leading "agent:"/"agent-" scheme stripped),
// gated at minIDFragmentLen so an 8-char short id matches but a 1–2 char stub does
// not. This is what lets "@agent-center-ba6bc42a" resolve to the agent whose
// member id is "agent-ba6bc42a" (fragment "ba6bc42a"), the T460 typo case.
func TokenMatchesID(token, idForm string) bool {
	token = strings.ToLower(strings.TrimSpace(token))
	id := strings.ToLower(strings.TrimSpace(idForm))
	if token == "" || id == "" {
		return false
	}
	if token == id {
		return true
	}
	frag := idFragment(id)
	return len(frag) >= minIDFragmentLen && strings.Contains(token, frag)
}

// idFragment strips a leading "agent:" then "agent-" scheme from an id so the
// member-id "agent-ba6bc42a" yields the discriminating fragment "ba6bc42a". A
// bare entity ULID is returned unchanged (it is already its own fragment).
func idFragment(id string) string {
	id = strings.TrimPrefix(id, "agent:")
	id = strings.TrimPrefix(id, "agent-")
	return id
}
