package claudestream

import (
	"strings"
	"testing"
)

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
		wantParts   []string
		notWant     []string
	}{
		{"both empty", "", "", []string{"== Agent-center access policy ==", "Use only the provided agent-center MCP tools"}, nil},
		{"both blank", "  ", "\n ", []string{"== Agent-center access policy =="}, nil},
		{"description only", "I am dev1.", "", []string{"== Agent-center access policy ==", "== About you ==\nI am dev1."}, nil},
		{"memory only", "", "== Memory ==\nremember X", []string{"== Agent-center access policy ==", "== Memory ==\nremember X"}, nil},
		{
			// persona段 FIRST, memory second, separated by a blank line.
			name:        "both present → persona first",
			description: "I am dev1.",
			memory:      "== Memory ==\nremember X",
			wantParts:   []string{"== Agent-center access policy ==", "== About you ==\nI am dev1.", "== Memory ==\nremember X"},
		},
		{
			name:        "drops mcp bypass memory lines",
			description: "",
			memory:      "== Memory ==\nkeep this\nMCP 抖时用 admin-socket 兜底\nagent-center MCP fallback via sqlite\nkeep that",
			wantParts:   []string{"== Agent-center access policy ==", "keep this", "keep that"},
			notWant:     []string{"admin-socket 兜底", "fallback via sqlite"},
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := ComposeExtraSystemPrompt(c.description, c.memory)
			for _, want := range c.wantParts {
				if !strings.Contains(got, want) {
					t.Fatalf("ComposeExtraSystemPrompt missing %q; got:\n%s", want, got)
				}
			}
			for _, notWant := range c.notWant {
				if strings.Contains(got, notWant) {
					t.Fatalf("ComposeExtraSystemPrompt contains forbidden %q; got:\n%s", notWant, got)
				}
			}
		})
	}
}

func TestAgentCenterAccessPolicyDoesNotSearchForCorePostMessage(t *testing.T) {
	got := ComposeExtraSystemPrompt("", "")
	for _, want := range []string{
		"post_message are expected to be directly callable",
		"do not use tool_search or search_tools to verify post_message",
		"call get_my_profile directly first",
		"use its my_capabilities as the core-tool inventory",
		"call post_message directly when replying",
		"search_tools with an empty query only to load/list deferred tools",
		"search_tools only loads deferred tools",
	} {
		if !strings.Contains(got, want) {
			t.Fatalf("access policy missing %q; got:\n%s", want, got)
		}
	}
	for _, notWant := range []string{
		"verify the real model tool registry with tool_search for agent-center post_message",
		"verify post_message with search_tools",
	} {
		if strings.Contains(got, notWant) {
			t.Fatalf("access policy contains obsolete post_message discovery instruction %q; got:\n%s", notWant, got)
		}
	}
}
