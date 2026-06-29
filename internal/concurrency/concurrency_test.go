package concurrency

import (
	"testing"
	"time"
)

func TestInMemoryStore_PutGet_AgeAndMiss(t *testing.T) {
	s := NewInMemoryStore()
	base := time.Unix(1_700_000_000, 0)

	// Miss: never recorded.
	if _, _, ok := s.Get("a1", base); ok {
		t.Fatal("Get on an unknown agent should report ok=false")
	}

	snap := AgentSnapshot{Active: 2, Executors: []ExecutorSnapshot{
		{ExecutorID: "e1", State: StateRunning, TaskID: "t1"},
		{ExecutorID: "e2", State: StateStarting},
	}}
	s.Put("a1", snap, base)

	got, age, ok := s.Get("a1", base.Add(5*time.Second))
	if !ok {
		t.Fatal("Get after Put should report ok=true")
	}
	if age != 5*time.Second {
		t.Errorf("age = %v, want 5s", age)
	}
	if got.Active != 2 || len(got.Executors) != 2 || got.Executors[0].TaskID != "t1" {
		t.Errorf("snapshot round-trip mismatch: %+v", got)
	}
}

func TestInMemoryStore_PutReplaces(t *testing.T) {
	s := NewInMemoryStore()
	base := time.Unix(1_700_000_000, 0)
	s.Put("a1", AgentSnapshot{Active: 3}, base)
	s.Put("a1", AgentSnapshot{Active: 0}, base.Add(time.Second)) // agent went idle
	got, age, ok := s.Get("a1", base.Add(time.Second))
	if !ok || got.Active != 0 {
		t.Fatalf("latest Put should win: active=%d ok=%v", got.Active, ok)
	}
	if age != 0 {
		t.Errorf("age = %v, want 0 (received at the same instant)", age)
	}
}

func TestInMemoryStore_EmptyAgentIDIgnored(t *testing.T) {
	s := NewInMemoryStore()
	s.Put("", AgentSnapshot{Active: 1}, time.Unix(1, 0))
	if _, _, ok := s.Get("", time.Unix(1, 0)); ok {
		t.Error("empty agent id must not be stored")
	}
}
