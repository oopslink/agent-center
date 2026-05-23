// Package cli implements the agent-center CLI router + handler registry.
//
// Per plan-1 § 3.1.4: stdlib `flag` + hand-rolled sub-command router (R8
// spike decision). The router supports nested verbs (e.g.
// `worker proposal accept`) by recursively matching positional args
// against a static command tree.
package cli

import (
	"bytes"
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"sort"
	"strings"
)

// Exit codes per 03-cli § 5.
const (
	ExitOK                 = 0
	ExitBusinessError      = 1
	ExitUsage              = 2
	ExitVersionConflict    = 16
	ExitNotFound           = 17
	ExitInvalidTransition  = 18
	ExitInvariantViolation = 19
	ExitNotImplemented     = 64
	ExitSIGINT             = 130
)

// Handler is the signature of a leaf command implementation.
//
// The framework parses positional + flag arguments before calling the
// handler. Handler implementations write business output to `out` (stdout)
// and diagnostics to `err` (stderr) — they MUST NOT call os.Exit; return
// an Exit struct instead.
type Handler func(ctx context.Context, args []string, out, err io.Writer) ExitCode

// ExitCode pairs a numeric exit value with optional structured info that
// the router can post-process (e.g. JSON-format mode).
type ExitCode int

// Command is a node in the command tree. A node is either a group (has
// Subcommands) or a leaf (has Run). It cannot be both.
type Command struct {
	Name        string
	Summary     string
	LongHelp    string
	Subcommands []*Command
	Run         Handler

	// Flags can be registered up-front via this hook. The hook gets a
	// fresh *flag.FlagSet to register on; the returned Handler receives
	// the parsed-arg slice.
	Flags func(fs *flag.FlagSet) Handler
}

// Router carries shared application state (DB, services, etc.) and the
// root command tree. Construct via NewRouter then add commands via Add.
type Router struct {
	Root *Command
	Out  io.Writer
	Err  io.Writer
}

// NewRouter builds an empty router rooted at the binary name.
func NewRouter(binaryName string) *Router {
	return &Router{
		Root: &Command{Name: binaryName, Summary: "agent-center CLI"},
		Out:  os.Stdout,
		Err:  os.Stderr,
	}
}

// Add inserts a sub-command tree at the given path (e.g. ["worker",
// "proposal"]). The leaf Command is appended as the last element's
// Subcommands.
func (r *Router) Add(path []string, cmd *Command) error {
	if cmd == nil {
		return errors.New("cli: Add requires non-nil Command")
	}
	parent := r.Root
	for _, segment := range path {
		next := findSubcommand(parent, segment)
		if next == nil {
			next = &Command{Name: segment}
			parent.Subcommands = append(parent.Subcommands, next)
		}
		parent = next
	}
	if findSubcommand(parent, cmd.Name) != nil {
		return fmt.Errorf("cli: duplicate command %s at path %v", cmd.Name, path)
	}
	parent.Subcommands = append(parent.Subcommands, cmd)
	return nil
}

// Run executes the router with the given args (typically os.Args[1:]).
// Returns the exit code the caller should pass to os.Exit. Help / version
// requests are part of `args` (e.g. `--help`).
func (r *Router) Run(ctx context.Context, args []string) ExitCode {
	cmd := r.Root
	for i := 0; i < len(args); i++ {
		a := args[i]
		if a == "--help" || a == "-h" {
			r.printHelp(cmd)
			return ExitOK
		}
		next := findSubcommand(cmd, a)
		if next == nil {
			break
		}
		cmd = next
		args = args[i+1:]
		i = -1
	}
	// If no leaf reached, print help for the deepest group.
	if cmd.Run == nil && cmd.Flags == nil {
		r.printHelp(cmd)
		return ExitOK
	}
	var handler Handler
	if cmd.Flags != nil {
		fs := flag.NewFlagSet(cmd.Name, flag.ContinueOnError)
		// silence default flag err output; we route diagnostics ourselves.
		fs.SetOutput(io.Discard)
		handler = cmd.Flags(fs)
		// Permissive parse loop: stdlib flag.Parse stops at the first
		// non-flag arg, but we want `cmd <pos> --flag=v` to work too.
		// Re-parse the residual until no more flags are seen.
		positionals, err := permissiveParse(fs, args)
		if err != nil {
			fmt.Fprintf(r.Err, "Error: usage: %v\n", err)
			r.printUsage(cmd, r.Err)
			return ExitUsage
		}
		args = positionals
		// P11 § 3.8: universal --format validation. If the leaf handler
		// declared a `--format` flag, normalise human→table and reject
		// values outside the {table,json,text} set before dispatching.
		if !validateRouterFormatFlag(fs, r.Err) {
			return ExitUsage
		}
	} else {
		handler = cmd.Run
	}
	if handler == nil {
		fmt.Fprintf(r.Err, "Error: command %q has no handler\n", cmd.Name)
		return ExitNotImplemented
	}
	return handler(ctx, args, r.Out, r.Err)
}

func findSubcommand(parent *Command, name string) *Command {
	for _, c := range parent.Subcommands {
		if c.Name == name {
			return c
		}
	}
	return nil
}

func (r *Router) printHelp(cmd *Command) {
	fmt.Fprintf(r.Out, "%s — %s\n", cmd.Name, cmd.Summary)
	if cmd.LongHelp != "" {
		fmt.Fprintf(r.Out, "\n%s\n", cmd.LongHelp)
	}
	if len(cmd.Subcommands) > 0 {
		fmt.Fprintln(r.Out, "\nSubcommands:")
		// sort for stable output
		subs := append([]*Command(nil), cmd.Subcommands...)
		sort.Slice(subs, func(i, j int) bool { return subs[i].Name < subs[j].Name })
		for _, s := range subs {
			fmt.Fprintf(r.Out, "  %-24s %s\n", s.Name, s.Summary)
		}
	}
}

func (r *Router) printUsage(cmd *Command, w io.Writer) {
	fmt.Fprintf(w, "Usage: %s ...\n", cmd.Name)
}

// ErrorReason captures the structured error returned to the user via JSON.
// Aligns with 03-cli § 6.
type ErrorReason struct {
	Reason  string `json:"reason"`
	Message string `json:"message"`
}

// FormatJSONError renders an error response.
func FormatJSONError(reason, message string) string {
	var b bytes.Buffer
	b.WriteString(`{"error":{"reason":`)
	b.WriteString(quoteJSON(reason))
	b.WriteString(`,"message":`)
	b.WriteString(quoteJSON(message))
	b.WriteString(`}}`)
	return b.String()
}

func quoteJSON(s string) string {
	// Minimal JSON string escape (sufficient for our diagnostic payloads).
	var b bytes.Buffer
	b.WriteByte('"')
	for _, c := range s {
		switch c {
		case '"':
			b.WriteString(`\"`)
		case '\\':
			b.WriteString(`\\`)
		case '\n':
			b.WriteString(`\n`)
		case '\t':
			b.WriteString(`\t`)
		default:
			if c < 0x20 {
				fmt.Fprintf(&b, `\u%04x`, c)
			} else {
				b.WriteRune(c)
			}
		}
	}
	b.WriteByte('"')
	return b.String()
}

// PrintError writes a formatted error to stderr in the requested format
// and returns the exit code.
func PrintError(w io.Writer, format, reason, message string, code ExitCode) ExitCode {
	if format == "json" {
		fmt.Fprintln(w, FormatJSONError(reason, message))
	} else {
		fmt.Fprintf(w, "Error: %s: %s\n", reason, message)
	}
	return code
}

// permissiveParse repeatedly Parses fs against args until no flags are
// consumed. Lets users freely interleave positional and flag args.
//
// Algorithm:
//  1. Call fs.Parse(args). If it errors, return error.
//  2. Collect positional residual via fs.Args().
//  3. If the FIRST element of the residual looks like a flag, slice off
//     the leading positional(s) we've already accumulated and re-Parse.
//  4. Stop when no further flags appear or residual is exhausted.
func permissiveParse(fs *flag.FlagSet, args []string) ([]string, error) {
	var positionals []string
	remaining := args
	for {
		if err := fs.Parse(remaining); err != nil {
			return nil, err
		}
		residual := fs.Args()
		if len(residual) == 0 {
			return positionals, nil
		}
		// Find the first flag-like token in residual.
		flagIdx := -1
		for i, a := range residual {
			if a == "--" {
				// POSIX end-of-flags: all remaining are positional.
				positionals = append(positionals, residual[:i]...)
				positionals = append(positionals, residual[i+1:]...)
				return positionals, nil
			}
			if len(a) >= 2 && a[0] == '-' {
				flagIdx = i
				break
			}
		}
		if flagIdx < 0 {
			// No more flags; all residual is positional.
			positionals = append(positionals, residual...)
			return positionals, nil
		}
		// Everything before flagIdx is positional; re-parse from flagIdx.
		positionals = append(positionals, residual[:flagIdx]...)
		remaining = residual[flagIdx:]
	}
}

// ParseFormat returns "human" or "json" (or yaml in the future); defaults
// to "human" for empty input.
func ParseFormat(v string) string {
	switch strings.ToLower(v) {
	case "", "human":
		return "human"
	case "json":
		return "json"
	case "yaml":
		return "yaml"
	}
	return v // caller validates
}
