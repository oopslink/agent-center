// Package mention holds the single-source @mention text matcher shared by
// the wake projector (who gets woken) and the v2.8 #268 unread badge model
// (mention_count). Keeping one implementation guarantees a mention badge
// counts exactly the messages that would actually wake/notify the user —
// no "phantom mention" that never woke anyone (Tester2 §2.3 wake-consistency).
package mention

import "strings"

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
