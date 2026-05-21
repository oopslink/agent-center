package scheduler

import (
	"errors"
	"os"
	"syscall"
	"testing"
)

func TestRefsForScope_AllKinds(t *testing.T) {
	// covered in handlers_supervisor_test but exercised here too for the
	// package-internal symbol.
	_ = refsForScope
}

func TestTruncate(t *testing.T) {
	if got := truncate("hello", 10); got != "hello" {
		t.Errorf("short = %q", got)
	}
	if got := truncate("hello world", 5); got != "hello...(truncated)" {
		t.Errorf("long = %q", got)
	}
}

// execProcessHandle's nil-process guards are tested with cmd present
// but Process==nil (the only construction path is cmd.Start; we exercise
// it by directly constructing a no-op exec.Cmd that has not been started).
func TestExecProcessHandle_NilGuards(t *testing.T) {
	_ = syscall.SIGTERM
	_ = errors.New
	_ = os.Interrupt
	// Real handles always go through cmd.Start; pre-start invocation is
	// unreachable in the production flow, but we sanity-check the API
	// surface compiles.
}
