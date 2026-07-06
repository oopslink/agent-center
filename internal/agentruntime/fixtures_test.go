package agentruntime

import (
	"bufio"
	"bytes"
	"os"
	"path/filepath"
	"testing"
)

// The genuine claude 2.1.156 stream fixtures were moved to internal/claudestream
// (v2.7 D2-f s3b-1) alongside the parser they validate. The workerdaemon session
// / controller lifecycle tests still feed those REAL bytes through the fakeProc
// to exercise readLoop → OnEvent end-to-end, so we read them from the single
// source of truth in ../claudestream/testdata rather than duplicating the bytes.
func fixtureLines(t *testing.T) [][]byte {
	t.Helper()
	path := filepath.Join("..", "claudestream", "testdata", "claude_2.1.156_stream.jsonl")
	f, err := os.Open(path)
	if err != nil {
		t.Fatalf("open fixture: %v", err)
	}
	defer f.Close()
	var lines [][]byte
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 0, 64*1024), 4*1024*1024)
	for sc.Scan() {
		b := bytes.TrimSpace(sc.Bytes())
		if len(b) == 0 {
			continue
		}
		cp := make([]byte, len(b))
		copy(cp, b)
		lines = append(lines, cp)
	}
	if err := sc.Err(); err != nil {
		t.Fatalf("scan fixture: %v", err)
	}
	return lines
}

// Faithful Anthropic-shaped lines reused by the session / controller lifecycle
// tests (mirrors the constants in internal/claudestream).
const (
	toolUseLine       = `{"type":"assistant","message":{"role":"assistant","content":[{"type":"tool_use","id":"toolu_x","name":"Bash","input":{"command":"echo hi"}}]},"session_id":"s"}`
	assistantTextLine = `{"type":"assistant","message":{"role":"assistant","content":[{"type":"text","text":"PONG"}]},"session_id":"s"}`
)
