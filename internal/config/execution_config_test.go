package config

import (
	"testing"
	"time"
)

func TestExecutionConfig_Durations(t *testing.T) {
	cfg := ExecutionConfig{
		SubmittedTimeoutSeconds:    300,
		DefaultTimeoutHours:        6,
		DispatchAckTimeoutSeconds:  30,
		InputRequestPingHours:      4,
		InputRequestTimeoutHours:   24,
		ShimHelloTimeoutSeconds:    60,
		KillGraceSeconds:           5,
	}
	if cfg.SubmittedTimeout() != 5*time.Minute {
		t.Fatalf("submitted: %v", cfg.SubmittedTimeout())
	}
	if cfg.ExecutionTimeout() != 6*time.Hour {
		t.Fatalf("execution: %v", cfg.ExecutionTimeout())
	}
	if cfg.DispatchAckTimeout() != 30*time.Second {
		t.Fatalf("dispatch ack: %v", cfg.DispatchAckTimeout())
	}
	if cfg.InputRequestPing() != 4*time.Hour {
		t.Fatalf("ping: %v", cfg.InputRequestPing())
	}
	if cfg.InputRequestTimeout() != 24*time.Hour {
		t.Fatalf("timeout: %v", cfg.InputRequestTimeout())
	}
	if cfg.ShimHelloTimeout() != 60*time.Second {
		t.Fatalf("shim hello: %v", cfg.ShimHelloTimeout())
	}
	if cfg.KillGrace() != 5*time.Second {
		t.Fatalf("kill grace: %v", cfg.KillGrace())
	}
}

func TestExecutionConfig_Defaults(t *testing.T) {
	// Zero config should fall back to spec defaults.
	cfg := ExecutionConfig{}
	if cfg.SubmittedTimeout() != 5*time.Minute {
		t.Fatal("submitted default")
	}
	if cfg.ExecutionTimeout() != 6*time.Hour {
		t.Fatal("execution default")
	}
	if cfg.DispatchAckTimeout() != 30*time.Second {
		t.Fatal("dispatch default")
	}
	if cfg.InputRequestPing() != 4*time.Hour {
		t.Fatal("ping default")
	}
	if cfg.InputRequestTimeout() != 24*time.Hour {
		t.Fatal("timeout default")
	}
	if cfg.ShimHelloTimeout() != 60*time.Second {
		t.Fatal("shim default")
	}
	if cfg.KillGrace() != 5*time.Second {
		t.Fatal("kill default")
	}
}
