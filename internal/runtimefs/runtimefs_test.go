package runtimefs

import (
	"testing"
	"time"
)

// The Dispatcher is the req_id correlator: a registered waiter gets exactly the
// matching Response; an unknown / late reply is a no-op; a released slot never matches.
func TestDispatcher_RegisterResolve(t *testing.T) {
	d := NewDispatcher()
	ch, release := d.Register("req-1")
	defer release()

	if ok := d.Resolve(Response{ReqID: "req-1", AgentID: "a", Code: ""}); !ok {
		t.Fatal("Resolve(req-1) = false, want true (a waiter is registered)")
	}
	select {
	case got := <-ch:
		if got.ReqID != "req-1" {
			t.Fatalf("got req_id %q, want req-1", got.ReqID)
		}
	case <-time.After(time.Second):
		t.Fatal("waiter did not receive the resolved response")
	}
}

func TestDispatcher_ResolveUnknownIsNoop(t *testing.T) {
	d := NewDispatcher()
	if ok := d.Resolve(Response{ReqID: "ghost"}); ok {
		t.Fatal("Resolve of an unregistered req_id = true, want false (no waiter)")
	}
}

func TestDispatcher_ReleaseStopsMatching(t *testing.T) {
	d := NewDispatcher()
	_, release := d.Register("req-x")
	release() // caller timed out / context-cancelled
	if ok := d.Resolve(Response{ReqID: "req-x"}); ok {
		t.Fatal("Resolve after release = true, want false (slot freed)")
	}
}

func TestDispatcher_ResolveOnlyOnce(t *testing.T) {
	d := NewDispatcher()
	_, release := d.Register("req-1")
	defer release()
	if ok := d.Resolve(Response{ReqID: "req-1"}); !ok {
		t.Fatal("first Resolve should match")
	}
	if ok := d.Resolve(Response{ReqID: "req-1"}); ok {
		t.Fatal("second Resolve should NOT match (slot consumed) — a duplicate worker reply is a no-op")
	}
}
