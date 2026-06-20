package conversation

import "testing"

// ResolveOwnerContext must map each pm:// owner scheme to an id-anchored
// OwnerContext carrying the right Kind/Label/IDField and the parsed id.
func TestResolveOwnerContext_AnchoredOwners(t *testing.T) {
	cases := []struct {
		ownerRef string
		kind     OwnerKind
		label    string
		idField  string
		id       string
	}{
		{"pm://plans/plan-abc", OwnerKindPlan, "Plan", "plan_id", "plan-abc"},
		{"pm://issues/issue-7", OwnerKindIssue, "Issue", "issue_id", "issue-7"},
		{"pm://tasks/task-9", OwnerKindTask, "Task", "task_id", "task-9"},
		{"pm://projects/project-1", OwnerKindProject, "Project", "project_id", "project-1"},
	}
	for _, c := range cases {
		oc, ok := ResolveOwnerContext(c.ownerRef)
		if !ok {
			t.Fatalf("%s: expected ok", c.ownerRef)
		}
		if oc.Kind != c.kind || oc.Label != c.label || oc.IDField != c.idField || oc.ID != c.id {
			t.Fatalf("%s: got %+v, want kind=%s label=%s idField=%s id=%s", c.ownerRef, oc, c.kind, c.label, c.idField, c.id)
		}
		if !oc.Anchored {
			t.Fatalf("%s: pm:// owner must be Anchored", c.ownerRef)
		}
		if oc.Name != "" {
			t.Fatalf("%s: resolver must leave Name empty (env fills it), got %q", c.ownerRef, oc.Name)
		}
	}
}

// A channel (id://organizations/{org}) resolves but is NOT id-anchored — it
// keeps its own [Channel #name] framing.
func TestResolveOwnerContext_ChannelNotAnchored(t *testing.T) {
	oc, ok := ResolveOwnerContext("id://organizations/org-1")
	if !ok {
		t.Fatalf("channel owner_ref must resolve")
	}
	if oc.Kind != OwnerKindChannel || oc.Anchored {
		t.Fatalf("channel must be Kind=channel, Anchored=false, got %+v", oc)
	}
	if oc.ID != "org-1" {
		t.Fatalf("channel id must parse, got %q", oc.ID)
	}
}

// Empty (dm) and unknown schemes do not resolve — the caller falls back.
func TestResolveOwnerContext_EmptyAndUnknown(t *testing.T) {
	if _, ok := ResolveOwnerContext(""); ok {
		t.Fatalf("empty owner_ref must not resolve")
	}
	if _, ok := ResolveOwnerContext("   "); ok {
		t.Fatalf("whitespace owner_ref must not resolve")
	}
	if _, ok := ResolveOwnerContext("pm://unknown/x"); ok {
		t.Fatalf("unknown scheme must not resolve")
	}
	// A bare scheme with no id is not well-formed → must not resolve.
	if _, ok := ResolveOwnerContext("pm://tasks/"); ok {
		t.Fatalf("scheme without an id must not resolve")
	}
}

// OQ4 exhaustiveness guard: every scheme OwnerRef.WellFormed() recognises MUST
// have an entry in the OwnerContext resolution table. This is what catches a
// future "added a 6th owner scheme but forgot to teach the brief about it" —
// remove any entry from ownerContextRegistry and this test goes red.
func TestOwnerContext_AllWellFormedSchemesRegistered(t *testing.T) {
	for _, scheme := range knownOwnerSchemes {
		if _, ok := ownerContextRegistry[scheme]; !ok {
			t.Fatalf("scheme %q is WellFormed but not registered in ownerContextRegistry", scheme)
		}
		// And the registry entry must round-trip through ResolveOwnerContext for a
		// concrete ref of that scheme.
		if _, ok := ResolveOwnerContext(scheme + "x"); !ok {
			t.Fatalf("scheme %q does not resolve via ResolveOwnerContext", scheme)
		}
	}
}

// Reverse drift guard: every registered scheme must also be WellFormed, so the
// two lists can't silently diverge in the other direction.
func TestOwnerContext_RegistryEntriesAreWellFormed(t *testing.T) {
	for scheme := range ownerContextRegistry {
		if !OwnerRef(scheme + "x").WellFormed() {
			t.Fatalf("registered scheme %q is not accepted by WellFormed", scheme)
		}
	}
}
