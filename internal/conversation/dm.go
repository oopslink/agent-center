package conversation

import (
	"sort"
	"strings"
)

// DM dedup key (T288). A DM is uniquely identified by its participant SET within
// an org: one human↔agent (or any participant pair) must map to exactly ONE DM,
// reused on every entry point (Start-a-DM, reminder delivery, agent open) instead
// of opening a duplicate. DMKey computes the canonical, order-independent key over
// a DM's ACTIVE participants so two creations of the same pair collide.
//
// The key is the sorted, unit-separator-joined active participant identity refs.
// `\x1f` (US, ASCII 31) can't appear in an IdentityRef, so it is an unambiguous
// join delimiter. It is stored on the conversation row (dm_key) and enforced by a
// partial UNIQUE index (organization_id, dm_key) WHERE kind='dm' — the regression
// guard that makes concurrent/retried creates idempotent at the DB layer.
const dmKeySep = "\x1f"

// DMKey returns the canonical dedup key for the given participant set, or "" when
// there are no active participants (caller stores NULL — no DM-dedup constraint).
// It is order-independent and ignores left (inactive) participants, so the key is
// stable for a 1:1 DM regardless of the order the two sides were listed.
func DMKey(participants []ParticipantElement) string {
	ids := make([]string, 0, len(participants))
	for _, p := range participants {
		if p.IsActive() {
			ids = append(ids, string(p.IdentityID))
		}
	}
	if len(ids) == 0 {
		return ""
	}
	sort.Strings(ids)
	return strings.Join(ids, dmKeySep)
}
