package peek

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

// Server is the worker-daemon side peek-trace server. It listens on a
// unix socket and serves Request frames by tailing the per-execution
// events.jsonl file.
//
// ExecutionRoot is the on-disk root holding per-execution dirs (matches
// shim.Dir layout: <root>/<execution_id>/events.jsonl).
type Server struct {
	executionRoot string
	listener      net.Listener
	addr          string

	pollInterval time.Duration
}

// NewServer constructs a Server. socketPath is where the daemon listens
// (typically /var/run/agent-center-worker/peek.sock). executionRoot is the
// per-execution dir root.
func NewServer(socketPath, executionRoot string) (*Server, error) {
	if socketPath == "" {
		return nil, errors.New("peek server: socket_path required")
	}
	if executionRoot == "" {
		return nil, errors.New("peek server: execution_root required")
	}
	// Ensure parent dir of socket exists; remove stale socket file.
	if err := os.MkdirAll(filepath.Dir(socketPath), 0o755); err != nil {
		return nil, fmt.Errorf("peek server: mkdir: %w", err)
	}
	_ = os.Remove(socketPath)
	ln, err := net.Listen("unix", socketPath)
	if err != nil {
		return nil, fmt.Errorf("peek server: listen: %w", err)
	}
	return &Server{
		executionRoot: executionRoot,
		listener:      ln,
		addr:          socketPath,
		pollInterval:  100 * time.Millisecond,
	}, nil
}

// Addr returns the socket path.
func (s *Server) Addr() string { return s.addr }

// WithPollInterval overrides the follow-mode poll cadence (tests use a
// shorter value to keep wall-clock low).
func (s *Server) WithPollInterval(d time.Duration) *Server {
	if d > 0 {
		s.pollInterval = d
	}
	return s
}

// Close shuts down the listener.
func (s *Server) Close() error {
	if s.listener == nil {
		return nil
	}
	err := s.listener.Close()
	_ = os.Remove(s.addr)
	return err
}

// Serve accepts connections until ctx is cancelled.
func (s *Server) Serve(ctx context.Context) error {
	go func() {
		<-ctx.Done()
		_ = s.listener.Close()
	}()
	var wg sync.WaitGroup
	for {
		conn, err := s.listener.Accept()
		if err != nil {
			if errors.Is(err, net.ErrClosed) || ctx.Err() != nil {
				wg.Wait()
				return nil
			}
			return fmt.Errorf("peek server: accept: %w", err)
		}
		wg.Add(1)
		go func() {
			defer wg.Done()
			s.handle(ctx, conn)
		}()
	}
}

func (s *Server) handle(ctx context.Context, conn net.Conn) {
	defer conn.Close()
	r := bufio.NewReader(conn)
	line, err := r.ReadBytes('\n')
	if err != nil {
		return
	}
	var req Request
	if err := json.Unmarshal(line, &req); err != nil {
		_ = writeErr(conn, ReasonInvalidRequest, fmt.Sprintf("malformed request: %v", err))
		return
	}
	if req.ExecutionID == "" {
		_ = writeErr(conn, ReasonInvalidRequest, "execution_id required")
		return
	}
	execDir := filepath.Join(s.executionRoot, req.ExecutionID)
	if st, err := os.Stat(execDir); err != nil || !st.IsDir() {
		_ = writeErr(conn, ReasonExecutionNotFound, fmt.Sprintf("execution dir not found: %s", execDir))
		return
	}
	eventsPath := filepath.Join(execDir, "events.jsonl")
	if _, err := os.Stat(eventsPath); err != nil {
		_ = writeErr(conn, ReasonTraceFileMissing, fmt.Sprintf("events.jsonl not present at %s", eventsPath))
		return
	}
	// Step 1: read existing lines, optionally tail to last N.
	all, err := readAllLines(eventsPath)
	if err != nil {
		_ = writeErr(conn, ReasonTraceFileMissing, err.Error())
		return
	}
	filtered := filterLines(all, req.Kind)
	if req.Last > 0 && len(filtered) > req.Last {
		filtered = filtered[len(filtered)-req.Last:]
	}
	for _, l := range filtered {
		if err := writeLine(conn, l); err != nil {
			return
		}
	}
	if !req.Follow {
		_ = writeDone(conn)
		return
	}
	// Step 2: follow mode — poll for new bytes and stream them.
	offset := totalBytes(all)
	ticker := time.NewTicker(s.pollInterval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			_ = writeErr(conn, ReasonStreamCanceled, "server shutting down")
			return
		case <-ticker.C:
			newLines, newOffset, err := readSince(eventsPath, offset)
			if err != nil {
				_ = writeErr(conn, ReasonTraceFileMissing, err.Error())
				return
			}
			offset = newOffset
			for _, nl := range filterLines(newLines, req.Kind) {
				if err := writeLine(conn, nl); err != nil {
					return
				}
			}
		}
	}
}

func writeLine(w io.Writer, line string) error {
	resp := Response{Line: line}
	return writeJSON(w, resp)
}

func writeErr(w io.Writer, reason, message string) error {
	resp := Response{Error: &ErrorPayload{Reason: reason, Message: message}}
	return writeJSON(w, resp)
}

func writeDone(w io.Writer) error {
	return writeJSON(w, Response{Done: true})
}

func writeJSON(w io.Writer, resp Response) error {
	b, err := json.Marshal(resp)
	if err != nil {
		return err
	}
	b = append(b, '\n')
	_, err = w.Write(b)
	return err
}

func readAllLines(path string) ([]string, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	if len(b) == 0 {
		return nil, nil
	}
	out := strings.Split(strings.TrimRight(string(b), "\n"), "\n")
	return out, nil
}

func totalBytes(lines []string) int64 {
	var n int64
	for _, l := range lines {
		n += int64(len(l)) + 1
	}
	return n
}

func readSince(path string, offset int64) ([]string, int64, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, offset, err
	}
	defer f.Close()
	st, err := f.Stat()
	if err != nil {
		return nil, offset, err
	}
	size := st.Size()
	if size <= offset {
		return nil, offset, nil
	}
	if _, err := f.Seek(offset, io.SeekStart); err != nil {
		return nil, offset, err
	}
	scanner := bufio.NewScanner(f)
	scanner.Buffer(make([]byte, 64*1024), 1024*1024)
	var lines []string
	for scanner.Scan() {
		lines = append(lines, scanner.Text())
	}
	if err := scanner.Err(); err != nil {
		return nil, offset, err
	}
	return lines, size, nil
}

func filterLines(lines []string, kind string) []string {
	if kind == "" || kind == "all" {
		return lines
	}
	target := `"type":"` + kind + `"`
	out := make([]string, 0, len(lines))
	for _, l := range lines {
		if strings.Contains(l, target) {
			out = append(out, l)
		}
	}
	return out
}
