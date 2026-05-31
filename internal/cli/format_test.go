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
// NormalizeFormat / IsValidFormat
// =============================================================================

func TestNormalizeFormat_Canonical(t *testing.T) {
	cases := map[string]string{
		"":       FormatTable,
		"table":  FormatTable,
		"TABLE":  FormatTable,
		" table": FormatTable,
		"human":  FormatTable, // backwards-compat alias
		"HUMAN":  FormatTable,
		"json":   FormatJSON,
		"JSON":   FormatJSON,
		"text":   FormatText,
		"TEXT":   FormatText,
	}
	for in, want := range cases {
		got, ok := NormalizeFormat(in)
		if !ok {
			t.Errorf("NormalizeFormat(%q): ok=false", in)
			continue
		}
		if got != want {
			t.Errorf("NormalizeFormat(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestNormalizeFormat_Invalid(t *testing.T) {
	for _, in := range []string{"yaml", "csv", "weird", "table2"} {
		if _, ok := NormalizeFormat(in); ok {
			t.Errorf("NormalizeFormat(%q) accepted unknown value", in)
		}
	}
}

func TestIsValidFormat(t *testing.T) {
	if !IsValidFormat("table") || !IsValidFormat("json") || !IsValidFormat("text") || !IsValidFormat("human") {
		t.Fatal("all canonical formats should validate")
	}
	if IsValidFormat("yaml") {
		t.Fatal("yaml should not validate")
	}
}

// =============================================================================
// validateRouterFormatFlag — in-place normalisation + error gate
// =============================================================================

func TestValidateRouterFormatFlag_HumanCollapses(t *testing.T) {
	fs := flag.NewFlagSet("t", flag.ContinueOnError)
	f := fs.String("format", "human", "")
	var errw bytes.Buffer
	if !validateRouterFormatFlag(fs, &errw) {
		t.Fatalf("expected ok; err=%s", errw.String())
	}
	if *f != FormatTable {
		t.Fatalf("expected human collapsed to table; got %q", *f)
	}
}

func TestValidateRouterFormatFlag_NoFormatFlag(t *testing.T) {
	fs := flag.NewFlagSet("t", flag.ContinueOnError)
	var errw bytes.Buffer
	if !validateRouterFormatFlag(fs, &errw) {
		t.Fatal()
	}
}

func TestValidateRouterFormatFlag_Invalid(t *testing.T) {
	fs := flag.NewFlagSet("t", flag.ContinueOnError)
	_ = fs.String("format", "yaml", "")
	var errw bytes.Buffer
	if validateRouterFormatFlag(fs, &errw) {
		t.Fatal("expected reject")
	}
	if !strings.Contains(errw.String(), "invalid --format") {
		t.Fatalf("err missing message: %s", errw.String())
	}
}

func TestValidateRouterFormatFlag_PreservesJSON(t *testing.T) {
	fs := flag.NewFlagSet("t", flag.ContinueOnError)
	f := fs.String("format", "json", "")
	var errw bytes.Buffer
	if !validateRouterFormatFlag(fs, &errw) {
		t.Fatal()
	}
	if *f != "json" {
		t.Fatalf("json should stay json; got %q", *f)
	}
}

// =============================================================================
// End-to-end: --format=text on list handlers prints one ID per line.
// =============================================================================

func TestCLI_ChannelList_TextFormat(t *testing.T) {
	app := newTestApp(t)
	_, _, _ = runOn(t, app, "channel", "create", []string{"--name=t1"})
	_, _, _ = runOn(t, app, "channel", "create", []string{"--name=t2"})
	out, _, code := runOn(t, app, "channel", "list", []string{"--format=text"})
	if code != ExitOK {
		t.Fatalf("code %d", code)
	}
	lines := strings.Split(strings.TrimSpace(out), "\n")
	if len(lines) != 2 || lines[0] != "t1" || lines[1] != "t2" {
		t.Fatalf("expected t1\\nt2; got %q", out)
	}
}

func TestCLI_AgentList_TextFormat(t *testing.T) {
	app := newTestApp(t)
	_, _, _ = runOn(t, app, "agent", "create", []string{
		"--name=a1", "--agent-cli=claudecode", "--worker=w-1",
	})
	out, _, code := runOn(t, app, "agent", "list", []string{"--format=text"})
	if code != ExitOK {
		t.Fatalf("code %d", code)
	}
	if strings.TrimSpace(out) == "" {
		t.Fatalf("expected non-empty id list; got %q", out)
	}
	if strings.Contains(out, "STATE") || strings.Contains(out, "WORKER") {
		t.Fatalf("text output should be ids only; got %q", out)
	}
}

func TestCLI_SecretList_TextFormat(t *testing.T) {
	app := newAppWithSecret(t)
	_, _, _ = runOn(t, app, "secret", "create", []string{
		"--name=s1", "--value-file=" + writeTempFile(t, "v"),
	})
	out, _, code := runOn(t, app, "secret", "list", []string{"--format=text"})
	if code != ExitOK {
		t.Fatalf("code %d", code)
	}
	if strings.TrimSpace(out) == "" {
		t.Fatalf("expected id list; got %q", out)
	}
}

// =============================================================================
// End-to-end: --format=human still works (backwards compat alias).
// =============================================================================

func TestCLI_ChannelList_HumanAlias(t *testing.T) {
	app := newTestApp(t)
	_, _, _ = runOn(t, app, "channel", "create", []string{"--name=ha"})
	out, _, code := runOn(t, app, "channel", "list", []string{"--format=human"})
	if code != ExitOK {
		t.Fatalf("code %d", code)
	}
	// human aliases to table, which prints a header.
	if !strings.Contains(out, "NAME") || !strings.Contains(out, "STATUS") {
		t.Fatalf("human/table should print header; got %q", out)
	}
}

// =============================================================================
// End-to-end: --format=weird is rejected with ExitUsage at the router.
// =============================================================================

func TestCLI_FormatRejected_AtRouter(t *testing.T) {
	// Synthetic router with a single leaf command that declares a --format
	// flag and would otherwise succeed. The router gate must reject
	// --format=weird before invoking the handler.
	var handlerCalled bool
	cmd := &Command{
		Name: "demo",
		Flags: func(fs *flag.FlagSet) Handler {
			_ = fs.String("format", FormatTable, formatFlagHelp())
			return func(_ context.Context, _ []string, _, _ io.Writer) ExitCode {
				handlerCalled = true
				return ExitOK
			}
		},
	}
	var outBuf, errBuf bytes.Buffer
	router := &Router{Root: cmd, Out: &outBuf, Err: &errBuf}
	code := router.Run(context.Background(), []string{"--format=weird"})
	if code != ExitUsage {
		t.Fatalf("code %d; err=%s", code, errBuf.String())
	}
	if handlerCalled {
		t.Fatal("handler should not run for invalid --format")
	}
	if !strings.Contains(errBuf.String(), "invalid --format") {
		t.Fatalf("expected invalid --format in err; got %q", errBuf.String())
	}
}
