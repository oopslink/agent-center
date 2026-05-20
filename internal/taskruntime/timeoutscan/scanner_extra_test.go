package timeoutscan

import (
	"context"
	"testing"
	"time"
)

func TestDefaultConfigDefaults(t *testing.T) {
	cfg := DefaultConfig()
	if cfg.SubmittedTimeout != 5*time.Minute {
		t.Fatal("submitted")
	}
	if cfg.ExecutionTimeout != 6*time.Hour {
		t.Fatal("execution")
	}
	if cfg.InputRequestTimeoutT2 != 24*time.Hour {
		t.Fatal("ir t2")
	}
}

func TestNewScanner_DefaultsApplied(t *testing.T) {
	// nil clock + zero cfg → defaults set
	s := NewScanner(nil, nil, nil, nil, nil, nil, nil, Config{})
	if s.cfg.TickInterval != 30*time.Second {
		t.Fatal("tick")
	}
	if s.cfg.SubmittedTimeout != 5*time.Minute {
		t.Fatal("submitted")
	}
	if s.cfg.ExecutionTimeout != 6*time.Hour {
		t.Fatal("execution")
	}
	if s.cfg.InputRequestTimeoutT2 != 24*time.Hour {
		t.Fatal("t2")
	}
	if s.cfg.InputRequestPingT1 != 4*time.Hour {
		t.Fatal("t1")
	}
}

func TestTick_ActorValidation(t *testing.T) {
	s := NewScanner(nil, nil, nil, nil, nil, nil, nil, DefaultConfig())
	if err := s.Tick(context.Background(), "BAD"); err == nil {
		t.Fatal("expected actor")
	}
}
