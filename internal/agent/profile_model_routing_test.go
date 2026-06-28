package agent

import "testing"

// TestEffectiveMaxConcurrentTasks covers the F3 default-3 helper (design §5):
// unset/zero/negative all resolve to DefaultMaxConcurrentTasks; a positive value
// is returned as-is.
func TestEffectiveMaxConcurrentTasks(t *testing.T) {
	cases := []struct {
		name string
		in   int
		want int
	}{
		{"zero defaults to 3", 0, DefaultMaxConcurrentTasks},
		{"negative defaults to 3", -1, DefaultMaxConcurrentTasks},
		{"positive kept", 5, 5},
		{"one kept", 1, 1},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			p := Profile{MaxConcurrentTasks: c.in}
			if got := p.EffectiveMaxConcurrentTasks(); got != c.want {
				t.Fatalf("EffectiveMaxConcurrentTasks(%d) = %d, want %d", c.in, got, c.want)
			}
		})
	}
}
