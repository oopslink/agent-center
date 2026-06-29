package projectmanager

import (
	"testing"
	"time"
)

func TestNormalizeCapabilities(t *testing.T) {
	cases := []struct {
		name string
		in   []string
		want []string
	}{
		{"nil → nil", nil, nil},
		{"empty → nil", []string{}, nil},
		{"all blank → nil", []string{"  ", "\t"}, nil},
		{"trim + lowercase", []string{"  Go ", "RUST"}, []string{"go", "rust"}},
		{"dedup case-insensitive, first-seen order", []string{"Go", "go", "GO", "rust"}, []string{"go", "rust"}},
		{"drops blanks among valid", []string{"go", "", "  ", "rust"}, []string{"go", "rust"}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := NormalizeCapabilities(c.in)
			if len(got) != len(c.want) {
				t.Fatalf("NormalizeCapabilities(%v) = %v, want %v", c.in, got, c.want)
			}
			for i := range c.want {
				if got[i] != c.want[i] {
					t.Fatalf("entry %d = %q, want %q", i, got[i], c.want[i])
				}
			}
		})
	}
}

func TestTask_RequiredCapabilities(t *testing.T) {
	now := time.Unix(1_700_000_000, 0).UTC()
	// Default: a task created without capabilities is unrestricted (nil).
	tk, err := NewTask(NewTaskInput{ID: "t1", ProjectID: "p1", Title: "x", CreatedBy: "user:a", CreatedAt: now})
	if err != nil {
		t.Fatal(err)
	}
	if tk.RequiredCapabilities() != nil {
		t.Fatalf("default required_capabilities = %v, want nil", tk.RequiredCapabilities())
	}

	// NewTask canonicalizes the input.
	tk2, err := NewTask(NewTaskInput{
		ID: "t2", ProjectID: "p1", Title: "x", CreatedBy: "user:a", CreatedAt: now,
		RequiredCapabilities: []string{" Go ", "go", "RUST"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if got := tk2.RequiredCapabilities(); len(got) != 2 || got[0] != "go" || got[1] != "rust" {
		t.Fatalf("NewTask caps = %v, want [go rust]", got)
	}

	// SetRequiredCapabilities replaces + canonicalizes; empty clears.
	if err := tk2.SetRequiredCapabilities([]string{"Python", "python"}, now); err != nil {
		t.Fatal(err)
	}
	if got := tk2.RequiredCapabilities(); len(got) != 1 || got[0] != "python" {
		t.Fatalf("after set = %v, want [python]", got)
	}
	if err := tk2.SetRequiredCapabilities(nil, now); err != nil {
		t.Fatal(err)
	}
	if tk2.RequiredCapabilities() != nil {
		t.Fatalf("after clear = %v, want nil", tk2.RequiredCapabilities())
	}

	// Defensive copy: mutating the returned slice must not affect the task.
	_ = tk2.SetRequiredCapabilities([]string{"go"}, now)
	got := tk2.RequiredCapabilities()
	got[0] = "MUTATED"
	if tk2.RequiredCapabilities()[0] != "go" {
		t.Fatal("RequiredCapabilities must return a defensive copy")
	}
}

// An archived task rejects capability edits (read-only invariant).
func TestSetRequiredCapabilities_ArchivedRejected(t *testing.T) {
	now := time.Unix(1_700_000_000, 0).UTC()
	archived, err := RehydrateTask(RehydrateTaskInput{
		ID: "t1", ProjectID: "p1", Title: "x", Status: TaskOpen, CreatedBy: "user:a",
		CreatedAt: now, UpdatedAt: now, Version: 1, ArchivedAt: &now, ArchivedBy: "user:a",
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := archived.SetRequiredCapabilities([]string{"go"}, now); err != ErrTaskArchived {
		t.Fatalf("SetRequiredCapabilities on archived = %v, want ErrTaskArchived", err)
	}
}
