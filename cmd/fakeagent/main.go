// Command fakeagent is the Phase 7 e2e harness fake agent CLI
// (plan-7 § 3.8). It plays the role of a vendor agent (claude / codex /
// opencode etc.) inside e2e scenarios so the test runtime can spawn a
// real subprocess that emits the agent-adapter event stream the worker
// daemon expects.
//
// Phase 5/6 tests already cover the deeper shim protocol with
// `internal/agentadapter/...` fakes; this binary is the public CLI
// the harness invokes when it needs cross-process behaviour
// (PID isolation, setsid escape, etc.).
//
// Script format (line-delimited JSON):
//
//	{"type":"start","text":"working on T-1"}
//	{"type":"progress","milestone":"step_1","content":"compiled"}
//	{"type":"done","exit_code":0}
//
// Optional env vars:
//
//	FAKEAGENT_FAIL_AT=step_3  → write `failed` after step_3
//	FAKEAGENT_HANG=true       → block forever after first event
//
// See `docs/plans/phase-7-bridge-inbound-deploy.md § 3.8` for the
// full design intent.
package main

import (
	"bufio"
	"flag"
	"fmt"
	"io"
	"os"
	"strings"
)

func main() {
	script := flag.String("script", "", "path to JSONL script")
	flag.Parse()
	if *script == "" {
		fmt.Fprintln(os.Stderr, "fakeagent: --script=<path> required")
		os.Exit(2)
	}
	f, err := os.Open(*script)
	if err != nil {
		fmt.Fprintf(os.Stderr, "fakeagent: open script: %v\n", err)
		os.Exit(2)
	}
	defer f.Close()
	if err := run(f, os.Stdout); err != nil {
		fmt.Fprintf(os.Stderr, "fakeagent: %v\n", err)
		os.Exit(1)
	}
}

func run(in io.Reader, out io.Writer) error {
	scanner := bufio.NewScanner(in)
	failAt := os.Getenv("FAKEAGENT_FAIL_AT")
	hang := os.Getenv("FAKEAGENT_HANG") == "true"
	step := 0
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" {
			continue
		}
		fmt.Fprintln(out, line)
		step++
		if failAt != "" && strings.Contains(line, failAt) {
			fmt.Fprintln(out, `{"type":"failed","reason":"fakeagent_failpoint"}`)
			return nil
		}
		if hang {
			select {} // block forever
		}
	}
	return scanner.Err()
}
