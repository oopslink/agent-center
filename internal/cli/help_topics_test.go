package cli

import (
	"bytes"
	"context"
	"flag"
	"io"
	"strings"
	"testing"
)

// =============================================================================
// Topic registry — body / summary / order
// =============================================================================

func TestHelpTopicBody_KnownTopics(t *testing.T) {
	for _, topic := range helpTopicOrder() {
		body, ok := helpTopicBody(topic)
		if !ok {
			t.Errorf("topic %q registered in helpTopicOrder but has no body", topic)
			continue
		}
		if !strings.HasPrefix(body, topic) {
			t.Errorf("topic %q body should start with its name; got %q", topic, body[:min(40, len(body))])
		}
		if helpTopicSummary(topic) == "" {
			t.Errorf("topic %q missing summary", topic)
		}
	}
}

func TestHelpTopicBody_Unknown(t *testing.T) {
	if _, ok := helpTopicBody("nope"); ok {
		t.Fatal()
	}
	if helpTopicSummary("nope") != "" {
		t.Fatal()
	}
}

func TestHelpTopic_FormatMentionsThreeValues(t *testing.T) {
	body, _ := helpTopicBody("format")
	for _, v := range []string{"table", "json", "text"} {
		if !strings.Contains(body, v) {
			t.Errorf("format topic missing %q", v)
		}
	}
}

func TestHelpTopic_IdentityMentionsThreeKinds(t *testing.T) {
	body, _ := helpTopicBody("identity")
	for _, k := range []string{"user:", "agent:", "system:"} {
		if !strings.Contains(body, k) {
			t.Errorf("identity topic missing %q", k)
		}
	}
}

// =============================================================================
// Group ordering + bucketing
// =============================================================================

func TestGroupOrder_Stable(t *testing.T) {
	want := []string{"Help & info", "Resources", "Runtime", "Observability", "Admin"}
	got := groupOrder()
	if len(got) != len(want) {
		t.Fatalf("got %v", got)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("[%d] want %s got %s", i, want[i], got[i])
		}
	}
}

func TestGroupTopLevel_DefaultsToOther(t *testing.T) {
	cmds := []*Command{
		{Name: "a", Group: "Resources"},
		{Name: "b"}, // empty group → "Other"
	}
	got := groupTopLevel(cmds)
	if len(got["Resources"]) != 1 || got["Resources"][0].Name != "a" {
		t.Fatalf("Resources bucket: %v", got["Resources"])
	}
	if len(got["Other"]) != 1 || got["Other"][0].Name != "b" {
		t.Fatalf("Other bucket: %v", got["Other"])
	}
}

func TestAssignTopLevelMeta_FillsBlank(t *testing.T) {
	root := &Command{
		Subcommands: []*Command{
			{Name: "channel"}, // empty Group + Summary
			{Name: "version", Summary: "already set", Group: "Help & info"},
			{Name: "weird-no-meta"},
		},
	}
	assignTopLevelMeta(root)
	channel := findSubcommand(root, "channel")
	if channel.Group != "Resources" || channel.Summary == "" {
		t.Fatalf("channel meta not assigned: %+v", channel)
	}
	version := findSubcommand(root, "version")
	if version.Summary != "already set" {
		t.Fatalf("version summary clobbered: %s", version.Summary)
	}
	weird := findSubcommand(root, "weird-no-meta")
	if weird.Group != "" {
		t.Fatalf("weird should keep empty group; got %q", weird.Group)
	}
}

// =============================================================================
// HelpCommand — `help` / `help <command>` / `help <topic>` / unknown
// =============================================================================

func TestRouter_RootHelp_GroupedSections(t *testing.T) {
	r := buildHelpSandboxRouter()
	var out, errw bytes.Buffer
	r.Out, r.Err = &out, &errw
	r.printRootHelp()
	s := out.String()
	for _, want := range []string{
		"Help & info:",
		"Resources:",
		"Topics:",
		"format ",
		"identity ",
		"exit-codes ",
		"channel ",
		"agent ",
	} {
		if !strings.Contains(s, want) {
			t.Errorf("root help missing %q\n--full output--\n%s", want, s)
		}
	}
}

func TestRouter_HelpNoArgs_UnifiesWithRoot(t *testing.T) {
	r := buildHelpSandboxRouter()
	var out, errw bytes.Buffer
	r.Out, r.Err = &out, &errw
	code := r.runHelp(nil, &out, &errw)
	if code != ExitOK {
		t.Fatal(code)
	}
	// Compare with direct printRootHelp.
	var out2 bytes.Buffer
	r.Out = &out2
	r.printRootHelp()
	if out.String() != out2.String() {
		t.Fatalf("`help` (no args) should equal printRootHelp\n--help--\n%s\n--root--\n%s",
			out.String(), out2.String())
	}
}

func TestRouter_HelpCommand_DispatchToSubtree(t *testing.T) {
	r := buildHelpSandboxRouter()
	var out, errw bytes.Buffer
	r.Out, r.Err = &out, &errw
	code := r.runHelp([]string{"channel"}, &out, &errw)
	if code != ExitOK {
		t.Fatalf("code %d; err=%s", code, errw.String())
	}
	if !strings.Contains(out.String(), "channel — ") {
		t.Fatalf("expected channel header; got %q", out.String())
	}
	if !strings.Contains(out.String(), "Subcommands:") {
		t.Fatalf("expected Subcommands listing; got %q", out.String())
	}
}

func TestRouter_HelpCommand_DrillIntoLeaf(t *testing.T) {
	r := buildHelpSandboxRouter()
	var out, errw bytes.Buffer
	r.Out, r.Err = &out, &errw
	code := r.runHelp([]string{"channel", "create"}, &out, &errw)
	if code != ExitOK {
		t.Fatal(code)
	}
	s := out.String()
	if !strings.Contains(s, "Examples:") {
		t.Fatalf("expected Examples: section; got %q", s)
	}
	if !strings.Contains(s, "--format=json") {
		t.Fatalf("expected --format=json example; got %q", s)
	}
}

func TestRouter_HelpCommand_TopicBody(t *testing.T) {
	r := buildHelpSandboxRouter()
	var out, errw bytes.Buffer
	r.Out, r.Err = &out, &errw
	code := r.runHelp([]string{"format"}, &out, &errw)
	if code != ExitOK {
		t.Fatal(code)
	}
	if !strings.Contains(out.String(), "table") || !strings.Contains(out.String(), "json") {
		t.Fatalf("expected format topic body; got %q", out.String())
	}
}

func TestRouter_HelpCommand_UnknownTarget(t *testing.T) {
	r := buildHelpSandboxRouter()
	var out, errw bytes.Buffer
	r.Out, r.Err = &out, &errw
	code := r.runHelp([]string{"definitely-not-a-thing"}, &out, &errw)
	if code != ExitUsage {
		t.Fatalf("code %d; err=%s", code, errw.String())
	}
	if !strings.Contains(errw.String(), "unknown help target") {
		t.Fatalf("expected usage error; got %q", errw.String())
	}
}

func TestRouter_HelpCommand_IsRegisteredAtRoot(t *testing.T) {
	r := buildHelpSandboxRouter()
	if findSubcommand(r.Root, "help") == nil {
		t.Fatal("help command should be at root")
	}
}

// =============================================================================
// Leaf --help renders Examples + Flags
// =============================================================================

func TestRouter_PrintNodeHelp_LeafShowsFlagsAndExamples(t *testing.T) {
	r := buildHelpSandboxRouter()
	leaf := &Command{
		Name:    "demo",
		Summary: "Demo leaf",
		Flags: func(fs *flag.FlagSet) Handler {
			_ = fs.String("name", "", "the name")
			_ = fs.String("format", FormatTable, formatFlagHelp())
			return func(_ context.Context, _ []string, _, _ io.Writer) ExitCode { return ExitOK }
		},
		Examples: []string{"agent-center demo --name=x", "agent-center demo --format=json"},
	}
	var out bytes.Buffer
	r.Out = &out
	r.printNodeHelp(leaf)
	for _, w := range []string{"Flags:", "Examples:", "--name", "--format", "agent-center demo --format=json"} {
		if !strings.Contains(out.String(), w) {
			t.Errorf("leaf help missing %q\n%s", w, out.String())
		}
	}
}

// =============================================================================
// Helpers
// =============================================================================

// buildHelpSandboxRouter constructs a router with a minimal subset of
// commands sufficient to exercise help rendering without touching a real
// DB. We mirror the top-level shape that BuildRouter produces.
func buildHelpSandboxRouter() *Router {
	r := NewRouter("agent-center")
	r.Root.Summary = "agent-center — single binary CLI"
	// Resources group: channel with create/list leaves carrying Examples.
	_ = r.Add(nil, &Command{
		Name: "channel",
		Subcommands: []*Command{
			{
				Name: "create", Summary: "Create channel",
				Examples: []string{`agent-center channel create --name=alpha`, `agent-center channel create --name=ops --format=json`},
				Flags: func(fs *flag.FlagSet) Handler {
					_ = fs.String("name", "", "")
					_ = fs.String("format", FormatTable, formatFlagHelp())
					return func(_ context.Context, _ []string, _, _ io.Writer) ExitCode { return ExitOK }
				},
			},
			{Name: "list", Summary: "List channels",
				Flags: func(fs *flag.FlagSet) Handler {
					_ = fs.String("format", FormatTable, formatFlagHelp())
					return func(_ context.Context, _ []string, _, _ io.Writer) ExitCode { return ExitOK }
				}},
		},
	})
	// Resources: agent group (just summary, no leaves needed).
	_ = r.Add(nil, &Command{Name: "agent", Subcommands: []*Command{{Name: "list"}}})
	// Runtime / Admin / Observability sample.
	_ = r.Add(nil, &Command{Name: "server", Run: func(_ context.Context, _ []string, _, _ io.Writer) ExitCode { return ExitOK }})
	_ = r.Add(nil, &Command{Name: "migrate", Run: func(_ context.Context, _ []string, _, _ io.Writer) ExitCode { return ExitOK }})
	_ = r.Add(nil, &Command{Name: "inspect", Subcommands: []*Command{{Name: "task"}}})
	_ = r.Add(nil, r.HelpCommand())
	assignTopLevelMeta(r.Root)
	return r
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}
