package workerdaemon

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/oopslink/agent-center/internal/clock"
)

type fakeStartTimer struct {
	start time.Time
	err   error
}

func (f fakeStartTimer) GetStartTime(_ int) (time.Time, error) {
	return f.start, f.err
}

func TestShimSupervisor_HelloTimeout(t *testing.T) {
	clk := clock.NewFakeClock(time.Date(2026, 5, 21, 12, 0, 0, 0, time.UTC))
	uploader := &recordingUploader{}
	sup := NewShimSupervisor(nil, clk, 60*time.Second, uploader)
	sup.Register(ShimRecord{
		ExecutionID:    "E-1",
		ShimPID:        100,
		ShimStartTime:  clk.Now(),
		HelloReceived:  false,
		SpawnedAt:      clk.Now(),
	})
	clk.Advance(70 * time.Second)
	res, err := sup.Check(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(res.NoHello) != 1 || res.NoHello[0] != "E-1" {
		t.Fatalf("no_hello: %+v", res)
	}
	if len(uploader.noHello) != 1 {
		t.Fatalf("upload no_hello: %+v", uploader.noHello)
	}
}

func TestShimSupervisor_HelloReceived_NoTimeout(t *testing.T) {
	clk := clock.NewFakeClock(time.Date(2026, 5, 21, 12, 0, 0, 0, time.UTC))
	uploader := &recordingUploader{}
	timer := fakeStartTimer{start: clk.Now()}
	sup := NewShimSupervisor(timer, clk, 60*time.Second, uploader)
	sup.Register(ShimRecord{
		ExecutionID: "E-1", ShimPID: 100, ShimStartTime: clk.Now(),
		SpawnedAt: clk.Now(),
	})
	sup.MarkHelloReceived("E-1")
	clk.Advance(120 * time.Second)
	res, err := sup.Check(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(res.NoHello) != 0 {
		t.Fatalf("expected no no_hello: %+v", res)
	}
}

func TestShimSupervisor_Crashed_StartTimeMismatch(t *testing.T) {
	clk := clock.NewFakeClock(time.Date(2026, 5, 21, 12, 0, 0, 0, time.UTC))
	uploader := &recordingUploader{}
	timer := fakeStartTimer{start: clk.Now().Add(time.Hour)} // mismatch
	sup := NewShimSupervisor(timer, clk, 60*time.Second, uploader)
	sup.Register(ShimRecord{
		ExecutionID: "E-1", ShimPID: 100, ShimStartTime: clk.Now(),
		SpawnedAt: clk.Now(),
	})
	sup.MarkHelloReceived("E-1")
	res, err := sup.Check(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Crashed) != 1 {
		t.Fatalf("crashed: %+v", res)
	}
}

func TestShimSupervisor_Crashed_ProcessGone(t *testing.T) {
	clk := clock.NewFakeClock(time.Now())
	uploader := &recordingUploader{}
	timer := fakeStartTimer{start: time.Time{}} // not found
	sup := NewShimSupervisor(timer, clk, 60*time.Second, uploader)
	sup.Register(ShimRecord{
		ExecutionID: "E-1", ShimPID: 100, ShimStartTime: clk.Now(),
		SpawnedAt: clk.Now(),
	})
	sup.MarkHelloReceived("E-1")
	res, _ := sup.Check(context.Background())
	if len(res.Crashed) != 1 {
		t.Fatalf("crashed: %+v", res)
	}
}

func TestShimSupervisor_RemoveAndSnapshot(t *testing.T) {
	sup := NewShimSupervisor(nil, nil, 0, nil)
	sup.Register(ShimRecord{ExecutionID: "E-1", SpawnedAt: time.Now()})
	if len(sup.Snapshot()) != 1 {
		t.Fatal("expected 1")
	}
	sup.Remove("E-1")
	if len(sup.Snapshot()) != 0 {
		t.Fatal("expected 0")
	}
}

func TestShimSupervisor_TimerErrorIgnored(t *testing.T) {
	clk := clock.NewFakeClock(time.Now())
	uploader := &recordingUploader{}
	timer := fakeStartTimer{err: errors.New("read fail")}
	sup := NewShimSupervisor(timer, clk, 60*time.Second, uploader)
	sup.Register(ShimRecord{ExecutionID: "E-1", ShimPID: 100, ShimStartTime: clk.Now(), SpawnedAt: clk.Now()})
	sup.MarkHelloReceived("E-1")
	res, err := sup.Check(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	// On error, don't classify as crashed (let next sweep retry).
	if len(res.Crashed) != 0 {
		t.Fatalf("expected no crashed on timer err: %+v", res)
	}
}
