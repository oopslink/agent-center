package agentruntime

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"net/http"
	"os"
	"strings"
	"testing"
	"time"
)

// scriptedInflightCaller is a fake ToolCaller that records the call and returns a
// scripted raw response (or error), so the parsing/wiring of ListMyInflightTasks is
// tested without a real transport.
type scriptedInflightCaller struct {
	gotTool string
	gotBody any
	resp    string
	err     error
}

func (s *scriptedInflightCaller) CallAgentTool(_ context.Context, tool string, body any, out *json.RawMessage) error {
	s.gotTool = tool
	s.gotBody = body
	if s.err != nil {
		return s.err
	}
	if out != nil {
		*out = append((*out)[:0], []byte(s.resp)...)
	}
	return nil
}

// ListMyInflightTasks posts to list_my_inflight_tasks with the agent_id and parses the
// full (unfiltered) task set — a running task with unsatisfied deps (t-2, open) is kept.
func TestListMyInflightTasks_ParsesUnfilteredSet(t *testing.T) {
	sc := &scriptedInflightCaller{resp: `{"tasks":[
		{"task_id":"t-1","title":"A","status":"running","blocked_reason":"","blocked_reason_type":"","blocked_comment":"","lease_expires_at":"2026-07-04T00:00:00Z"},
		{"task_id":"t-2","title":"B","status":"open"}
	]}`}
	got, err := NewInflightTaskLister(sc).ListMyInflightTasks(context.Background(), "agent-1")
	if err != nil {
		t.Fatalf("ListMyInflightTasks: %v", err)
	}
	if sc.gotTool != "list_my_inflight_tasks" {
		t.Fatalf("tool = %q, want list_my_inflight_tasks", sc.gotTool)
	}
	if m, _ := sc.gotBody.(map[string]any); m["agent_id"] != "agent-1" {
		t.Fatalf("body agent_id = %v, want agent-1", sc.gotBody)
	}
	if len(got) != 2 {
		t.Fatalf("len = %d, want 2: %+v", len(got), got)
	}
	if got[0].TaskID != "t-1" || got[0].Status != "running" || got[0].LeaseExpiresAt != "2026-07-04T00:00:00Z" {
		t.Fatalf("task[0] = %+v", got[0])
	}
	if got[1].TaskID != "t-2" || got[1].Status != "open" {
		t.Fatalf("task[1] = %+v", got[1])
	}
}

// An empty task set decodes to a non-nil empty slice (never nil) so callers can range
// without a nil guard.
func TestListMyInflightTasks_EmptyResponse(t *testing.T) {
	got, err := NewInflightTaskLister(&scriptedInflightCaller{resp: `{"tasks":[]}`}).
		ListMyInflightTasks(context.Background(), "a")
	if err != nil {
		t.Fatalf("err = %v", err)
	}
	if got == nil || len(got) != 0 {
		t.Fatalf("want empty non-nil slice, got %+v", got)
	}
}

// A transport error propagates verbatim.
func TestListMyInflightTasks_CallerError(t *testing.T) {
	_, err := NewInflightTaskLister(&scriptedInflightCaller{err: errors.New("boom")}).
		ListMyInflightTasks(context.Background(), "a")
	if err == nil {
		t.Fatal("want error, got nil")
	}
}

// A nil caller yields a nil lister (graceful-degrade contract, mirrors newCenterClient).
func TestNewInflightTaskLister_NilCaller(t *testing.T) {
	if NewInflightTaskLister(nil) != nil {
		t.Fatal("nil caller must yield a nil lister")
	}
}

// NewCenterHTTPClient rejects an empty admin URL (no silent nil-target build).
func TestNewCenterHTTPClient_EmptyURL(t *testing.T) {
	if _, err := NewCenterHTTPClient("", "", "tok", 0); err == nil {
		t.Fatal("want error for empty admin_url")
	}
}

// The self-built CenterHTTPClient really talks HTTP over the same transport the daemon
// uses: it POSTs to /admin/agent-tools/<tool> with the worker bearer + JSON body, and
// parses the raw response. Exercised over a unix socket (no TLS/fingerprint needed).
func TestCenterHTTPClient_CallAgentTool_UnixSocket(t *testing.T) {
	sock := fmt.Sprintf("/tmp/t842-inflight-%d.sock", os.Getpid())
	_ = os.Remove(sock)
	ln, err := net.Listen("unix", sock)
	if err != nil {
		t.Fatalf("listen unix: %v", err)
	}
	defer func() { _ = ln.Close(); _ = os.Remove(sock) }()

	var gotPath, gotAuth, gotAgent string
	srv := &http.Server{Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		gotAuth = r.Header.Get("Authorization")
		var body struct {
			AgentID string `json:"agent_id"`
		}
		_ = json.NewDecoder(r.Body).Decode(&body)
		gotAgent = body.AgentID
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"tasks":[{"task_id":"t-9","status":"running"}]}`))
	})}
	go func() { _ = srv.Serve(ln) }()
	defer func() { _ = srv.Close() }()

	c, err := NewCenterHTTPClient("unix:"+sock, "", "worker-tok", 5*time.Second)
	if err != nil {
		t.Fatalf("NewCenterHTTPClient: %v", err)
	}
	got, err := NewInflightTaskLister(c).ListMyInflightTasks(context.Background(), "agent-7")
	if err != nil {
		t.Fatalf("ListMyInflightTasks over unix socket: %v", err)
	}
	if gotPath != "/admin/agent-tools/list_my_inflight_tasks" {
		t.Errorf("path = %q", gotPath)
	}
	if gotAuth != "Bearer worker-tok" {
		t.Errorf("auth = %q, want Bearer worker-tok", gotAuth)
	}
	if gotAgent != "agent-7" {
		t.Errorf("agent_id = %q, want agent-7", gotAgent)
	}
	if len(got) != 1 || got[0].TaskID != "t-9" {
		t.Errorf("parsed = %+v", got)
	}
}

// A non-2xx from the center surfaces as an error carrying the status (self-built path).
func TestCenterHTTPClient_Non2xx_Errors(t *testing.T) {
	sock := fmt.Sprintf("/tmp/t842-inflight-err-%d.sock", os.Getpid())
	_ = os.Remove(sock)
	ln, err := net.Listen("unix", sock)
	if err != nil {
		t.Fatalf("listen unix: %v", err)
	}
	defer func() { _ = ln.Close(); _ = os.Remove(sock) }()

	srv := &http.Server{Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusForbidden)
		_, _ = w.Write([]byte(`{"error":"forbidden"}`))
	})}
	go func() { _ = srv.Serve(ln) }()
	defer func() { _ = srv.Close() }()

	c, err := NewCenterHTTPClient("unix:"+sock, "", "tok", 5*time.Second)
	if err != nil {
		t.Fatalf("NewCenterHTTPClient: %v", err)
	}
	if err := c.CallAgentTool(context.Background(), "list_my_inflight_tasks", map[string]any{"agent_id": "a"}, nil); err == nil {
		t.Fatal("want error on 403, got nil")
	} else if !strings.Contains(err.Error(), "403") {
		t.Fatalf("error missing status: %v", err)
	}
}
