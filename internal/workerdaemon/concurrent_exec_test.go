package workerdaemon

// concurrent_exec_test.go — covers the daemon-side shared opt-in predicate
// (concurrencyEnabled). The engine attach/reattach/snapshot wiring moved into the
// agent-runtime path (package agentruntime) along with its tests.

import (
	"testing"

	"github.com/oopslink/agent-center/internal/agent"
)

// testExecs is a one-entry allowed_executors list that opts an agent into concurrency.
var testExecs = []agent.ExecutorProfile{{CLI: "claude-code", Model: "m"}}

func TestConcurrencyEnabled(t *testing.T) {
	cases := []struct {
		name string
		pl   reconcilePayload
		want bool
	}{
		{"both set", reconcilePayload{MaxConcurrentTasks: 2, AllowedExecutors: testExecs}, true},
		{"no max", reconcilePayload{AllowedExecutors: testExecs}, false},
		{"no executors", reconcilePayload{MaxConcurrentTasks: 2}, false},
		{"neither", reconcilePayload{}, false},
	}
	for _, tc := range cases {
		if got := concurrencyEnabled(tc.pl); got != tc.want {
			t.Errorf("%s: concurrencyEnabled = %v, want %v", tc.name, got, tc.want)
		}
	}
}
