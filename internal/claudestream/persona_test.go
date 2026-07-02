package claudestream

import "testing"

func TestPersonaDescriptionSection(t *testing.T) {
	cases := []struct {
		name string
		in   string
		want string
	}{
		{"empty", "", ""},
		{"blank", "   \n\t ", ""},
		{"simple", "A helpful coder.", "== About you ==\nA helpful coder."},
		{"trims surrounding ws", "  hi there  ", "== About you ==\nhi there"},
		{"multiline preserved", "line1\nline2", "== About you ==\nline1\nline2"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := PersonaDescriptionSection(c.in); got != c.want {
				t.Fatalf("PersonaDescriptionSection(%q) = %q, want %q", c.in, got, c.want)
			}
		})
	}
}

func TestComposeExtraSystemPrompt(t *testing.T) {
	cases := []struct {
		name        string
		description string
		memory      string
		want        string
	}{
		{"both empty", "", "", ""},
		{"both blank", "  ", "\n ", ""},
		{"description only", "I am dev1.", "", "== About you ==\nI am dev1."},
		{"memory only", "", "== Memory ==\nremember X", "== Memory ==\nremember X"},
		{
			// persona段 FIRST, memory second, separated by a blank line.
			name:        "both present → persona first",
			description: "I am dev1.",
			memory:      "== Memory ==\nremember X",
			want:        "== About you ==\nI am dev1.\n\n== Memory ==\nremember X",
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := ComposeExtraSystemPrompt(c.description, c.memory); got != c.want {
				t.Fatalf("ComposeExtraSystemPrompt(%q,%q) = %q, want %q", c.description, c.memory, got, c.want)
			}
		})
	}
}
