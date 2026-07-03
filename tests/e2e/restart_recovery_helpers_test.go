package e2e

// Helpers for the deployment-level restart-recovery e2e (restart_recovery_e2e_test.go).
// Everything here spawns/queries REAL processes: a real agent-center server over its
// real admin unix socket, and a real `agent-center worker run` daemon. No in-process
// shortcuts for the worker/supervisor/session path under test.

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// buildBin builds a package main into out (absolute path). Shared go build cache
// makes repeats cheap.
func buildBin(t *testing.T, pkg, out string) {
	t.Helper()
	cmd := exec.Command("go", "build", "-o", out, pkg)
	cmd.Stderr = os.Stderr
	if err := cmd.Run(); err != nil {
		t.Fatalf("go build %s: %v", pkg, err)
	}
}

// unixHTTPClient returns an http.Client that dials the given unix socket for every
// request (the admin endpoint is an HTTP server on a unix socket).
func unixHTTPClient(sock string) *http.Client {
	return &http.Client{
		Timeout: 15 * time.Second,
		Transport: &http.Transport{
			DialContext: func(ctx context.Context, _, _ string) (net.Conn, error) {
				var d net.Dialer
				return d.DialContext(ctx, "unix", sock)
			},
		},
	}
}

// adminReq issues an HTTP request to the admin endpoint over the unix socket with a
// bearer token. Returns status + raw body.
func adminReq(t *testing.T, sock, method, path, token string, body any) (int, string) {
	t.Helper()
	var rdr *bytes.Reader
	if body != nil {
		b, _ := json.Marshal(body)
		rdr = bytes.NewReader(b)
	} else {
		rdr = bytes.NewReader(nil)
	}
	req, err := http.NewRequest(method, "http://unix"+path, rdr)
	if err != nil {
		t.Fatalf("new request: %v", err)
	}
	req.Header.Set("Content-Type", "application/json")
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	resp, err := unixHTTPClient(sock).Do(req)
	if err != nil {
		t.Fatalf("admin %s %s: %v", method, path, err)
	}
	defer resp.Body.Close()
	buf := new(bytes.Buffer)
	_, _ = buf.ReadFrom(resp.Body)
	return resp.StatusCode, buf.String()
}

// waitFile polls until path exists or the deadline passes.
func waitFile(t *testing.T, path string, within time.Duration) {
	t.Helper()
	deadline := time.Now().Add(within)
	for time.Now().Before(deadline) {
		if _, err := os.Stat(path); err == nil {
			return
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatalf("file %s never appeared within %s", path, within)
}

// readTrim reads a whole file and trims whitespace.
func readTrim(t *testing.T, path string) string {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	return strings.TrimSpace(string(b))
}

// mintToken mints a scoped admin token via /admin/admintoken/create using the
// bootstrap token, returning the plaintext.
func mintToken(t *testing.T, sock, bootstrap, owner string, scopes []string) string {
	t.Helper()
	code, body := adminReq(t, sock, "POST", "/admin/admintoken/create", bootstrap, map[string]any{
		"owner": owner, "scopes": scopes, "created_by": "p46-e2e",
	})
	if code != 200 {
		t.Fatalf("mint token (%s): code=%d body=%s", owner, code, body)
	}
	var r struct {
		Plaintext string `json:"plaintext"`
	}
	if err := json.Unmarshal([]byte(body), &r); err != nil || r.Plaintext == "" {
		t.Fatalf("mint token decode: err=%v body=%s", err, body)
	}
	return r.Plaintext
}

// procHandle wraps a spawned process plus its captured stdout/stderr.
type procHandle struct {
	cmd     *exec.Cmd
	outPath string
	errPath string
}

func (p *procHandle) out() string {
	o, _ := os.ReadFile(p.outPath)
	e, _ := os.ReadFile(p.errPath)
	return string(o) + "\n" + string(e)
}

// spawn starts a process with captured stdio. env entries (KEY=VAL) are appended
// to the current environment.
//
// stdout/stderr are backed by real *os.File sinks (NOT an in-memory writer). This is
// deliberate: with a non-*os.File writer, os/exec creates an OS pipe and a copy
// goroutine that cmd.Wait() awaits (awaitGoroutines). Under the controller /
// process-per-agent model, the worker's agent-runtime children are an INTENTIONALLY
// surviving, separate process group (ASSERT-2 / gap5) that inherit the pipe's write
// end — so the pipe never reaches EOF and cmd.Wait() blocks until the test's 10-min
// timeout. Handing the child a plain *os.File fd (no copy goroutine) makes cmd.Wait()
// reap only the direct child and return promptly, leaving the designed survivor alive
// without wedging teardown.
func spawn(t *testing.T, name string, args []string, env []string) *procHandle {
	t.Helper()
	cmd := exec.Command(name, args...)
	cmd.Env = append(os.Environ(), env...)
	outF, err := os.CreateTemp(t.TempDir(), "proc-stdout-*.log")
	if err != nil {
		t.Fatalf("spawn %s: create stdout sink: %v", name, err)
	}
	errF, err := os.CreateTemp(t.TempDir(), "proc-stderr-*.log")
	if err != nil {
		t.Fatalf("spawn %s: create stderr sink: %v", name, err)
	}
	cmd.Stdout = outF
	cmd.Stderr = errF
	h := &procHandle{cmd: cmd, outPath: outF.Name(), errPath: errF.Name()}
	if err := cmd.Start(); err != nil {
		t.Fatalf("start %s: %v", name, err)
	}
	// The child dup'd the fds at Start; the parent's copies aren't needed for the
	// child (or its surviving descendants) to keep writing.
	_ = outF.Close()
	_ = errF.Close()
	return h
}

// waitBounded reaps the process but never blocks the caller past timeout. A surviving
// descendant that inherited an fd must not be able to wedge teardown; the *os.File
// stdio above already prevents the copy-goroutine hang, this is a defensive cap for
// any other inherited-fd surprise.
func (p *procHandle) waitBounded(timeout time.Duration) {
	if p.cmd.Process == nil {
		return
	}
	done := make(chan struct{})
	go func() { _ = p.cmd.Wait(); close(done) }()
	select {
	case <-done:
	case <-time.After(timeout):
	}
}

// sigkill hard-kills the process and reaps it (bounded). It kills ONLY the direct
// process, not the process group: the restart-recovery test kills the worker mid-run
// and REQUIRES the agent-runtime survivor to stay alive (ASSERT-2 / gap5). Reaping the
// designed survivors is the caller's separate stray-reap cleanup, not this helper.
func (p *procHandle) sigkill(t *testing.T) {
	t.Helper()
	if p.cmd.Process != nil {
		_ = p.cmd.Process.Kill()
	}
	p.waitBounded(10 * time.Second)
}

// sigterm sends SIGTERM and waits (graceful, bounded).
func (p *procHandle) sigterm() {
	if p.cmd.Process != nil {
		_ = p.cmd.Process.Signal(os.Interrupt)
	}
	p.waitBounded(10 * time.Second)
}

// waitFor polls fn until it returns true or the deadline passes; returns the final
// bool. Polls every 100ms.
func waitFor(within time.Duration, fn func() bool) bool {
	deadline := time.Now().Add(within)
	for time.Now().Before(deadline) {
		if fn() {
			return true
		}
		time.Sleep(100 * time.Millisecond)
	}
	return fn()
}

// fileContains reports whether the file at path exists and contains substr.
func fileContains(path, substr string) bool {
	b, err := os.ReadFile(path)
	if err != nil {
		return false
	}
	return strings.Contains(string(b), substr)
}

// countOccurrences counts non-overlapping occurrences of substr in the file.
func countOccurrences(path, substr string) int {
	b, err := os.ReadFile(path)
	if err != nil {
		return 0
	}
	return strings.Count(string(b), substr)
}

// dumpDir returns a recursive listing of dir for diagnostics.
func dumpDir(dir string) string {
	var b strings.Builder
	_ = filepath.Walk(dir, func(p string, info os.FileInfo, err error) error {
		if err != nil {
			return nil
		}
		rel, _ := filepath.Rel(dir, p)
		fmt.Fprintf(&b, "  %s\n", rel)
		return nil
	})
	return b.String()
}
