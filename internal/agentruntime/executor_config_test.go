package agentruntime

import "testing"

// TestExecutorConfig_ConcurrencyEnabled covers the runtime concurrency opt-in
// predicate (moved from the daemon's concurrent_exec.go into the runtime domain,
// issue-68ccb310): concurrency activates only when the config sets
// MaxConcurrentTasks>0 AND lists at least one allowed executor. testExecs is the
// shared one-entry allowed_executors fixture defined in executor_runtime_test.go.
func TestExecutorConfig_ConcurrencyEnabled(t *testing.T) {
	cases := []struct {
		name string
		cfg  ExecutorConfig
		want bool
	}{
		{"both set", ExecutorConfig{MaxConcurrentTasks: 2, AllowedExecutors: testExecs}, true},
		{"no max", ExecutorConfig{AllowedExecutors: testExecs}, false},
		{"no executors", ExecutorConfig{MaxConcurrentTasks: 2}, false},
		{"neither", ExecutorConfig{}, false},
	}
	for _, tc := range cases {
		if got := tc.cfg.ConcurrencyEnabled(); got != tc.want {
			t.Errorf("%s: ConcurrencyEnabled = %v, want %v", tc.name, got, tc.want)
		}
	}
}
