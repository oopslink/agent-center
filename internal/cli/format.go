package cli

import (
	"flag"
	"io"
	"strings"
)

// Output format constants. P11 § 3.8: the CLI exposes three formats —
// `table` (default, human-readable), `json` (stable schema, snake_case
// keys; safe for scripting), and `text` (one canonical identifier per
// line; safe for `xargs`).
//
// `human` is retained as a backwards-compatible alias of `table` so
// pre-§3.8 scripts that pass `--format=human` keep working. It is not
// advertised in help strings and `NormalizeFormat` collapses it onto
// `table`.
const (
	FormatTable = "table"
	FormatJSON  = "json"
	FormatText  = "text"
	FormatHuman = "human" // alias of FormatTable; not advertised.
)

// formatFlagHelp returns the help string for a `--format` flag. All
// callers use this so the help text stays in sync across the tree.
func formatFlagHelp() string {
	return "output format (table|json|text; default: table)"
}

// NormalizeFormat maps an input format string to one of the three
// canonical values. Empty input defaults to `table`. `human` aliases
// to `table`. Returns ok=false when the input is unrecognised.
func NormalizeFormat(in string) (string, bool) {
	switch strings.ToLower(strings.TrimSpace(in)) {
	case "", FormatTable, FormatHuman:
		return FormatTable, true
	case FormatJSON:
		return FormatJSON, true
	case FormatText:
		return FormatText, true
	default:
		return "", false
	}
}

// IsValidFormat reports whether the input is an accepted format string.
func IsValidFormat(in string) bool {
	_, ok := NormalizeFormat(in)
	return ok
}

// writeTextLines is a helper for list handlers' `text` format: writes
// one identifier per line, no headers, no decoration.
func writeTextLines(out io.Writer, ids []string) {
	for _, id := range ids {
		writeOut(out, id)
	}
}

// validateRouterFormatFlag is the router-level gate. If the leaf handler
// declared a `--format` flag, normalises its value in place (collapsing
// `human` → `table`) and rejects values outside {table,json,text}.
// Returns false (after emitting a usage_error) when invalid.
func validateRouterFormatFlag(fs *flag.FlagSet, errw io.Writer) bool {
	f := fs.Lookup("format")
	if f == nil {
		return true
	}
	norm, ok := NormalizeFormat(f.Value.String())
	if !ok {
		PrintError(errw, "text", "usage_error",
			"invalid --format (want table|json|text)", ExitUsage)
		return false
	}
	_ = f.Value.Set(norm)
	return true
}
