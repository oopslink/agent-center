package projectmanager

import (
	"strings"
	"testing"
	"time"
)

// --- SetTags validation (v2.8.1 edit-task #278) -----------------------------

func TestTask_SetTags_Valid(t *testing.T) {
	tk := newTask(t)
	if err := tk.SetTags([]string{" a ", "b", "a"}, t0.Add(time.Hour)); err != nil {
		t.Fatalf("SetTags: %v", err)
	}
	got := tk.Tags()
	// trimmed + deduped (the second "a" is an exact dup of the trimmed first).
	if len(got) != 2 || got[0] != "a" || got[1] != "b" {
		t.Fatalf("tags = %v, want [a b]", got)
	}
}

func TestTask_SetTags_TooMany(t *testing.T) {
	tk := newTask(t)
	many := make([]string, 11)
	for i := range many {
		many[i] = string(rune('a' + i))
	}
	if err := tk.SetTags(many, t0); err == nil {
		t.Fatalf("expected error for >10 tags")
	}
	if tk.Tags() != nil {
		t.Fatalf("tags must stay unset on reject, got %v", tk.Tags())
	}
}

func TestTask_SetTags_TooLong(t *testing.T) {
	tk := newTask(t)
	if err := tk.SetTags([]string{strings.Repeat("x", 17)}, t0); err == nil {
		t.Fatalf("expected error for 17-char tag")
	}
	// 16 is the boundary and must be accepted.
	if err := tk.SetTags([]string{strings.Repeat("x", 16)}, t0); err != nil {
		t.Fatalf("16-char tag should be valid: %v", err)
	}
	// The 16-limit is RUNE-based (CJK-correct): 16 Chinese chars = 48 bytes but
	// 16 runes → accepted; 17 → rejected. Critical for our (Chinese) team — a
	// byte-count would wrongly reject 16-char CJK tags (Tester #232 flag).
	if err := tk.SetTags([]string{strings.Repeat("中", 16)}, t0); err != nil {
		t.Fatalf("16-rune CJK tag (48 bytes) must be valid (rune-based, not byte): %v", err)
	}
	if err := tk.SetTags([]string{strings.Repeat("中", 17)}, t0); err == nil {
		t.Fatalf("expected error for 17-rune CJK tag")
	}
}

func TestTask_SetTags_EmptyRejected(t *testing.T) {
	tk := newTask(t)
	if err := tk.SetTags([]string{"ok", "   "}, t0); err == nil {
		t.Fatalf("expected error for blank tag")
	}
}

// --- statusChangedAt placement ----------------------------------------------

func TestTask_StatusChangedAt_SetOnConstruct(t *testing.T) {
	tk := newTask(t)
	if !tk.StatusChangedAt().Equal(t0) {
		t.Fatalf("statusChangedAt = %v, want createdAt %v", tk.StatusChangedAt(), t0)
	}
}

func TestTask_StatusChangedAt_UpdatesOnSetStatus(t *testing.T) {
	tk := newTask(t)
	at := t0.Add(2 * time.Hour)
	if err := tk.SetStatus(TaskRunning, at); err != nil {
		t.Fatal(err)
	}
	if !tk.StatusChangedAt().Equal(at) {
		t.Fatalf("statusChangedAt = %v, want %v", tk.StatusChangedAt(), at)
	}
}

func TestTask_StatusChangedAt_UpdatesOnTransition(t *testing.T) {
	tk := newTask(t)
	at := t0.Add(3 * time.Hour)
	if err := tk.Start(at); err != nil { // open→running via simpleTransition
		t.Fatal(err)
	}
	if !tk.StatusChangedAt().Equal(at) {
		t.Fatalf("statusChangedAt = %v, want %v (Start)", tk.StatusChangedAt(), at)
	}
}

func TestTask_StatusChangedAt_NotChangedByRenameAssignTags(t *testing.T) {
	tk := newTask(t)
	orig := tk.StatusChangedAt()
	if err := tk.Rename("new title", t0.Add(time.Hour)); err != nil {
		t.Fatal(err)
	}
	if err := tk.Assign("user:b", t0.Add(2*time.Hour)); err != nil {
		t.Fatal(err)
	}
	if err := tk.SetTags([]string{"x"}, t0.Add(3*time.Hour)); err != nil {
		t.Fatal(err)
	}
	if !tk.StatusChangedAt().Equal(orig) {
		t.Fatalf("statusChangedAt moved on metadata edit: %v != %v", tk.StatusChangedAt(), orig)
	}
}

func TestIssue_SetTags_And_StatusChangedAt(t *testing.T) {
	is, err := NewIssue(NewIssueInput{ID: "I1", ProjectID: "P1", Title: "x", CreatedBy: "user:a", CreatedAt: t0})
	if err != nil {
		t.Fatal(err)
	}
	if !is.StatusChangedAt().Equal(t0) {
		t.Fatalf("issue statusChangedAt = %v, want %v", is.StatusChangedAt(), t0)
	}
	orig := is.StatusChangedAt()
	if err := is.SetTags([]string{"a", "a", "b"}, t0.Add(time.Hour)); err != nil {
		t.Fatal(err)
	}
	if got := is.Tags(); len(got) != 2 {
		t.Fatalf("issue tags = %v, want 2 deduped", got)
	}
	if !is.StatusChangedAt().Equal(orig) {
		t.Fatalf("issue statusChangedAt moved on SetTags")
	}
	at := t0.Add(5 * time.Hour)
	if err := is.SetStatus(IssueResolved, at); err != nil {
		t.Fatal(err)
	}
	if !is.StatusChangedAt().Equal(at) {
		t.Fatalf("issue statusChangedAt = %v, want %v after SetStatus", is.StatusChangedAt(), at)
	}
}
