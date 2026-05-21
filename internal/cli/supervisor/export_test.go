package supervisor

import "github.com/oopslink/agent-center/internal/cognition"

// ExportParseScope exposes parseScopeFlag to *_test packages.
func ExportParseScope(s string) (cognition.InvocationScope, error) {
	return parseScopeFlag(s)
}
