package cli

import (
	"bytes"
	"context"
	"flag"
	"io"
	"strings"
	"testing"
)

func TestRouter_RootHelp(t *testing.T) {
	r := NewRouter("agent-center")
	var out bytes.Buffer
	r.Out = &out
	code := r.Run(context.Background(), []string{"--help"})
	if code != ExitOK {
		t.Fatalf("code: %d", code)
	}
	if !strings.Contains(out.String(), "agent-center") {
		t.Fatalf("help missing name: %s", out.String())
	}
}

func TestRouter_AddAndDispatch(t *testing.T) {
	r := NewRouter("agent-center")
	called := false
	_ = r.Add(nil, &Command{
		Name: "ping",
		Run: func(ctx context.Context, args []string, out, err io.Writer) ExitCode {
			called = true
			return ExitOK
		},
	})
	code := r.Run(context.Background(), []string{"ping"})
	if code != ExitOK {
		t.Fatalf("code: %d", code)
	}
	if !called {
		t.Fatal("handler not called")
	}
}

func TestRouter_NestedSubcommand(t *testing.T) {
	r := NewRouter("agent-center")
	called := false
	_ = r.Add([]string{"group", "sub"}, &Command{
		Name: "leaf",
		Run: func(ctx context.Context, args []string, out, err io.Writer) ExitCode {
			called = true
			return ExitOK
		},
	})
	code := r.Run(context.Background(), []string{"group", "sub", "leaf"})
	if code != ExitOK {
		t.Fatalf("code: %d", code)
	}
	if !called {
		t.Fatal("nested handler not called")
	}
}

func TestRouter_NestedHelp(t *testing.T) {
	r := NewRouter("agent-center")
	_ = r.Add([]string{"worker"}, &Command{
		Name: "list",
		Run: func(ctx context.Context, args []string, out, err io.Writer) ExitCode {
			return ExitOK
		},
	})
	var out bytes.Buffer
	r.Out = &out
	r.Run(context.Background(), []string{"worker", "--help"})
	if !strings.Contains(out.String(), "list") {
		t.Fatalf("nested help missing subcommand: %s", out.String())
	}
}

func TestRouter_UnknownCommandRunsHelp(t *testing.T) {
	r := NewRouter("agent-center")
	var out bytes.Buffer
	r.Out = &out
	// Unknown args fall through to group help.
	code := r.Run(context.Background(), []string{"bogus"})
	if code != ExitOK {
		t.Fatalf("code: %d", code)
	}
}

func TestRouter_FlagsParsed(t *testing.T) {
	r := NewRouter("agent-center")
	var captured string
	_ = r.Add(nil, &Command{
		Name: "echo",
		Flags: func(fs *flag.FlagSet) Handler {
			s := fs.String("msg", "", "")
			return func(ctx context.Context, args []string, out, err io.Writer) ExitCode {
				captured = *s
				return ExitOK
			}
		},
	})
	r.Run(context.Background(), []string{"echo", "--msg=hi"})
	if captured != "hi" {
		t.Fatalf("got %q", captured)
	}
}

func TestRouter_FlagParseError(t *testing.T) {
	r := NewRouter("agent-center")
	var out, errBuf bytes.Buffer
	r.Out = &out
	r.Err = &errBuf
	_ = r.Add(nil, &Command{
		Name: "echo",
		Flags: func(fs *flag.FlagSet) Handler {
			fs.Int("n", 0, "")
			return func(ctx context.Context, args []string, out, err io.Writer) ExitCode {
				return ExitOK
			}
		},
	})
	code := r.Run(context.Background(), []string{"echo", "--n=notanint"})
	if code != ExitUsage {
		t.Fatalf("code: %d", code)
	}
	if !strings.Contains(errBuf.String(), "usage") {
		t.Fatalf("err: %s", errBuf.String())
	}
}

func TestRouter_DuplicateCommandError(t *testing.T) {
	r := NewRouter("agent-center")
	err := r.Add(nil, &Command{Name: "x", Run: noop})
	if err != nil {
		t.Fatal(err)
	}
	err = r.Add(nil, &Command{Name: "x", Run: noop})
	if err == nil {
		t.Fatal("expected duplicate error")
	}
}

func TestRouter_NilCommandRejected(t *testing.T) {
	r := NewRouter("agent-center")
	if err := r.Add(nil, nil); err == nil {
		t.Fatal()
	}
}

func TestRouter_NoHandlerOnLeafReturnsNotImplemented(t *testing.T) {
	r := NewRouter("agent-center")
	// A "leaf" that registers Flags but the Flags hook returns nil
	// handler — exercises the no-handler defensive path.
	_ = r.Add(nil, &Command{
		Name: "noop",
		Flags: func(fs *flag.FlagSet) Handler {
			return nil
		},
	})
	var out, errBuf bytes.Buffer
	r.Out = &out
	r.Err = &errBuf
	code := r.Run(context.Background(), []string{"noop"})
	if code != ExitNotImplemented {
		t.Fatalf("code: %d", code)
	}
}

func TestFormatJSONError(t *testing.T) {
	s := FormatJSONError("worker_not_found", `Worker "W-1" not found`)
	if !strings.Contains(s, `"worker_not_found"`) {
		t.Fatal()
	}
	if !strings.Contains(s, `Worker \"W-1\" not found`) {
		t.Fatalf("escape: %s", s)
	}
}

func TestPrintError_Human(t *testing.T) {
	var buf bytes.Buffer
	code := PrintError(&buf, "human", "x", "y", ExitBusinessError)
	if code != ExitBusinessError {
		t.Fatal()
	}
	if !strings.Contains(buf.String(), "Error: x: y") {
		t.Fatalf("got %s", buf.String())
	}
}

func TestPrintError_JSON(t *testing.T) {
	var buf bytes.Buffer
	code := PrintError(&buf, "json", "x", "y", ExitNotFound)
	if code != ExitNotFound {
		t.Fatal()
	}
	if !strings.Contains(buf.String(), `"x"`) {
		t.Fatalf("got %s", buf.String())
	}
}

func TestParseFormat(t *testing.T) {
	for in, want := range map[string]string{
		"":        "human",
		"human":   "human",
		"json":    "json",
		"yaml":    "yaml",
		"JSON":    "json",
		"unknown": "unknown",
	} {
		if got := ParseFormat(in); got != want {
			t.Fatalf("ParseFormat(%q)=%q want %q", in, got, want)
		}
	}
}

func noop(ctx context.Context, args []string, out, err io.Writer) ExitCode { return ExitOK }

func TestQuoteJSON_Specials(t *testing.T) {
	s := quoteJSON("\n\ta\\b\"c" + string(rune(0)))
	checks := []string{
		"\\n",
		"\\t",
		"\\\\",
		"\\\"",
		"\\u0000",
	}
	for _, want := range checks {
		if !strings.Contains(s, want) {
			t.Fatalf("escape missing %q in %q", want, s)
		}
	}
}
