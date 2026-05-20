package shim

import (
	"bytes"
	"context"
	"errors"
	"io"
	"strings"
	"testing"
	"time"

	"github.com/oopslink/agent-center/internal/agentadapter"
	"github.com/oopslink/agent-center/internal/agentadapter/claudecode"
	"github.com/oopslink/agent-center/internal/clock"
)

// stubProcess streams a fixed JSONL transcript and finishes.
type stubProcess struct {
	pid      int
	stdout   io.Reader
	stderr   io.Reader
	exit     int
	waitErr  error
	killed   bool
}

func (p *stubProcess) PID() int           { return p.pid }
func (p *stubProcess) Wait() (int, error) { return p.exit, p.waitErr }
func (p *stubProcess) Kill() error        { p.killed = true; return nil }
func (p *stubProcess) Stdout() io.Reader  { return p.stdout }
func (p *stubProcess) Stderr() io.Reader  { return p.stderr }

type stubSpawner struct {
	out  string
	err  error
	exit int
}

func (s stubSpawner) Spawn(_ context.Context, _ agentadapter.CmdSpec, _, _ io.Writer) (Process, error) {
	if s.err != nil {
		return nil, s.err
	}
	return &stubProcess{
		pid:    9999,
		stdout: strings.NewReader(s.out),
		stderr: bytes.NewReader(nil),
		exit:   s.exit,
	}, nil
}

func TestShim_New_Validation(t *testing.T) {
	cases := []struct {
		name string
		cfg  Config
	}{
		{"missing execution_id", Config{ShimToken: "t", Adapter: claudecode.New(""), Dir: &Dir{Root: "/x", ExecutionID: "E"}}},
		{"missing token", Config{ExecutionID: "E", Adapter: claudecode.New(""), Dir: &Dir{Root: "/x", ExecutionID: "E"}}},
		{"missing adapter", Config{ExecutionID: "E", ShimToken: "t", Dir: &Dir{Root: "/x", ExecutionID: "E"}}},
		{"missing dir", Config{ExecutionID: "E", ShimToken: "t", Adapter: claudecode.New("")}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if _, err := New(c.cfg); err == nil {
				t.Fatal("expected error")
			}
		})
	}
}

func TestShim_StartAndStreamEvents(t *testing.T) {
	tmp := t.TempDir()
	d, err := NewDir(tmp, "E-1")
	if err != nil {
		t.Fatal(err)
	}
	jsonl := `{"type":"thinking","text":"foo"}` + "\n" +
		`{"type":"end_turn"}` + "\n"
	cfg := Config{
		ExecutionID:  "E-1",
		ShimToken:    "tok",
		Adapter:      claudecode.New(""),
		Dir:          d,
		Spawner:      stubSpawner{out: jsonl},
		Clock:        clock.NewFakeClock(time.Now()),
		SpawnRequest: agentadapter.SpawnRequest{ExecutionID: "E-1", Prompt: "hi"},
	}
	s, err := New(cfg)
	if err != nil {
		t.Fatal(err)
	}
	if err := s.Start(context.Background(), []byte(`{}`)); err != nil {
		t.Fatal(err)
	}
	var captured []agentadapter.AgentTraceEvent
	if err := s.StreamEvents(context.Background(), agentadapter.NewUnknownEventReporter(agentadapter.DefaultReporterConfig()), func(ev agentadapter.AgentTraceEvent) {
		captured = append(captured, ev)
	}); err != nil {
		t.Fatal(err)
	}
	if len(captured) != 2 {
		t.Fatalf("captured: %d", len(captured))
	}
	if captured[0].Type != agentadapter.EventThinking || captured[1].Type != agentadapter.EventTurnEnd {
		t.Fatalf("types: %v", captured)
	}
	if captured[0].Seq != 1 || captured[1].Seq != 2 {
		t.Fatalf("seq: %d/%d", captured[0].Seq, captured[1].Seq)
	}
	// status.json should now be 'running'
	status, err := d.ReadStatus()
	if err != nil {
		t.Fatal(err)
	}
	if status.Phase != PhaseRunning {
		t.Fatalf("phase: %s", status.Phase)
	}
	count, _ := d.CountEvents()
	if count != 2 {
		t.Fatalf("event count: %d", count)
	}
}

func TestShim_SeqResumeAfterReconnect(t *testing.T) {
	tmp := t.TempDir()
	d, _ := NewDir(tmp, "E-1")
	s, _ := New(Config{
		ExecutionID:  "E-1",
		ShimToken:    "tok",
		Adapter:      claudecode.New(""),
		Dir:          d,
		Spawner:      stubSpawner{out: `{"type":"end_turn"}` + "\n"},
		Clock:        clock.NewFakeClock(time.Now()),
		SpawnRequest: agentadapter.SpawnRequest{ExecutionID: "E-1", Prompt: "hi"},
	})
	if err := s.Start(context.Background(), []byte(`{}`)); err != nil {
		t.Fatal(err)
	}
	s.SetSeq(42)
	_ = s.StreamEvents(context.Background(), nil, nil)
	if s.Seq() != 43 {
		t.Fatalf("seq after stream: %d", s.Seq())
	}
}

func TestShim_StreamEvents_ContextCancel(t *testing.T) {
	tmp := t.TempDir()
	d, _ := NewDir(tmp, "E-1")
	// stdout that blocks forever via pipe
	pr, _ := io.Pipe()
	stub := &stubProcess{pid: 1, stdout: pr, stderr: bytes.NewReader(nil)}
	cfg := Config{
		ExecutionID:  "E-1",
		ShimToken:    "tok",
		Adapter:      claudecode.New(""),
		Dir:          d,
		Spawner:      preStartedSpawner{p: stub},
		Clock:        clock.NewFakeClock(time.Now()),
		SpawnRequest: agentadapter.SpawnRequest{ExecutionID: "E-1", Prompt: "hi"},
	}
	s, _ := New(cfg)
	if err := s.Start(context.Background(), []byte(`{}`)); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() {
		done <- s.StreamEvents(ctx, nil, nil)
	}()
	cancel()
	// Force the scanner to read by closing the writer side… but scanner is
	// stuck in scanner.Scan(); we close the pipe to release the read.
	go func() { time.Sleep(20 * time.Millisecond); _ = pr.CloseWithError(io.EOF) }()
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("expected stream events to return on cancel/close")
	}
}

type preStartedSpawner struct{ p Process }

func (s preStartedSpawner) Spawn(context.Context, agentadapter.CmdSpec, io.Writer, io.Writer) (Process, error) {
	return s.p, nil
}

func TestShim_StreamEvents_ProcessNotStarted(t *testing.T) {
	s, _ := New(Config{
		ExecutionID: "E", ShimToken: "t", Adapter: claudecode.New(""), Dir: &Dir{Root: t.TempDir(), ExecutionID: "E"},
	})
	if err := s.StreamEvents(context.Background(), nil, nil); err == nil {
		t.Fatal("expected error")
	}
}

func TestShim_Close(t *testing.T) {
	d, _ := NewDir(t.TempDir(), "E-1")
	s, _ := New(Config{
		ExecutionID: "E-1", ShimToken: "t", Adapter: claudecode.New(""), Dir: d,
		Spawner: stubSpawner{out: ""}, Clock: clock.NewFakeClock(time.Now()),
		SpawnRequest: agentadapter.SpawnRequest{ExecutionID: "E-1", Prompt: "hi"},
	})
	if err := s.Start(context.Background(), []byte(`{}`)); err != nil {
		t.Fatal(err)
	}
	if err := s.Close(0); err != nil {
		t.Fatal(err)
	}
	status, _ := d.ReadStatus()
	if status.Phase != PhaseDone {
		t.Fatalf("phase: %s", status.Phase)
	}
}

func TestShim_SpawnerError(t *testing.T) {
	d, _ := NewDir(t.TempDir(), "E-1")
	s, _ := New(Config{
		ExecutionID: "E-1", ShimToken: "t", Adapter: claudecode.New(""), Dir: d,
		Spawner: stubSpawner{err: errors.New("boom")}, Clock: clock.NewFakeClock(time.Now()),
		SpawnRequest: agentadapter.SpawnRequest{ExecutionID: "E-1", Prompt: "hi"},
	})
	if err := s.Start(context.Background(), []byte(`{}`)); err == nil {
		t.Fatal("expected spawn error")
	}
}

func TestShim_BuildCommandError(t *testing.T) {
	d, _ := NewDir(t.TempDir(), "E-1")
	s, _ := New(Config{
		ExecutionID: "E-1", ShimToken: "t", Adapter: claudecode.New(""), Dir: d,
		Spawner: stubSpawner{},
		Clock:   clock.NewFakeClock(time.Now()),
		// SpawnRequest with no Prompt → BuildCommand will fail
		SpawnRequest: agentadapter.SpawnRequest{ExecutionID: "E-1"},
	})
	if err := s.Start(context.Background(), []byte(`{}`)); err == nil {
		t.Fatal("expected build command error")
	}
}

func TestShim_CopyAgentLog(t *testing.T) {
	src := strings.NewReader("hello")
	var dst bytes.Buffer
	if err := CopyAgentLog(&dst, src); err != nil {
		t.Fatal(err)
	}
	if dst.String() != "hello" {
		t.Fatalf("got %s", dst.String())
	}
}

func TestShim_ProcessGetter(t *testing.T) {
	d, _ := NewDir(t.TempDir(), "E-1")
	s, _ := New(Config{
		ExecutionID: "E-1", ShimToken: "t", Adapter: claudecode.New(""), Dir: d,
		Spawner: stubSpawner{out: ""}, Clock: clock.NewFakeClock(time.Now()),
		SpawnRequest: agentadapter.SpawnRequest{ExecutionID: "E-1", Prompt: "hi"},
	})
	if s.Process() != nil {
		t.Fatal("expected nil before start")
	}
	if err := s.Start(context.Background(), []byte(`{}`)); err != nil {
		t.Fatal(err)
	}
	if s.Process() == nil {
		t.Fatal("expected non-nil after start")
	}
}
