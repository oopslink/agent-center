package peek_test

import (
	"context"
	"errors"
	"fmt"
	"math/rand"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/oopslink/agent-center/internal/observability/peek"
)

func shortSock(t *testing.T) string {
	t.Helper()
	p := fmt.Sprintf("/tmp/pk_%d_%d.sock", os.Getpid(), rand.Int63())
	t.Cleanup(func() { _ = os.Remove(p) })
	return p
}

func setupServer(t *testing.T) (*peek.Server, string, string) {
	t.Helper()
	root := t.TempDir()
	// macOS limits unix socket paths to ~104 bytes; t.TempDir() is too
	// long, so use a short /tmp/<random> filename.
	sock := shortSock(t)
	srv, err := peek.NewServer(sock, root)
	if err != nil {
		t.Fatal(err)
	}
	srv = srv.WithPollInterval(20 * time.Millisecond)
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(func() {
		cancel()
		_ = srv.Close()
	})
	go func() {
		_ = srv.Serve(ctx)
	}()
	// Allow the listener to come up.
	time.Sleep(50 * time.Millisecond)
	return srv, sock, root
}

func writeEvents(t *testing.T, root, execID string, lines []string) string {
	t.Helper()
	dir := filepath.Join(root, execID)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	p := filepath.Join(dir, "events.jsonl")
	f, err := os.Create(p)
	if err != nil {
		t.Fatal(err)
	}
	for _, l := range lines {
		_, _ = f.WriteString(l + "\n")
	}
	_ = f.Close()
	return p
}

func TestPeek_TailLast(t *testing.T) {
	_, sock, root := setupServer(t)
	writeEvents(t, root, "E-1", []string{
		`{"type":"thinking","text":"a"}`,
		`{"type":"thinking","text":"b"}`,
		`{"type":"tool_use","name":"Bash"}`,
		`{"type":"thinking","text":"c"}`,
	})
	c := peek.NewClient(sock)
	frames, err := c.Stream(context.Background(), peek.Request{ExecutionID: "E-1", Last: 2})
	if err != nil {
		t.Fatal(err)
	}
	var got []string
	for f := range frames {
		if f.Err != nil {
			t.Fatalf("server err: %+v", f.Err)
		}
		if f.Done {
			break
		}
		got = append(got, f.Line)
	}
	if len(got) != 2 {
		t.Fatalf("expected 2 lines, got %d: %v", len(got), got)
	}
}

func TestPeek_KindFilter(t *testing.T) {
	_, sock, root := setupServer(t)
	writeEvents(t, root, "E-1", []string{
		`{"type":"thinking","text":"a"}`,
		`{"type":"tool_use","name":"Bash"}`,
		`{"type":"thinking","text":"b"}`,
	})
	c := peek.NewClient(sock)
	frames, err := c.Stream(context.Background(), peek.Request{ExecutionID: "E-1", Kind: "thinking"})
	if err != nil {
		t.Fatal(err)
	}
	var n int
	for f := range frames {
		if f.Err != nil {
			t.Fatalf("server err: %+v", f.Err)
		}
		if f.Done {
			break
		}
		n++
	}
	if n != 2 {
		t.Fatalf("expected 2 thinking lines, got %d", n)
	}
}

func TestPeek_ExecutionNotFound(t *testing.T) {
	_, sock, _ := setupServer(t)
	c := peek.NewClient(sock)
	frames, err := c.Stream(context.Background(), peek.Request{ExecutionID: "E-missing"})
	if err != nil {
		t.Fatal(err)
	}
	var got *peek.ErrorPayload
	for f := range frames {
		if f.Err != nil {
			got = f.Err
			break
		}
	}
	if got == nil || got.Reason != peek.ReasonExecutionNotFound {
		t.Fatalf("expected execution_not_found, got %+v", got)
	}
}

func TestPeek_TraceFileMissing(t *testing.T) {
	_, sock, root := setupServer(t)
	// Create dir without events.jsonl
	_ = os.MkdirAll(filepath.Join(root, "E-2"), 0o755)
	c := peek.NewClient(sock)
	frames, _ := c.Stream(context.Background(), peek.Request{ExecutionID: "E-2"})
	var got *peek.ErrorPayload
	for f := range frames {
		if f.Err != nil {
			got = f.Err
			break
		}
	}
	if got == nil || got.Reason != peek.ReasonTraceFileMissing {
		t.Fatalf("expected trace_file_missing, got %+v", got)
	}
}

func TestPeek_InvalidRequest_EmptyExecutionID(t *testing.T) {
	_, sock, _ := setupServer(t)
	c := peek.NewClient(sock)
	if _, err := c.Stream(context.Background(), peek.Request{ExecutionID: ""}); !errors.Is(err, peek.ErrInvalidRequest) {
		t.Fatalf("expected ErrInvalidRequest, got %v", err)
	}
}

func TestPeek_WorkerOffline_ClientDialFails(t *testing.T) {
	c := peek.NewClient(filepath.Join(t.TempDir(), "absent.sock"))
	_, err := c.Stream(context.Background(), peek.Request{ExecutionID: "E-1"})
	var ce *peek.ErrConnectFailed
	if !errors.As(err, &ce) {
		t.Fatalf("expected ErrConnectFailed, got %v", err)
	}
}

func TestPeek_FollowMode_StreamsAppends(t *testing.T) {
	_, sock, root := setupServer(t)
	writeEvents(t, root, "E-3", []string{`{"type":"thinking","text":"a"}`})
	c := peek.NewClient(sock)
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	frames, err := c.Stream(ctx, peek.Request{ExecutionID: "E-3", Follow: true})
	if err != nil {
		t.Fatal(err)
	}
	// Read the initial line.
	first := <-frames
	if first.Err != nil || first.Line == "" {
		t.Fatalf("first frame: %+v", first)
	}
	// Append a new line.
	go func() {
		time.Sleep(80 * time.Millisecond)
		f, _ := os.OpenFile(filepath.Join(root, "E-3", "events.jsonl"), os.O_APPEND|os.O_WRONLY, 0o644)
		_, _ = f.WriteString(`{"type":"thinking","text":"b"}` + "\n")
		_ = f.Close()
	}()
	select {
	case f := <-frames:
		if f.Err != nil {
			t.Fatalf("follow frame error: %+v", f.Err)
		}
		if f.Line == "" {
			t.Fatal("follow frame missing line")
		}
	case <-time.After(1500 * time.Millisecond):
		t.Fatal("follow did not deliver appended line in 1.5s")
	}
}

func TestPeek_ConfigurationValidation(t *testing.T) {
	if _, err := peek.NewServer("", "/tmp"); err == nil {
		t.Fatal("expected error for empty socket_path")
	}
	if _, err := peek.NewServer("/tmp/x.sock", ""); err == nil {
		t.Fatal("expected error for empty execution_root")
	}
}
