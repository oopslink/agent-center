package projectmanager

import "strings"

// =============================================================================
// merge-to-main hard acceptance gate — DETECTION half (issue-d2f14e0e, P0).
//
// b5ddb42e landed the CONSUMER: EnsureTaskRunnable → acceptanceVerdictBlocks
// hard-rejects a task carrying TagMergeToMain unless a resolved-pass upstream
// acceptance/decision CONDITION gate exists. But nothing PRODUCED the tag on real
// ship nodes, so in production the gate was opt-in / near-inert — an author who
// forgot to tag fell back to the P67 hole (a Ship node racing the verdict). This
// file owns the DETECTION predicate the plan builder uses to AUTO-STAMP
// TagMergeToMain onto ship / merge-to-main nodes (buildPlanGraph), closing the
// "consumer wired, source not provisioned" gap so the gate is truly un-bypassable.
//
// Detection is pure/domain so it is unit-testable in isolation; the stamp site is
// buildPlanGraph and the read site is acceptanceVerdictBlocks — both key off the
// SAME TagMergeToMain, so they can never drift.
// =============================================================================

// acceptanceMarkerTags are the task tags that EXPLICITLY mark a node as a
// merge-to-main / ship action requiring the upstream acceptance gate. A plan
// author tags the node with any of these to opt it into the hard gate
// deterministically (the authoritative signal; the title heuristic below is a
// backstop for un-tagged ship nodes). Tags are capped at 16 chars, so the aliases
// are short; the canonical TagMergeToMain is the tag the builder actually stamps
// and the run-gate consumes.
var acceptanceMarkerTags = map[string]struct{}{
	TagMergeToMain:     {}, // "merge-to-main" — the canonical stamped/consumed tag
	"merge":            {},
	"ship":             {},
	"needs-acceptance": {},
}

// RequiresAcceptance reports whether a task is a merge-to-main / ship action that
// must be hard-gated behind a passed upstream acceptance/decision condition. It is
// the plan builder's AUTO-STAMP predicate (buildPlanGraph stamps TagMergeToMain
// when this is true). Detection is fail-CLOSED biased for P0 safety: a false
// positive merely holds a non-merge node until acceptance passes (recoverable),
// while a false negative lets a real merge race the verdict (the P0 failure). It
// matches EITHER an explicit marker tag OR a tight "merge … main/master" title
// phrase.
func RequiresAcceptance(t *Task) bool {
	if t == nil {
		return false
	}
	for _, tag := range t.Tags() {
		if _, ok := acceptanceMarkerTags[strings.ToLower(strings.TrimSpace(tag))]; ok {
			return true
		}
	}
	return titleLooksLikeMergeToMain(t.Title())
}

// titleLooksLikeMergeToMain matches the canonical merge-to-main phrasing in a task
// title, DIRECTION-SENSITIVE: main/master must be the TARGET (right of a direction
// indicator — an arrow →/->/=>, or the word "to"/"into"), never the source. This
// covers the repo's mainstream arrow-style ship naming ("dev/team-phase1 → main",
// "feat→main", "Ship: dev/v2.44.0 -> main", "集成 X → main" — incl. the actual P67
// title "merge(release): … → main") as well as "merge to main". Crucially, a title
// where main is the SOURCE — "sync origin/main into dev" — does NOT gate, because we
// only accept main immediately after a direction token. Fail-closed biased
// (issue-d2f14e0e): a missed ship node is the P0 failure; an over-gated node merely
// waits for acceptance (recoverable).
func titleLooksLikeMergeToMain(title string) bool {
	low := strings.ToLower(title)
	// Canonicalize direction arrows to " > " FIRST — before any '-'/'_' munging — so
	// "feat→main", "x -> main", "y => main", "a --> main" all become "… > main".
	s := low
	for _, arrow := range []string{"-->", "->", "=>", "→", "⟶", "➜"} {
		s = strings.ReplaceAll(s, arrow, " > ")
	}
	// Remaining separators → space; collapse runs (so "team-phase1 > main" normalizes).
	s = strings.NewReplacer("-", " ", "_", " ", "\t", " ").Replace(s)
	for strings.Contains(s, "  ") {
		s = strings.ReplaceAll(s, "  ", " ")
	}
	// main/master as the TARGET: right of an arrow (" > "), or after "to"/"into".
	// The word-boundary check after the target avoids "mainframe"/"master-fix" match.
	if hasDirectedTarget(s, []string{" > ", " to ", " into "}, []string{"main", "master"}) {
		return true
	}
	// Chinese merge-to-main: target main/master after 到 / 合 (合并到 main / 合 main /
	// 到 main). Direction is unambiguous in these phrasings.
	for _, p := range []string{"合并到 main", "合并到main", "合 main", "合main", "到 main", "到main", "合并到 master", "到 master"} {
		if strings.Contains(low, p) {
			return true
		}
	}
	return false
}

// hasDirectedTarget reports whether s (already lowercased, arrows → " > ", single
// spaces) contains any direction token immediately followed by a target word at a
// word boundary — e.g. " > main", " to master", " into main". Space-padding both
// ends makes the leading direction boundary uniform; isWordByte enforces the
// trailing boundary so "> mainframe" / "to maintainer" do not over-match.
func hasDirectedTarget(s string, directions, targets []string) bool {
	padded := " " + s + " "
	for _, d := range directions {
		for _, tgt := range targets {
			needle := d + tgt
			from := 0
			for {
				j := strings.Index(padded[from:], needle)
				if j < 0 {
					break
				}
				after := padded[from+j+len(needle):]
				if after == "" || !isWordByte(after[0]) {
					return true
				}
				from += j + 1
			}
		}
	}
	return false
}

// isWordByte reports whether b continues a lowercase-alphanumeric word (boundary test).
func isWordByte(b byte) bool {
	return (b >= 'a' && b <= 'z') || (b >= '0' && b <= '9')
}
