package team

import (
	"regexp"
	"sort"
	"strings"
)

// scrub.go implements the curation-assist scrub (design §9): scope filtering
// does NOT guarantee cleanliness — a team-scope lesson can still name a concrete
// repo / code name / path. So on extraction (and before export) we HIGHLIGHT
// suspected proprietary tokens for the human curator. This is an ASSIST, not an
// automatic sanitiser: nothing is removed, findings are surfaced for review.

// ScrubKind classifies a suspected proprietary token.
type ScrubKind string

const (
	// ScrubCodeName is a ticket / code name like "T950" or "T-1234".
	ScrubCodeName ScrubKind = "code_name"
	// ScrubPath is a filesystem-ish path ("internal/team/foo.go", "/etc/x").
	ScrubPath ScrubKind = "path"
	// ScrubURL is a URL / host reference.
	ScrubURL ScrubKind = "url"
	// ScrubRepoName is a repo-name-shaped token (e.g. "agent-center", "my_repo").
	ScrubRepoName ScrubKind = "repo_name"
)

// ScrubFinding is one highlighted suspected-proprietary token.
type ScrubFinding struct {
	// ExperienceSlug locates which experience the token came from ("" for a raw
	// text scrub).
	ExperienceSlug string
	Kind           ScrubKind
	// Token is the matched text.
	Token string
}

var (
	// "T950", "T-1234", "PROJ-42" style ticket / code names.
	reCodeName = regexp.MustCompile(`\b(?:[A-Z]{1,6}-?\d{2,})\b`)
	// URLs / hosts.
	reURL = regexp.MustCompile(`\bhttps?://[^\s)]+|\b[a-z0-9.-]+\.(?:com|net|org|io|dev|internal|local)\b`)
	// path-ish: two or more slash-separated segments, or a leading-slash abspath.
	rePath = regexp.MustCompile(`(?:^|\s)(/[A-Za-z0-9._-]+(?:/[A-Za-z0-9._-]+)+|[A-Za-z0-9._-]+/[A-Za-z0-9._/-]+)`)
	// repo-name-shaped: a hyphen/underscore-joined lowercase token (>= 2 parts),
	// e.g. "agent-center", "my_repo". Deliberately conservative but noisy — it is
	// a HIGHLIGHT for the human, not a filter.
	reRepoName = regexp.MustCompile(`\b[a-z0-9]+(?:[-_][a-z0-9]+)+\b`)
)

// commonHyphenWords are frequent English/technical hyphenated terms that would
// otherwise flood the repo-name highlight with false positives. They are still
// visible to the curator via other means; excluding them keeps the signal high.
var commonHyphenWords = map[string]struct{}{
	"table-driven": {}, "round-robin": {}, "least-busy": {}, "read-only": {},
	"author-time": {}, "run-time": {}, "end-to-end": {}, "cross-org": {},
	"per-agent": {}, "per-team": {}, "up-to-date": {}, "well-known": {},
	"fine-grained": {}, "first-class": {}, "opt-in": {}, "opt-out": {},
}

// ScrubText highlights suspected proprietary tokens in a raw string. slug is
// attached to each finding for provenance ("" when scrubbing free text).
func ScrubText(slug, text string) []ScrubFinding {
	var out []ScrubFinding
	seen := make(map[string]struct{})
	add := func(kind ScrubKind, tok string) {
		tok = strings.TrimSpace(tok)
		if tok == "" {
			return
		}
		key := string(kind) + "\x00" + tok
		if _, dup := seen[key]; dup {
			return
		}
		seen[key] = struct{}{}
		out = append(out, ScrubFinding{ExperienceSlug: slug, Kind: kind, Token: tok})
	}

	for _, m := range reCodeName.FindAllString(text, -1) {
		add(ScrubCodeName, m)
	}
	for _, m := range reURL.FindAllString(text, -1) {
		add(ScrubURL, m)
	}
	for _, m := range rePath.FindAllString(text, -1) {
		add(ScrubPath, m)
	}
	for _, m := range reRepoName.FindAllString(text, -1) {
		if _, common := commonHyphenWords[strings.ToLower(m)]; common {
			continue
		}
		// Skip tokens already reported as a path segment / url substring to avoid
		// double-flagging the obvious ones.
		add(ScrubRepoName, m)
	}

	sort.SliceStable(out, func(i, j int) bool {
		if out[i].Kind != out[j].Kind {
			return out[i].Kind < out[j].Kind
		}
		return out[i].Token < out[j].Token
	})
	return out
}

// ScrubExperience scrubs an experience's title, description and body, tagging
// every finding with the experience slug.
func ScrubExperience(e Experience) []ScrubFinding {
	return ScrubText(e.Slug, strings.Join([]string{e.Title, e.Description, e.Body}, "\n"))
}
