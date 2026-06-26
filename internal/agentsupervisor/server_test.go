package agentsupervisor_test

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/oopslink/agent-center/internal/agentsupervisor"
)

// shortTempDir returns a temp directory short enough for Unix domain sockets
// on macOS (sun_path max 104 bytes). t.TempDir() paths on macOS exceed this
// when combined with "supervisor.sock".
func shortTempDir(t *testing.T) string {
	t.Helper()
	if runtime.GOOS != "darwin" {
		return t.TempDir()
	}
	dir, err := os.MkdirTemp("/tmp", "acsock")
	if err != nil {
		t.Fatalf("shortTempDir: %v", err)
	}
	t.Cleanup(func() { os.RemoveAll(dir) })
	return dir
}

// echoChildScript is a stand-in claude (NO real claude): it drains its own
// stdin line-by-line and, for every injected line, emits ONE valid stream-json
// event whose nested text echoes a marker so a test can observe inject reaching
// the held-open stdin end-to-end. It never blocks and runs until killed.
//
// The injected line is the stream-json user envelope encodeUserMessage produces;
// we just need to prove it ARRIVED on stdin, so we emit a system event tagged
// with a monotonically increasing counter plus the raw injected bytes length —
// but to make the assertion robust against JSON-in-JSON quoting we emit a
// dedicated "injected" subtype and let the test match on that. We also tick
// independently so read-from-offset has a steady supply.
const echoChildScript = `
i=0
while IFS= read -r line; do
  i=$((i+1))
  printf '{"type":"system","subtype":"injected","n":%d}\n' "$i"
done
`

// tickChildScript drains stdin and emits a stream-json line per tick, so the
// drain has a steady supply with no consumer. Used by read/ack tests.
const tickChildScript = `
cat >/dev/null &
i=0
while true; do
  i=$((i+1))
  printf '{"type":"system","subtype":"tick","n":%d}\n' "$i"
  sleep 0.02
done
`

func startSupervisor(t *testing.T, home string, script string) *agentsupervisor.Supervisor {
	t.Helper()
	sup, err := agentsupervisor.New(agentsupervisor.Config{
		AgentID:  "agent-sock",
		HomeDir:  home,
		ChildCmd: []string{"sh", "-c", script},
		Logger:   func(string) {},
	})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	if err := sup.Start(); err != nil {
		t.Fatalf("Start: %v", err)
	}
	t.Cleanup(func() { _ = sup.Stop(false) })
	return sup
}

func serveAndConnect(t *testing.T, sup *agentsupervisor.Supervisor, home string) (*agentsupervisor.AttachClient, context.CancelFunc) {
	t.Helper()
	ctx, cancel := context.WithCancel(context.Background())
	sock := filepath.Join(home, agentsupervisor.DefaultSocketName)
	serveErr := make(chan error, 1)
	go func() { serveErr <- sup.Serve(ctx, sock) }()

	// Wait for the socket to come up, then dial.
	var cli *agentsupervisor.AttachClient
	deadline := time.Now().Add(3 * time.Second)
	for {
		c, err := agentsupervisor.Connect(ctx, sock)
		if err == nil {
			cli = c
			break
		}
		if time.Now().After(deadline) {
			cancel()
			t.Fatalf("could not connect to socket: %v", err)
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Cleanup(func() {
		_ = cli.Close()
		cancel()
		<-serveErr
	})
	return cli, cancel
}

// TestServer_Hello: hello returns ProtocolVersion + identity + offsets.
func TestServer_Hello(t *testing.T) {
	home := t.TempDir()
	sup := startSupervisor(t, home, tickChildScript)
	cli, _ := serveAndConnect(t, sup, home)

	h, err := cli.Hello(context.Background())
	if err != nil {
		t.Fatalf("Hello: %v", err)
	}
	if h.ProtocolVersion != agentsupervisor.ProtocolVersion {
		t.Fatalf("protocol version=%d want %d", h.ProtocolVersion, agentsupervisor.ProtocolVersion)
	}
	if h.InstanceID != sup.InstanceID() {
		t.Fatalf("instance id=%q want %q", h.InstanceID, sup.InstanceID())
	}
	if h.AgentID != "agent-sock" {
		t.Fatalf("agent id=%q", h.AgentID)
	}
	if h.ChildPID != sup.ChildPID() || h.ChildPID == 0 {
		t.Fatalf("child pid=%d want %d", h.ChildPID, sup.ChildPID())
	}
	if h.StartedAt == "" {
		t.Fatalf("started_at empty")
	}
	if h.BaseOffset != 0 {
		t.Fatalf("base offset=%d want 0", h.BaseOffset)
	}
}

// TestServer_InjectReachesStdin proves client.Inject reaches claude's held-open
// stdin: the stand-in child emits an "injected" event per stdin line, which
// appears in events.jsonl / via read.
func TestServer_InjectReachesStdin(t *testing.T) {
	home := shortTempDir(t)
	sup := startSupervisor(t, home, echoChildScript)
	cli, _ := serveAndConnect(t, sup, home)

	if err := cli.Inject(context.Background(), "ping"); err != nil {
		t.Fatalf("Inject: %v", err)
	}

	// Poll read until the injected event shows up.
	deadline := time.Now().Add(3 * time.Second)
	for {
		data, _, _, err := cli.ReadFrom(context.Background(), 0, 1<<20)
		if err != nil {
			t.Fatalf("ReadFrom: %v", err)
		}
		if strings.Contains(string(data), `"subtype":"injected"`) {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("injected event never appeared; got %q", string(data))
		}
		time.Sleep(20 * time.Millisecond)
	}
}

// TestServer_ReadFromOffset: drain produces events; ReadFrom(0) returns them,
// next advances, eof true when caught up, reading from next returns new events.
func TestServer_ReadFromOffset(t *testing.T) {
	home := shortTempDir(t)
	sup := startSupervisor(t, home, tickChildScript)
	cli, _ := serveAndConnect(t, sup, home)

	// Wait until some events exist.
	var first []byte
	var next int64
	deadline := time.Now().Add(3 * time.Second)
	for {
		data, n, _, err := cli.ReadFrom(context.Background(), 0, 1<<20)
		if err != nil {
			t.Fatalf("ReadFrom(0): %v", err)
		}
		if len(data) > 0 {
			first, next = data, n
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("no events produced")
		}
		time.Sleep(20 * time.Millisecond)
	}
	if next != int64(len(first)) {
		t.Fatalf("next=%d want len(first)=%d (base 0)", next, len(first))
	}
	if !strings.Contains(string(first), `"type":"system"`) {
		t.Fatalf("first read not stream-json: %q", string(first))
	}

	// Reading again from next eventually returns NEW events with a larger next.
	deadline = time.Now().Add(3 * time.Second)
	for {
		data, n2, _, err := cli.ReadFrom(context.Background(), next, 1<<20)
		if err != nil {
			t.Fatalf("ReadFrom(next): %v", err)
		}
		if len(data) > 0 {
			if n2 <= next {
				t.Fatalf("next did not advance: n2=%d next=%d", n2, next)
			}
			return
		}
		if time.Now().After(deadline) {
			t.Fatalf("no fresh events after offset %d", next)
		}
		time.Sleep(20 * time.Millisecond)
	}
}

// TestServer_AckTruncateOffsetStability is the 4b core: after reading up to N,
// Ack(N) shrinks events.jsonl on disk (base==N), a subsequent ReadFrom(N) still
// works on the SAME absolute-offset stream, ReadFrom(<N) → offset_truncated, and
// the file does NOT grow unbounded across many ack cycles.
func TestServer_AckTruncateOffsetStability(t *testing.T) {
	home := shortTempDir(t)
	sup := startSupervisor(t, home, tickChildScript)
	cli, _ := serveAndConnect(t, sup, home)
	eventsPath := filepath.Join(home, agentsupervisor.EventsFileName)

	readUntil := func(min int64) int64 {
		t.Helper()
		deadline := time.Now().Add(3 * time.Second)
		for {
			_, n, _, err := cli.ReadFrom(context.Background(), min, 1<<20)
			if err != nil {
				t.Fatalf("ReadFrom: %v", err)
			}
			if n > min {
				return n
			}
			if time.Now().After(deadline) {
				t.Fatalf("events did not advance past %d", min)
			}
			time.Sleep(20 * time.Millisecond)
		}
	}

	// Read up to N, ack N.
	var base int64
	var maxDiskSize int64
	var lastAbsOffset int64
	for cycle := 0; cycle < 20; cycle++ {
		n := readUntil(base)
		gotBase, err := cli.Ack(context.Background(), n)
		if err != nil {
			t.Fatalf("Ack(%d): %v", n, err)
		}
		if gotBase != n {
			t.Fatalf("ack base=%d want %d", gotBase, n)
		}
		base = n
		lastAbsOffset = n

		// On-disk file shrinks: it holds only [base, current) so its size is
		// current-base, which stays small (bounded), NOT the absolute offset.
		fi := statSize(t, eventsPath)
		if fi > maxDiskSize {
			maxDiskSize = fi
		}
	}

	// Absolute offsets stayed stable + grew well past the disk file size: the
	// file is bounded while offsets are absolute from life-start.
	if lastAbsOffset <= maxDiskSize {
		t.Fatalf("offsets not absolute/stable: lastAbsOffset=%d maxDiskSize=%d", lastAbsOffset, maxDiskSize)
	}
	if maxDiskSize > 1<<16 {
		t.Fatalf("events.jsonl grew unbounded across ack cycles: maxDiskSize=%d", maxDiskSize)
	}

	// ReadFrom(base) still works on the same absolute stream.
	_, _, _, err := cli.ReadFrom(context.Background(), base, 1<<20)
	if err != nil {
		t.Fatalf("ReadFrom(base) after ack: %v", err)
	}

	// ReadFrom below base → offset_truncated.
	if base > 0 {
		_, _, _, err := cli.ReadFrom(context.Background(), base-1, 1<<20)
		if !errors.Is(err, agentsupervisor.ErrOffsetTruncated) {
			t.Fatalf("ReadFrom(<base) err=%v want ErrOffsetTruncated", err)
		}
	}
}

// (TestVersionCompat removed in v2.7: the cross-version gate / IsCompatible was
// dropped — the protocol is assumed backward-compatible. The version-removal
// regression is covered by supervisormanager's
// TestProbeAgent_DifferentVersionStillReattachable.)

// TestServer_Concurrency: concurrent Inject + ReadFrom + drain appends → no
// race (run under -race), offsets monotonic, events readable.
func TestServer_Concurrency(t *testing.T) {
	home := t.TempDir()
	sup := startSupervisor(t, home, echoChildScript)
	cli, _ := serveAndConnect(t, sup, home)

	// Each goroutine uses its OWN connection (the client is single-flight per
	// conn). The injector runs UNBOUNDED until stop; the reader is bounded. We
	// wait ONLY on the reader, then close stop and join the injector — closing
	// stop before waiting would let the injector outlive nothing, and waiting on
	// the injector before closing stop would deadlock.
	stop := make(chan struct{})
	var injDone sync.WaitGroup
	injDone.Add(1)
	go func() {
		defer injDone.Done()
		for {
			select {
			case <-stop:
				return
			default:
				_ = cli.Inject(context.Background(), "x")
				time.Sleep(time.Millisecond)
			}
		}
	}()

	rc, err := agentsupervisor.Connect(context.Background(), filepath.Join(home, agentsupervisor.DefaultSocketName))
	if err != nil {
		t.Fatalf("Connect reader: %v", err)
	}
	defer rc.Close()

	var readDone sync.WaitGroup
	readDone.Add(1)
	go func() {
		defer readDone.Done()
		var off int64
		var prev int64
		for i := 0; i < 200; i++ {
			data, n, _, err := rc.ReadFrom(context.Background(), off, 4096)
			if err != nil {
				t.Errorf("ReadFrom: %v", err)
				return
			}
			if n < prev {
				t.Errorf("offset went backwards: n=%d prev=%d", n, prev)
				return
			}
			prev = n
			if len(data) > 0 {
				off = n
			}
			// THREE-WAY race (PM focus): drain APPENDS while we ACK-TRUNCATE while
			// reading. Ack(off) rewrites the events.jsonl prefix concurrently with
			// the drain's append — the offMu-serialized rewrite must not corrupt
			// the append or the absolute offsets. Ack up to where we've read.
			if i%10 == 9 && off > 0 {
				if _, err := rc.Ack(context.Background(), off); err != nil {
					t.Errorf("concurrent Ack(%d): %v", off, err)
					return
				}
			}
			time.Sleep(time.Millisecond)
		}
	}()

	readDone.Wait()
	close(stop)
	injDone.Wait()

	// After concurrent acks the front of events.jsonl is truncated, so read from
	// the current base_offset (reading from 0 would be offset_truncated — itself a
	// correctness signal that truncation happened). Final read returns valid
	// content and a sane offset.
	h, err := rc.Hello(context.Background())
	if err != nil {
		t.Fatalf("final Hello: %v", err)
	}
	data, n, _, err := rc.ReadFrom(context.Background(), h.BaseOffset, 1<<20)
	if err != nil {
		t.Fatalf("final ReadFrom(base=%d): %v", h.BaseOffset, err)
	}
	if n < h.BaseOffset {
		t.Fatalf("bad final offset %d (base %d)", n, h.BaseOffset)
	}
	if len(data) > 0 && !strings.Contains(string(data), `"type":"system"`) {
		t.Fatalf("final read not stream-json: %q", string(data))
	}
}

// TestServer_OversizeFrame: an oversize frame is rejected and the supervisor
// stays up (a fresh connection still works).
func TestServer_OversizeFrame(t *testing.T) {
	home := t.TempDir()
	sup := startSupervisor(t, home, tickChildScript)
	cli, _ := serveAndConnect(t, sup, home)
	sock := filepath.Join(home, agentsupervisor.DefaultSocketName)

	// Send a raw oversize length prefix (> 16 MiB) on a separate raw connection.
	raw, err := dialRaw(sock)
	if err != nil {
		t.Fatalf("dial raw: %v", err)
	}
	// 4-byte big-endian length = 0xFFFFFFFF, no body.
	if _, err := raw.Write([]byte{0xFF, 0xFF, 0xFF, 0xFF}); err != nil {
		t.Fatalf("write oversize header: %v", err)
	}
	// The server should drop this connection without replying / crashing.
	raw.Close()

	// Supervisor still up: the original client's Hello still works.
	if _, err := cli.Hello(context.Background()); err != nil {
		t.Fatalf("Hello after oversize frame: %v", err)
	}
	_ = sup
}
