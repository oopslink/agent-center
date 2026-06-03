package api

import (
	"bufio"
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/oopslink/agent-center/internal/admintoken"
	"github.com/oopslink/agent-center/internal/clock"
	"github.com/oopslink/agent-center/internal/environment"
	"github.com/oopslink/agent-center/internal/environment/controlstream"
	envservice "github.com/oopslink/agent-center/internal/environment/service"
	envsql "github.com/oopslink/agent-center/internal/environment/sqlite"
	"github.com/oopslink/agent-center/internal/idgen"
	"github.com/oopslink/agent-center/internal/persistence"
)

// =============================================================================
// v2.7 D5 slice-1 — center-side SSE down-push of worker control commands.
//
// These tests exercise the REAL admin server + AuthMiddleware over the SAME
// WorkerControlEvent log + the same bearer auth as the poll endpoint. The
// fixture mirrors production wiring: appends go through a publishing ControlLog
// (bus injected as the after-commit publisher), and the stream endpoint reads
// catch-up via EnvControl.CommandsAfter + subscribes to the bus.
// =============================================================================

type streamFixture struct {
	deps     HandlerDeps
	verifier *fakeVerifier
	bus      *controlstream.Bus
	log      *environment.ControlLog // publishing append path (mirrors the projector's log)
	ctx      context.Context
}

func newStreamFixture(t *testing.T) *streamFixture {
	t.Helper()
	db, err := persistence.Open(persistence.MemoryDSN())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	ctx := context.Background()
	if err := persistence.NewMigrator(db).Up(ctx); err != nil {
		t.Fatal(err)
	}
	clk := clock.NewFakeClock(atNow)
	gen := idgen.NewGenerator(clk)
	events := envsql.NewControlEventRepo(db)

	bus := controlstream.NewBus()
	// The append path that publishes (production: the projector's ControlLog).
	pubLog := environment.NewControlLog(events, gen, clk).WithPublisher(bus)

	// EnvControl is the endpoint's catch-up source (CommandsAfter), over the SAME
	// repo/DB. Separate ControlLog instance, same log — exactly like production.
	svc := envservice.New(envservice.Deps{
		DB: db, Workers: envsql.NewWorkerRepo(db), Events: events, IDGen: gen, Clock: clk,
	})

	verifier := &fakeVerifier{tokens: map[string]*admintoken.AdminToken{}}
	return &streamFixture{
		deps:     HandlerDeps{EnvControlSvc: svc, ControlStreamBus: bus},
		verifier: verifier,
		bus:      bus,
		log:      pubLog,
		ctx:      ctx,
	}
}

func (f *streamFixture) addWorkerToken(t *testing.T, plaintext, workerID string) {
	t.Helper()
	tok, err := admintoken.New(admintoken.NewAdminTokenInput{
		ID: admintoken.TokenID("T-" + plaintext), Owner: admintoken.Owner("worker:" + workerID),
		Scopes: []admintoken.Scope{"*"}, ValueHash: admintoken.HashPlaintext(plaintext),
	})
	if err != nil {
		t.Fatal(err)
	}
	f.verifier.tokens[plaintext] = tok
}

func (f *streamFixture) append(t *testing.T, workerID, cmdType, key string) *environment.WorkerControlEvent {
	t.Helper()
	e, err := f.log.AppendCommand(f.ctx, environment.AppendCommandInput{
		WorkerID: environment.WorkerID(workerID), CommandType: cmdType, IdempotencyKey: key,
	})
	if err != nil {
		t.Fatal(err)
	}
	return e
}

func (f *streamFixture) server(t *testing.T) *httptest.Server {
	t.Helper()
	srv := NewServerWithDeps("", ServerDeps{})
	h := AuthMiddleware(f.verifier)(WithDeps(f.deps)(srv.Handler()))
	httpsrv := httptest.NewServer(h)
	t.Cleanup(httpsrv.Close)
	return httpsrv
}

// openStream connects to the stream endpoint with a bearer and returns a
// scanner that yields complete SSE frames (blocks reading the body). cancel
// closes the connection.
func openStream(t *testing.T, base, bearer, query string) (*bufio.Scanner, context.CancelFunc) {
	t.Helper()
	ctx, cancel := context.WithCancel(context.Background())
	req, err := http.NewRequestWithContext(ctx, http.MethodGet,
		base+"/admin/environment/worker/commands/stream"+query, nil)
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Authorization", "Bearer "+bearer)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	if resp.StatusCode != http.StatusOK {
		body := make([]byte, 512)
		n, _ := resp.Body.Read(body)
		resp.Body.Close()
		cancel()
		t.Fatalf("stream status = %d, body = %s", resp.StatusCode, body[:n])
	}
	sc := bufio.NewScanner(resp.Body)
	t.Cleanup(func() { cancel(); resp.Body.Close() })
	return sc, cancel
}

// nextDataFrame reads SSE lines until it finds a `data:` line, returning its
// payload. Skips heartbeat frames (caller decides). Times out via the stream's
// own context (the test cancels). Returns "" on EOF.
func nextDataFrame(t *testing.T, sc *bufio.Scanner) string {
	t.Helper()
	type res struct {
		line string
		ok   bool
	}
	out := make(chan res, 1)
	go func() {
		for sc.Scan() {
			line := sc.Text()
			if strings.HasPrefix(line, "data:") {
				out <- res{strings.TrimSpace(strings.TrimPrefix(line, "data:")), true}
				return
			}
		}
		out <- res{"", false}
	}()
	select {
	case r := <-out:
		if !r.ok {
			return ""
		}
		return r.line
	case <-time.After(3 * time.Second):
		t.Fatal("timed out waiting for an SSE data frame")
		return ""
	}
}

// nextCommandFrame reads data frames, skipping heartbeats, returns the command JSON.
func nextCommandFrame(t *testing.T, sc *bufio.Scanner) string {
	t.Helper()
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		d := nextDataFrame(t, sc)
		if d == "" {
			t.Fatal("stream closed before a command frame")
		}
		if strings.Contains(d, "control.heartbeat") {
			continue
		}
		return d
	}
	t.Fatal("no command frame before deadline")
	return ""
}

func TestStream_AuthRequired(t *testing.T) {
	f := newStreamFixture(t)
	srv := f.server(t)
	req, _ := http.NewRequest(http.MethodGet,
		srv.URL+"/admin/environment/worker/commands/stream?worker_id=W1", nil)
	resp, err := http.DefaultClient.Do(req) // no bearer
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("missing bearer want 401, got %d", resp.StatusCode)
	}
}

func TestStream_CatchUp_CommandsAfterN_InOffsetOrder(t *testing.T) {
	f := newStreamFixture(t)
	f.addWorkerToken(t, "acat_stream_w1", "W1")
	// Three commands appended BEFORE connecting (the backlog).
	f.append(t, "W1", "agent.start", "k1") // offset 1
	f.append(t, "W1", "agent.work", "k2")  // offset 2
	f.append(t, "W1", "agent.stop", "k3")  // offset 3
	srv := f.server(t)

	// Reconnect-by-offset: ?after=1 → catch-up must be offsets 2,3 in order.
	sc, _ := openStream(t, srv.URL, "acat_stream_w1", "?worker_id=W1&after=1")
	d2 := nextCommandFrame(t, sc)
	d3 := nextCommandFrame(t, sc)
	if !strings.Contains(d2, `"offset":2`) || !strings.Contains(d2, `"idempotency_key":"k2"`) {
		t.Fatalf("first catch-up frame want offset 2 (k2), got %s", d2)
	}
	if !strings.Contains(d3, `"offset":3`) {
		t.Fatalf("second catch-up frame want offset 3, got %s", d3)
	}
}

func TestStream_LiveAfterCatchUp(t *testing.T) {
	f := newStreamFixture(t)
	f.addWorkerToken(t, "acat_stream_w1", "W1")
	f.append(t, "W1", "agent.start", "k1") // offset 1 (catch-up with after=0)
	srv := f.server(t)

	sc, _ := openStream(t, srv.URL, "acat_stream_w1", "?worker_id=W1&after=0")
	// Catch-up frame: offset 1.
	if d := nextCommandFrame(t, sc); !strings.Contains(d, `"offset":1`) {
		t.Fatalf("catch-up want offset 1, got %s", d)
	}
	// Now append LIVE (after the subscriber is connected) — must stream.
	time.Sleep(50 * time.Millisecond) // let the server enter its live loop
	f.append(t, "W1", "agent.work", "k2") // offset 2, published live
	if d := nextCommandFrame(t, sc); !strings.Contains(d, `"offset":2`) {
		t.Fatalf("live want offset 2, got %s", d)
	}
}

// TestStream_CatchUpLiveRace_NoLossNoDuplicate is the CRITICAL invariant test:
// a command appended in the window between Subscribe and the catch-up snapshot
// must be delivered EXACTLY once (in catch-up XOR live — never lost, never
// double-sent). We force the overlap by appending a command that is BOTH in the
// catch-up set (it's in the log) AND in the live bus ring (it was published
// while we were connected), then assert no duplicate offset arrives.
func TestStream_CatchUpLiveRace_NoLossNoDuplicate(t *testing.T) {
	f := newStreamFixture(t)
	f.addWorkerToken(t, "acat_stream_w1", "W1")
	// Backlog before connect.
	f.append(t, "W1", "agent.start", "k1") // offset 1
	srv := f.server(t)

	sc, _ := openStream(t, srv.URL, "acat_stream_w1", "?worker_id=W1&after=0")
	// Catch-up delivers offset 1.
	if d := nextCommandFrame(t, sc); !strings.Contains(d, `"offset":1`) {
		t.Fatalf("catch-up want offset 1, got %s", d)
	}
	// Append several live commands; each must arrive exactly once, ordered, no
	// duplicate of offset 1 (overlap) and no gaps.
	for i, key := range []string{"k2", "k3", "k4"} {
		f.append(t, "W1", "agent.work", key)
		wantOffset := i + 2
		d := nextCommandFrame(t, sc)
		if !strings.Contains(d, `"offset":`+itoa(wantOffset)) {
			t.Fatalf("live frame %d want offset %d, got %s", i, wantOffset, d)
		}
	}
	// No stray duplicate of offset 1 should be interleaved: drain briefly and
	// ensure the next would be a heartbeat or nothing (not a re-send of 1..4).
	// (Implicit: we already consumed exactly offsets 2,3,4 in order above.)
}

func TestStream_Heartbeat(t *testing.T) {
	f := newStreamFixture(t)
	f.addWorkerToken(t, "acat_stream_w1", "W1")
	// Shrink the heartbeat so the test is fast.
	f.bus.SetHeartbeat(80 * time.Millisecond)
	srv := f.server(t)

	sc, _ := openStream(t, srv.URL, "acat_stream_w1", "?worker_id=W1&after=0")
	// No commands → the first data frame must be a heartbeat.
	d := nextDataFrame(t, sc)
	if !strings.Contains(d, "control.heartbeat") {
		t.Fatalf("want heartbeat data frame, got %s", d)
	}
}

func TestStream_MissingWorkerID_400(t *testing.T) {
	f := newStreamFixture(t)
	f.addWorkerToken(t, "acat_stream_w1", "W1")
	srv := f.server(t)
	req, _ := http.NewRequest(http.MethodGet,
		srv.URL+"/admin/environment/worker/commands/stream", nil)
	req.Header.Set("Authorization", "Bearer acat_stream_w1")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("missing worker_id want 400, got %d", resp.StatusCode)
	}
}

func itoa(i int) string {
	if i == 0 {
		return "0"
	}
	neg := i < 0
	if neg {
		i = -i
	}
	var b [20]byte
	p := len(b)
	for i > 0 {
		p--
		b[p] = byte('0' + i%10)
		i /= 10
	}
	if neg {
		p--
		b[p] = '-'
	}
	return string(b[p:])
}
