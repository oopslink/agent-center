package client

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
)

// FakeServer is an HTTP stub of the feishu open API used by integration +
// e2e tests. It records every received request and lets the test inject
// canned responses (success / 4xx / 5xx) per endpoint.
//
// Lifecycle:
//
//	fs := NewFakeServer()
//	defer fs.Close()
//	cfg := AdapterConfig{BaseURL: fs.URL(), AppID: "x", AppSecret: "y", ...}
//	adapter := NewOAPIAdapter(cfg)
//	adapter.Connect(ctx)
//
// The server is intentionally minimal — it serves only the surface the v1
// adapter needs:
//   - POST /open-apis/auth/v3/tenant_access_token/internal
//   - POST /open-apis/im/v1/messages?receive_id_type=...
type FakeServer struct {
	server *httptest.Server
	mu     sync.Mutex

	// Behaviour switches the test sets.
	connectAuthFails    bool
	connect5xxRemaining int
	sendPermFails       bool
	send5xxRemaining    int

	// Auto-incremented to give each send a unique vendor msg id.
	msgCounter int

	// Recordings.
	sends []SentRecord
}

// SentRecord captures a recorded outbound POST.
type SentRecord struct {
	URL          string
	ReceiveID    string
	ReceiveIDKey string // "chat_id" / "open_id"
	MsgType      string // "text" / "interactive"
	Content      string // raw JSON content
}

// NewFakeServer starts a fresh stub.
func NewFakeServer() *FakeServer {
	fs := &FakeServer{}
	fs.server = httptest.NewServer(http.HandlerFunc(fs.dispatch))
	return fs
}

// URL is the base URL to plug into AdapterConfig.BaseURL.
func (f *FakeServer) URL() string { return f.server.URL }

// Close stops the underlying httptest server.
func (f *FakeServer) Close() { f.server.Close() }

// Sends returns a snapshot of recorded outbound messages.
func (f *FakeServer) Sends() []SentRecord {
	f.mu.Lock()
	defer f.mu.Unlock()
	out := make([]SentRecord, len(f.sends))
	copy(out, f.sends)
	return out
}

// SendCount returns how many messages have been recorded.
func (f *FakeServer) SendCount() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return len(f.sends)
}

// SetAuthFails toggles tenant_access_token auth failure (4xx).
func (f *FakeServer) SetAuthFails(v bool) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.connectAuthFails = v
}

// SetConnect5xx makes the next N tenant_access_token calls return 500.
func (f *FakeServer) SetConnect5xx(n int) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.connect5xxRemaining = n
}

// SetSendPermFails toggles 4xx on messages.create (permanent failure).
func (f *FakeServer) SetSendPermFails(v bool) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.sendPermFails = v
}

// SetSend5xx makes the next N messages.create calls return 500.
func (f *FakeServer) SetSend5xx(n int) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.send5xxRemaining = n
}

func (f *FakeServer) dispatch(w http.ResponseWriter, r *http.Request) {
	switch {
	case strings.Contains(r.URL.Path, "/auth/v3/tenant_access_token/internal"):
		f.handleAuth(w, r)
	case strings.Contains(r.URL.Path, "/im/v1/messages"):
		f.handleSendMessage(w, r)
	default:
		w.WriteHeader(http.StatusNotFound)
		_, _ = w.Write([]byte(`{"code":404,"msg":"unknown endpoint"}`))
	}
}

func (f *FakeServer) handleAuth(w http.ResponseWriter, r *http.Request) {
	f.mu.Lock()
	if f.connect5xxRemaining > 0 {
		f.connect5xxRemaining--
		f.mu.Unlock()
		w.WriteHeader(http.StatusInternalServerError)
		_, _ = w.Write([]byte(`{"code":500,"msg":"transient"}`))
		return
	}
	authFails := f.connectAuthFails
	f.mu.Unlock()
	if authFails {
		w.WriteHeader(http.StatusUnauthorized)
		_, _ = w.Write([]byte(`{"code":99991663,"msg":"app secret invalid"}`))
		return
	}
	w.Header().Set("Content-Type", "application/json")
	_, _ = w.Write([]byte(`{"code":0,"msg":"ok","tenant_access_token":"t-fake-token","expire":7200}`))
}

func (f *FakeServer) handleSendMessage(w http.ResponseWriter, r *http.Request) {
	body, _ := io.ReadAll(r.Body)
	var parsed struct {
		ReceiveID string `json:"receive_id"`
		MsgType   string `json:"msg_type"`
		Content   string `json:"content"`
	}
	_ = json.Unmarshal(body, &parsed)
	receiveType := r.URL.Query().Get("receive_id_type")

	f.mu.Lock()
	if f.send5xxRemaining > 0 {
		f.send5xxRemaining--
		f.mu.Unlock()
		w.WriteHeader(http.StatusInternalServerError)
		_, _ = w.Write([]byte(`{"code":500,"msg":"transient"}`))
		return
	}
	if f.sendPermFails {
		f.mu.Unlock()
		w.WriteHeader(http.StatusBadRequest)
		_, _ = w.Write([]byte(`{"code":230002,"msg":"invalid content"}`))
		return
	}
	f.msgCounter++
	msgID := fmt.Sprintf("om_fake_%d", f.msgCounter)
	chatID := parsed.ReceiveID
	if chatID == "" || receiveType == "open_id" {
		// fresh thread: synthesize a chat id for the first DM.
		chatID = fmt.Sprintf("oc_fake_%d", f.msgCounter)
	}
	f.sends = append(f.sends, SentRecord{
		URL:          r.URL.String(),
		ReceiveID:    parsed.ReceiveID,
		ReceiveIDKey: receiveType,
		MsgType:      parsed.MsgType,
		Content:      parsed.Content,
	})
	f.mu.Unlock()

	w.Header().Set("Content-Type", "application/json")
	resp := map[string]any{
		"code": 0,
		"msg":  "ok",
		"data": map[string]any{
			"message_id": msgID,
			"chat_id":    chatID,
		},
	}
	_ = json.NewEncoder(w).Encode(resp)
}
