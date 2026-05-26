package api

import (
	"context"
	"crypto/sha256"
	"crypto/tls"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"path/filepath"
	"testing"
	"time"
)

// v2.3-7a (task #27): integration tests for the TCP+TLS admin listener
// added in this ST. Cover:
//   - TCP-only mode (socket empty, TCP set)
//   - Both modes simultaneously (existing tests cover unix-only)
//   - Both empty → boot error
//   - Cert fingerprint matches what client sees on dial

// dialTLS returns an http.Client that always dials the given TCP addr
// and trusts the server's cert by pinning the leaf-cert SHA256 (mirrors
// what the v2.3-7b client will eventually do).
func dialTLS(addr, expectedFingerprint string) *http.Client {
	return &http.Client{
		Transport: &http.Transport{
			DialTLSContext: func(ctx context.Context, _, _ string) (net.Conn, error) {
				conn, err := tls.Dial("tcp", addr, &tls.Config{
					InsecureSkipVerify: true, //nolint:gosec // fingerprint pinned below
				})
				if err != nil {
					return nil, err
				}
				state := conn.ConnectionState()
				if len(state.PeerCertificates) == 0 {
					_ = conn.Close()
					return nil, errors.New("no peer certs")
				}
				leaf := state.PeerCertificates[0]
				got := FormatFingerprint(leaf.Raw)
				if got != expectedFingerprint {
					_ = conn.Close()
					return nil, fmt.Errorf("fingerprint mismatch: got %q want %q", got, expectedFingerprint)
				}
				return conn, nil
			},
		},
		Timeout: 2 * time.Second,
	}
}

// waitForTCP polls until tcp dial succeeds.
func waitForTCP(t *testing.T, addr string, errCh <-chan error, timeout time.Duration) {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for {
		conn, err := net.Dial("tcp", addr)
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
			t.Fatalf("tcp %q never accepted dial within %s (last err=%v)", addr, timeout, err)
		}
		time.Sleep(5 * time.Millisecond)
	}
}

// freeTCPAddr returns a 127.0.0.1:<random-port> address known to be
// bind-able right now. There's a TOCTOU window between release + the
// caller's re-bind, but in test-loopback context that's never been a
// real issue.
func freeTCPAddr(t *testing.T) string {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	addr := ln.Addr().String()
	_ = ln.Close()
	return addr
}

func TestAdminServer_TCPOnly_Health(t *testing.T) {
	dir := t.TempDir()
	certPath := filepath.Join(dir, "tls.crt")
	keyPath := filepath.Join(dir, "tls.key")
	cert, fp, _, err := LoadOrGenerateCert(certPath, keyPath, "test-host")
	if err != nil {
		t.Fatal(err)
	}

	addr := freeTCPAddr(t)
	srv := NewServerWithTransports("" /* no unix */, addr, cert, fp, ServerDeps{})
	errCh := make(chan error, 1)
	go func() { errCh <- srv.ListenAndServe() }()
	defer func() { _ = srv.Shutdown(context.Background()) }()

	waitForTCP(t, addr, errCh, time.Second)
	cli := dialTLS(addr, fp)
	resp, err := cli.Get("https://" + addr + "/admin/health")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status=%d", resp.StatusCode)
	}
	body, _ := io.ReadAll(resp.Body)
	var m map[string]any
	if err := json.Unmarshal(body, &m); err != nil {
		t.Fatalf("decode: %v body=%s", err, body)
	}
	if m["ok"] != true {
		t.Fatalf("body=%s", body)
	}
}

func TestAdminServer_BothTransports_Health(t *testing.T) {
	sock := shortSocketPath(t, "both.sock")
	dir := t.TempDir()
	certPath := filepath.Join(dir, "tls.crt")
	keyPath := filepath.Join(dir, "tls.key")
	cert, fp, _, err := LoadOrGenerateCert(certPath, keyPath, "")
	if err != nil {
		t.Fatal(err)
	}
	addr := freeTCPAddr(t)
	srv := NewServerWithTransports(sock, addr, cert, fp, ServerDeps{})
	errCh := make(chan error, 1)
	go func() { errCh <- srv.ListenAndServe() }()
	defer func() { _ = srv.Shutdown(context.Background()) }()

	waitForSocket(t, sock, errCh, time.Second)
	waitForTCP(t, addr, errCh, time.Second)

	// Unix leg
	uResp, err := dialUnix(sock).Get("http://unix/admin/health")
	if err != nil {
		t.Fatalf("unix: %v", err)
	}
	defer uResp.Body.Close()
	if uResp.StatusCode != http.StatusOK {
		t.Fatalf("unix status=%d", uResp.StatusCode)
	}

	// TCP leg
	tResp, err := dialTLS(addr, fp).Get("https://" + addr + "/admin/health")
	if err != nil {
		t.Fatalf("tcp: %v", err)
	}
	defer tResp.Body.Close()
	if tResp.StatusCode != http.StatusOK {
		t.Fatalf("tcp status=%d", tResp.StatusCode)
	}
}

func TestAdminServer_BothEmpty_BootError(t *testing.T) {
	srv := NewServerWithTransports("", "", nil, "", ServerDeps{})
	err := srv.ListenAndServe()
	if err == nil || !errors.Is(err, errAtLeastOneListener) && !contains(err.Error(), "at least one") {
		t.Fatalf("expected at-least-one error, got %v", err)
	}
}

func TestAdminServer_TCPSetButCertNil_BootError(t *testing.T) {
	addr := freeTCPAddr(t)
	srv := NewServerWithTransports("", addr, nil, "", ServerDeps{})
	err := srv.ListenAndServe()
	if err == nil || !contains(err.Error(), "tlsCert is nil") {
		t.Fatalf("expected tlsCert-nil error, got %v", err)
	}
}

func TestAdminServer_TCPFingerprintRoundTrip(t *testing.T) {
	dir := t.TempDir()
	certPath := filepath.Join(dir, "c")
	keyPath := filepath.Join(dir, "k")
	cert, want, _, err := LoadOrGenerateCert(certPath, keyPath, "")
	if err != nil {
		t.Fatal(err)
	}
	// Verify that the leaf's raw DER SHA256 matches the fingerprint we
	// emit at boot, byte-for-byte. This is the client-pinning contract.
	sum := sha256.Sum256(cert.Certificate[0])
	gotPlain := ""
	for i, b := range sum {
		if i > 0 {
			gotPlain += ":"
		}
		gotPlain += fmt.Sprintf("%02X", b)
	}
	if want != "sha256:"+gotPlain {
		t.Fatalf("fingerprint mismatch: %q vs sha256:%s", want, gotPlain)
	}
}

func contains(s, sub string) bool {
	return len(s) >= len(sub) && indexOf(s, sub) >= 0
}

func indexOf(s, sub string) int {
	for i := 0; i+len(sub) <= len(s); i++ {
		if s[i:i+len(sub)] == sub {
			return i
		}
	}
	return -1
}

// errAtLeastOneListener is a stable sentinel for the all-empty case so
// tests can use errors.Is when the server signature evolves. v0
// implementation matches by substring; declared here as a no-op
// reference so a future refactor to errors.Is works.
var errAtLeastOneListener = errors.New("admin api: at least one of socket_path or tcp_listen required")
