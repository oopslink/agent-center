package workerdaemon

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net"
	"net/http"
	"testing"
	"time"

	"github.com/oopslink/agent-center/internal/mcphost"
)

// agentToolRecorder is a tiny unix-socket admin stub that records the one
// request CallAgentTool makes and replies with a configurable status+body.
type agentToolRecorder struct {
	gotPath  string
	gotAuth  string
	gotBody  []byte
	respCode int
	respBody string
}

func startAgentToolServer(t *testing.T, rec *agentToolRecorder) (*AdminClient, func()) {
	t.Helper()
	sock := shortSock(t, "agenttool.sock")
	ln, err := net.Listen("unix", sock)
	if err != nil {
		t.Fatal(err)
	}
	mux := http.NewServeMux()
	mux.HandleFunc("/admin/agent-tools/", func(w http.ResponseWriter, r *http.Request) {
		rec.gotPath = r.URL.Path
		rec.gotAuth = r.Header.Get("Authorization")
		rec.gotBody, _ = io.ReadAll(r.Body)
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(rec.respCode)
		_, _ = io.WriteString(w, rec.respBody)
	})
	srv := &http.Server{Handler: mux}
	go func() { _ = srv.Serve(ln) }()

	deadline := time.Now().Add(2 * time.Second)
	for {
		c, derr := net.Dial("unix", sock)
		if derr == nil {
			_ = c.Close()
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("agentTool server: socket never ready: %v", derr)
		}
		time.Sleep(5 * time.Millisecond)
	}

	client := NewAdminClient(sock, 2*time.Second).WithToken("tok-123")
	cleanup := func() {
		_ = srv.Shutdown(context.Background())
		_ = ln.Close()
	}
	return client, cleanup
}

func TestCallAgentTool_Success(t *testing.T) {
	rec := &agentToolRecorder{respCode: http.StatusOK, respBody: `{"work_items":[]}`}
	client, cleanup := startAgentToolServer(t, rec)
	defer cleanup()

	var out json.RawMessage
	err := client.CallAgentTool(context.Background(), "get_my_work",
		map[string]any{"agent_id": "agent-1"}, &out)
	if err != nil {
		t.Fatalf("CallAgentTool: %v", err)
	}
	if rec.gotPath != "/admin/agent-tools/get_my_work" {
		t.Errorf("path = %q, want /admin/agent-tools/get_my_work", rec.gotPath)
	}
	if rec.gotAuth != "Bearer tok-123" {
		t.Errorf("auth header = %q, want Bearer tok-123", rec.gotAuth)
	}
	var body map[string]any
	if uerr := json.Unmarshal(rec.gotBody, &body); uerr != nil {
		t.Fatalf("server body not JSON: %v", uerr)
	}
	if body["agent_id"] != "agent-1" {
		t.Errorf("body agent_id = %v, want agent-1", body["agent_id"])
	}
	if string(out) != `{"work_items":[]}` {
		t.Errorf("raw out = %q, want canned body", string(out))
	}
}

func TestCallAgentTool_Non2xxTypedError(t *testing.T) {
	rec := &agentToolRecorder{respCode: http.StatusForbidden, respBody: `{"error":"forbidden"}`}
	client, cleanup := startAgentToolServer(t, rec)
	defer cleanup()

	var out json.RawMessage
	err := client.CallAgentTool(context.Background(), "post_message",
		map[string]any{"agent_id": "a", "target": map[string]any{"type": "task", "id": "t"}, "text": "x"}, &out)
	if err == nil {
		t.Fatal("want error on 403")
	}
	var adminErr *mcphost.AdminToolError
	if !errors.As(err, &adminErr) {
		t.Fatalf("want *mcphost.AdminToolError, got %T: %v", err, err)
	}
	if adminErr.Status != http.StatusForbidden {
		t.Errorf("status = %d, want 403", adminErr.Status)
	}
	if adminErr.Body != `{"error":"forbidden"}` {
		t.Errorf("body = %q, want forbidden body", adminErr.Body)
	}
}
