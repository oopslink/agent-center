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

func TestErrConnectFailed_Wraps(t *testing.T) {
	base := errors.New("dial broken")
	e := &peek.ErrConnectFailed{Cause: base}
	if !errors.Is(e, base) {
		t.Fatal("Unwrap broken")
	}
	if e.Error() == "" {
		t.Fatal("Error() empty")
	}
}

func TestServer_Addr(t *testing.T) {
	sock := fmt.Sprintf("/tmp/pk_%d_%d.sock", os.Getpid(), rand.Int63())
	t.Cleanup(func() { _ = os.Remove(sock) })
	srv, err := peek.NewServer(sock, t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer srv.Close()
	if srv.Addr() != sock {
		t.Fatalf("addr: %s", srv.Addr())
	}
}

func TestServer_Close_Idempotent(t *testing.T) {
	sock := fmt.Sprintf("/tmp/pk_%d_%d.sock", os.Getpid(), rand.Int63())
	t.Cleanup(func() { _ = os.Remove(sock) })
	srv, err := peek.NewServer(sock, t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	if err := srv.Close(); err != nil {
		t.Fatal(err)
	}
	// Second close should not panic.
	_ = srv.Close()
}

func TestServer_MalformedJSONRequest_Reply(t *testing.T) {
	root := t.TempDir()
	sock := fmt.Sprintf("/tmp/pk_%d_%d.sock", os.Getpid(), rand.Int63())
	t.Cleanup(func() { _ = os.Remove(sock) })
	srv, err := peek.NewServer(sock, root)
	if err != nil {
		t.Fatal(err)
	}
	defer srv.Close()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go func() { _ = srv.Serve(ctx) }()
	time.Sleep(50 * time.Millisecond)
	// Establish a raw connection to write garbage.
	conn, err := dial(sock)
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	_, _ = conn.Write([]byte("not-json\n"))
	buf := make([]byte, 256)
	n, _ := conn.Read(buf)
	if n == 0 {
		t.Fatal("no reply")
	}
	if !contains(string(buf[:n]), "invalid_request") {
		t.Fatalf("unexpected reply: %s", buf[:n])
	}
}

func TestProtocol_ReasonsExist(t *testing.T) {
	for _, r := range []string{
		peek.ReasonExecutionNotFound, peek.ReasonWorkerOffline,
		peek.ReasonWorkerNotOwner, peek.ReasonTraceFileMissing,
		peek.ReasonStreamCanceled, peek.ReasonInvalidRequest,
	} {
		if r == "" {
			t.Fatal("empty reason")
		}
	}
}

// dial is a tiny test helper using net.Dial.
func dial(p string) (closableConn, error) {
	c, err := netDial("unix", p)
	if err != nil {
		return nil, err
	}
	return c, nil
}

func contains(haystack, needle string) bool {
	return len(haystack) >= len(needle) && index(haystack, needle) >= 0
}

func index(s, sub string) int {
	for i := 0; i+len(sub) <= len(s); i++ {
		if s[i:i+len(sub)] == sub {
			return i
		}
	}
	return -1
}

// closableConn shrinks net.Conn surface.
type closableConn interface {
	Read([]byte) (int, error)
	Write([]byte) (int, error)
	Close() error
}

// netDial is a wrapper to avoid importing net at the top.
func netDial(network, addr string) (closableConn, error) {
	return netDialImpl(network, addr)
}

var _ = filepath.Base
