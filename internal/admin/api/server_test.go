package api

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"testing"
	"time"
)

// shortSocketPath returns a unix-socket-safe path. macOS caps socket
// paths at 104 bytes; `t.TempDir()` on darwin returns /var/folders/...
// which alone is ~80 bytes, leaving no room for a filename. We allocate
// the socket directly under /tmp with a unique short name and register
// cleanup. (Linux's 108-byte limit is also satisfied.)
func shortSocketPath(t *testing.T, name string) string {
	t.Helper()
	dir, err := os.MkdirTemp("/tmp", "ac-adm-")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(dir) })
	return filepath.Join(dir, name)
}

// waitForSocket polls up to timeout for the unix socket to accept a
// dial. Polling on file existence is not enough — a stale regular
// file at the same path stats OK before the server replaces it with
// a real listening socket. Dial loops on ENOENT / ECONNREFUSED until
// the real socket is up.
func waitForSocket(t *testing.T, sock string, errCh <-chan error, timeout time.Duration) {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for {
		conn, err := net.Dial("unix", sock)
		if err == nil {
			_ = conn.Close()
			return
		}
		select {
		case lserr := <-errCh:
			if lserr != nil && !errors.Is(lserr, http.ErrServerClosed) {
				t.Fatalf("ListenAndServe: %v", lserr)
			}
		default:
		}
		if time.Now().After(deadline) {
			t.Fatalf("socket %q never accepted dial within %s (last err=%v)", sock, timeout, err)
		}
		time.Sleep(5 * time.Millisecond)
	}
}

// dialUnix returns an http.Client that always dials the given unix
// socket regardless of the URL host.
func dialUnix(socketPath string) *http.Client {
	return &http.Client{
		Transport: &http.Transport{
			DialContext: func(ctx context.Context, _, _ string) (net.Conn, error) {
				return (&net.Dialer{}).DialContext(ctx, "unix", socketPath)
			},
		},
		Timeout: 2 * time.Second,
	}
}

func TestAdminServer_HealthOverUnixSocket(t *testing.T) {
	sock := shortSocketPath(t, "a.sock")
	srv := NewServer(sock)
	errCh := make(chan error, 1)
	go func() { errCh <- srv.ListenAndServe() }()
	defer func() { _ = srv.Shutdown(context.Background()) }()

	waitForSocket(t, sock, errCh, time.Second)

	client := dialUnix(sock)
	resp, err := client.Get("http://unix/admin/health")
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status=%d", resp.StatusCode)
	}
	body, _ := io.ReadAll(resp.Body)
	var got map[string]any
	if err := json.Unmarshal(body, &got); err != nil {
		t.Fatalf("decode: %v body=%s", err, body)
	}
	if got["ok"] != true || got["transport"] != "unix" || got["endpoint"] != "admin" {
		t.Fatalf("unexpected body: %v", got)
	}
}

func TestAdminServer_SocketPermissions(t *testing.T) {
	sock := shortSocketPath(t, "p.sock")
	srv := NewServer(sock)
	errCh := make(chan error, 1)
	go func() { errCh <- srv.ListenAndServe() }()
	defer func() { _ = srv.Shutdown(context.Background()) }()

	waitForSocket(t, sock, errCh, time.Second)
	info, err := os.Stat(sock)
	if err != nil {
		t.Fatal(err)
	}
	mode := info.Mode().Perm()
	if mode&0o077 != 0 {
		t.Fatalf("socket mode %o not owner-only (expected 0600 or stricter)", mode)
	}
}

func TestAdminServer_RemovesStaleSocketOnStart(t *testing.T) {
	sock := shortSocketPath(t, "s.sock")
	if err := os.WriteFile(sock, []byte("stale"), 0o600); err != nil {
		t.Fatal(err)
	}
	srv := NewServer(sock)
	errCh := make(chan error, 1)
	go func() { errCh <- srv.ListenAndServe() }()
	defer func() { _ = srv.Shutdown(context.Background()) }()

	waitForSocket(t, sock, errCh, time.Second)
	client := dialUnix(sock)
	resp, err := client.Get("http://unix/admin/health")
	if err != nil {
		t.Fatalf("get after stale removal: %v", err)
	}
	_ = resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status=%d", resp.StatusCode)
	}
}

func TestAdminServer_ShutdownRemovesSocket(t *testing.T) {
	sock := shortSocketPath(t, "d.sock")
	srv := NewServer(sock)
	errCh := make(chan error, 1)
	go func() { errCh <- srv.ListenAndServe() }()

	waitForSocket(t, sock, errCh, time.Second)
	if err := srv.Shutdown(context.Background()); err != nil {
		t.Fatalf("shutdown: %v", err)
	}
	if _, err := os.Stat(sock); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("socket should be removed after shutdown; got err=%v", err)
	}
}

func TestNewServer_EmptySocketPathRejected(t *testing.T) {
	srv := NewServer("")
	if err := srv.ListenAndServe(); err == nil {
		t.Fatal("expected error for empty socket path")
	}
}
