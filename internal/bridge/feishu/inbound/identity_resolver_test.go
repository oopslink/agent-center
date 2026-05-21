package inbound_test

import (
	"context"
	"errors"
	"sync"
	"testing"

	"github.com/oopslink/agent-center/internal/bridge/feishu/inbound"
	"github.com/oopslink/agent-center/internal/conversation/identity"
	"github.com/oopslink/agent-center/internal/observability"
)

func TestResolver_AutoBindHappyPath(t *testing.T) {
	f := newFixture(t)
	want := f.seedUser(t, "hayang")
	id, err := f.resolver.Resolve(context.Background(), "ou-feishu-1")
	if err != nil {
		t.Fatalf("resolve: %v", err)
	}
	if id != want {
		t.Errorf("identity_id mismatch: got %s want %s", id, want)
	}
	if !f.hasEvent(t, "bridge.identity_auto_bound") {
		t.Error("bridge.identity_auto_bound not emitted")
	}
	if !f.hasEvent(t, "identity.channel_bound") {
		t.Error("identity.channel_bound not emitted (same-tx)")
	}
}

func TestResolver_RepeatHitsCache(t *testing.T) {
	f := newFixture(t)
	want := f.seedUser(t, "hayang")
	if _, err := f.resolver.Resolve(context.Background(), "ou-1"); err != nil {
		t.Fatalf("first: %v", err)
	}
	id, err := f.resolver.Resolve(context.Background(), "ou-1")
	if err != nil {
		t.Fatalf("second: %v", err)
	}
	if id != want {
		t.Errorf("id mismatch: %s vs %s", id, want)
	}
}

func TestResolver_NoUserIdentity(t *testing.T) {
	f := newFixture(t)
	_, err := f.resolver.Resolve(context.Background(), "ou-x")
	if !errors.Is(err, inbound.ErrNoUserIdentity) {
		t.Fatalf("want ErrNoUserIdentity, got %v", err)
	}
	if !f.hasEvent(t, "bridge.parse_failed") {
		t.Error("bridge.parse_failed not emitted")
	}
}

func TestResolver_AmbiguousUser(t *testing.T) {
	f := newFixture(t)
	f.seedUser(t, "alice")
	f.seedUser(t, "bob")
	_, err := f.resolver.Resolve(context.Background(), "ou-y")
	if !errors.Is(err, inbound.ErrAmbiguousUserIdentity) {
		t.Fatalf("want ErrAmbiguousUserIdentity, got %v", err)
	}
}

func TestResolver_EmptyUserID(t *testing.T) {
	f := newFixture(t)
	f.seedUser(t, "hayang")
	_, err := f.resolver.Resolve(context.Background(), "")
	if err == nil {
		t.Fatal("empty vendor_user_id should error")
	}
}

func TestResolver_AlreadyBound(t *testing.T) {
	f := newFixture(t)
	user := f.seedUser(t, "hayang")
	// Pre-bind via the registration service to skip the auto-bind path.
	if _, err := f.identReg.BindChannel(context.Background(), identity.BindChannelCommand{
		IdentityID:   user,
		Channel:      identity.Channel("feishu"),
		VendorUserID: "ou-preset",
		Preferred:    true,
		Actor:        observability.Actor("system"),
	}); err != nil {
		t.Fatalf("pre-bind: %v", err)
	}
	got, err := f.resolver.Resolve(context.Background(), "ou-preset")
	if err != nil {
		t.Fatalf("resolve: %v", err)
	}
	if got != user {
		t.Errorf("id mismatch: %s", got)
	}
}

func TestResolver_ConcurrentFirstBind(t *testing.T) {
	f := newFixture(t)
	want := f.seedUser(t, "hayang")
	var wg sync.WaitGroup
	N := 4
	results := make([]identity.IdentityID, N)
	errs := make([]error, N)
	wg.Add(N)
	for i := 0; i < N; i++ {
		i := i
		go func() {
			defer wg.Done()
			id, err := f.resolver.Resolve(context.Background(), "ou-concurrent")
			results[i] = id
			errs[i] = err
		}()
	}
	wg.Wait()
	for i, e := range errs {
		if e != nil {
			t.Fatalf("goroutine %d err: %v", i, e)
		}
		if results[i] != want {
			t.Errorf("goroutine %d id mismatch: %s", i, results[i])
		}
	}
	// Only one binding should exist (UNIQUE constraint enforced).
	b, err := f.bindings.FindByVendorUserID(context.Background(), identity.Channel("feishu"), "ou-concurrent")
	if err != nil {
		t.Fatalf("find binding: %v", err)
	}
	if b.IdentityID() != want {
		t.Errorf("binding identity: %s", b.IdentityID())
	}
}

func TestResolver_NewResolverValidation(t *testing.T) {
	// Bad channel.
	_, err := inbound.NewIdentityResolver(inbound.IdentityResolverDeps{})
	if err == nil {
		t.Fatal("want error on missing deps")
	}
}
