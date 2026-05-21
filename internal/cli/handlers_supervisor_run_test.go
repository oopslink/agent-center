package cli

import (
	"bytes"
	"context"
	"flag"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestSupervisorRunCommand_FailsWithoutScope exercises the full
// SupervisorRunCommand handler — flags, parsing, and the supervisor.Run
// error path when scope is missing.
func TestSupervisorRunCommand_FailsWithoutScope(t *testing.T) {
	cmd := SupervisorRunCommand()
	fs := flag.NewFlagSet(cmd.Name, flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	h := cmd.Flags(fs)
	if err := fs.Parse([]string{
		"--invocation-id=INV1",
		"--trigger-events=01HE",
		"--memory-dir=/tmp/test",
	}); err != nil {
		t.Fatal(err)
	}
	var out, errBuf bytes.Buffer
	code := h(context.Background(), nil, &out, &errBuf)
	if code == ExitOK {
		t.Errorf("expected non-OK; stderr=%s", errBuf.String())
	}
	if !strings.Contains(errBuf.String(), "scope") {
		t.Errorf("missing scope diagnostic: %s", errBuf.String())
	}
}

// TestSupervisorRunCommand_HappyWithFakeClaude exercises the full
// supervisor subprocess flow with a fake claude script.
func TestSupervisorRunCommand_HappyWithFakeClaude(t *testing.T) {
	memDir := t.TempDir()
	usageDir := t.TempDir()
	// fake claude script
	dir := t.TempDir()
	script := filepath.Join(dir, "fake.sh")
	body := `#!/bin/sh
echo '{"type":"thinking","text":"ok"}'
echo '{"type":"usage","input_tokens":1,"output_tokens":2}'
echo '{"type":"end_turn"}'
exit 0
`
	if err := os.WriteFile(script, []byte(body), 0o755); err != nil {
		t.Fatal(err)
	}
	cmd := SupervisorRunCommand()
	fs := flag.NewFlagSet(cmd.Name, flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	h := cmd.Flags(fs)
	if err := fs.Parse([]string{
		"--scope=task:T-1",
		"--invocation-id=INVHAPPY",
		"--trigger-events=01HEZX",
		"--memory-dir=" + memDir,
		"--usage-dir=" + usageDir,
		"--claude-binary=" + script,
	}); err != nil {
		t.Fatal(err)
	}
	var out, errBuf bytes.Buffer
	code := h(context.Background(), nil, &out, &errBuf)
	if code != ExitOK {
		t.Errorf("code = %d errs=%s", code, errBuf.String())
	}
	// usage file should exist
	if _, err := os.Stat(filepath.Join(usageDir, "INVHAPPY.usage.json")); err != nil {
		t.Errorf("usage missing: %v", err)
	}
}
