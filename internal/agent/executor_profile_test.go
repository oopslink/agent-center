package agent

import (
	"errors"
	"testing"
)

func TestNormalizeAllowedExecutors(t *testing.T) {
	t.Run("validates, trims, dedups, preserves order", func(t *testing.T) {
		in := []ExecutorProfile{
			{CLI: "claude-code", Model: " opus "}, // trimmed → "opus"
			{CLI: "codex", Model: "gpt-5-codex"},
			{CLI: "claude-code", Model: "opus"}, // exact dup of the trimmed first → dropped
		}
		got, err := NormalizeAllowedExecutors(in)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		want := []ExecutorProfile{
			{CLI: "claude-code", Model: "opus"},
			{CLI: "codex", Model: "gpt-5-codex"},
		}
		if len(got) != len(want) {
			t.Fatalf("got %v, want %v", got, want)
		}
		for i := range want {
			if got[i] != want[i] {
				t.Fatalf("entry %d = %v, want %v", i, got[i], want[i])
			}
		}
	})

	t.Run("nil/empty → nil", func(t *testing.T) {
		if got, err := NormalizeAllowedExecutors(nil); err != nil || got != nil {
			t.Fatalf("nil input → (%v, %v), want (nil, nil)", got, err)
		}
	})

	t.Run("unsupported cli → ErrInvalidExecutorProfile", func(t *testing.T) {
		_, err := NormalizeAllowedExecutors([]ExecutorProfile{{CLI: "opencode", Model: "m"}})
		if !errors.Is(err, ErrInvalidExecutorProfile) {
			t.Fatalf("err = %v, want ErrInvalidExecutorProfile", err)
		}
	})

	t.Run("empty cli → ErrInvalidExecutorProfile", func(t *testing.T) {
		_, err := NormalizeAllowedExecutors([]ExecutorProfile{{CLI: "", Model: "m"}})
		if !errors.Is(err, ErrInvalidExecutorProfile) {
			t.Fatalf("err = %v, want ErrInvalidExecutorProfile", err)
		}
	})

	t.Run("empty/whitespace model → ErrInvalidExecutorProfile", func(t *testing.T) {
		_, err := NormalizeAllowedExecutors([]ExecutorProfile{{CLI: "codex", Model: "   "}})
		if !errors.Is(err, ErrInvalidExecutorProfile) {
			t.Fatalf("err = %v, want ErrInvalidExecutorProfile", err)
		}
	})
}

func TestExecutorsFromModels(t *testing.T) {
	t.Run("pairs each model with the given cli", func(t *testing.T) {
		got := ExecutorsFromModels([]string{"a", "b"}, "codex")
		want := []ExecutorProfile{{CLI: "codex", Model: "a"}, {CLI: "codex", Model: "b"}}
		if len(got) != 2 || got[0] != want[0] || got[1] != want[1] {
			t.Fatalf("got %v, want %v", got, want)
		}
	})
	t.Run("empty cli defaults to claude-code (0085 backfill rule)", func(t *testing.T) {
		got := ExecutorsFromModels([]string{"a"}, "")
		if len(got) != 1 || got[0].CLI != DefaultExecutorCLI {
			t.Fatalf("got %v, want cli=%s", got, DefaultExecutorCLI)
		}
	})
	t.Run("no models → nil", func(t *testing.T) {
		if got := ExecutorsFromModels(nil, "codex"); got != nil {
			t.Fatalf("got %v, want nil", got)
		}
	})
}

func TestModelsOf(t *testing.T) {
	got := ModelsOf([]ExecutorProfile{
		{CLI: "claude-code", Model: "m1"},
		{CLI: "codex", Model: "m1"}, // same model, different cli → deduped to one model
		{CLI: "codex", Model: "m2"},
	})
	want := []string{"m1", "m2"}
	if len(got) != len(want) || got[0] != want[0] || got[1] != want[1] {
		t.Fatalf("ModelsOf = %v, want %v", got, want)
	}
	if ModelsOf(nil) != nil {
		t.Fatal("ModelsOf(nil) should be nil")
	}
}
