package identity

import (
	"errors"
	"strings"
	"testing"
	"time"
)

func TestKindFromID(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		in     IdentityID
		want   Kind
		errSub string
	}{
		{IdentityID("user:hayang"), KindUser, ""},
		{IdentityID("supervisor:inv-1"), KindSupervisor, ""},
		{IdentityID("agent:a-1"), KindAgent, ""},
		{IdentityID("bot"), KindBot, ""},
		{IdentityID(""), "", "cannot derive"},
		{IdentityID("worker:w-1"), "", "cannot derive"},
		{IdentityID("user:"), "", "cannot derive"},
	} {
		got, err := KindFromID(tc.in)
		if tc.errSub != "" {
			if err == nil || !strings.Contains(err.Error(), tc.errSub) {
				t.Errorf("KindFromID(%q) want err containing %q, got %v", tc.in, tc.errSub, err)
			}
			continue
		}
		if err != nil {
			t.Errorf("KindFromID(%q) unexpected err: %v", tc.in, err)
		}
		if got != tc.want {
			t.Errorf("KindFromID(%q) = %q, want %q", tc.in, got, tc.want)
		}
	}
}

func TestIdentityIDValidate(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		id    IdentityID
		valid bool
	}{
		{"user:hayang", true},
		{"supervisor:inv-1", true},
		{"agent:a-1", true},
		{"bot", true},
		{"", false},
		{"user:", false},
		{"worker:w-1", false},
	} {
		err := tc.id.Validate()
		if (err == nil) != tc.valid {
			t.Errorf("Validate(%q) valid=%v err=%v", tc.id, tc.valid, err)
		}
	}
}

func TestChannelValidate(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		c     Channel
		valid bool
	}{
		{"feishu", true},
		{"dingtalk", true},
		{"web-chat", true},
		{"", false},
		{"Feishu", false},
		{"feishu chat", false},
		{"feishu/x", false},
	} {
		err := tc.c.Validate()
		if (err == nil) != tc.valid {
			t.Errorf("Channel(%q) valid=%v err=%v", tc.c, tc.valid, err)
		}
	}
}

func TestNewIdentityHappyAndMismatch(t *testing.T) {
	t.Parallel()
	now := time.Date(2026, 5, 21, 10, 0, 0, 0, time.UTC)
	id, err := NewIdentity(NewIdentityInput{
		ID: "user:hayang", Kind: KindUser, DisplayName: "hayang", CreatedAt: now,
	})
	if err != nil {
		t.Fatal(err)
	}
	if id.Version() != 1 || id.Kind() != KindUser || id.DisplayName() != "hayang" {
		t.Fatalf("identity construction lost data: %+v", id)
	}
	if _, err := NewIdentity(NewIdentityInput{
		ID: "user:hayang", Kind: KindSupervisor, DisplayName: "hayang", CreatedAt: now,
	}); err == nil {
		t.Fatal("want kind mismatch err")
	}
	if _, err := NewIdentity(NewIdentityInput{
		ID: "user:hayang", Kind: KindUser, DisplayName: "  ", CreatedAt: now,
	}); err == nil {
		t.Fatal("want empty display_name err")
	}
	if _, err := NewIdentity(NewIdentityInput{
		ID: "user:hayang", Kind: KindUser, DisplayName: "x",
	}); err == nil {
		t.Fatal("want zero time err")
	}
	if _, err := NewIdentity(NewIdentityInput{
		ID: "", Kind: KindUser, DisplayName: "x", CreatedAt: now,
	}); err == nil {
		t.Fatal("want empty id err")
	}
}

func TestRenameBumpsVersion(t *testing.T) {
	t.Parallel()
	now := time.Date(2026, 5, 21, 10, 0, 0, 0, time.UTC)
	id, _ := NewIdentity(NewIdentityInput{
		ID: "user:hayang", Kind: KindUser, DisplayName: "hayang", CreatedAt: now,
	})
	if err := id.Rename("Hayang Updated", now.Add(time.Minute)); err != nil {
		t.Fatal(err)
	}
	if id.Version() != 2 {
		t.Fatalf("version: %d", id.Version())
	}
	if id.DisplayName() != "Hayang Updated" {
		t.Fatalf("display: %s", id.DisplayName())
	}
	if err := id.Rename("  ", now); err == nil {
		t.Fatal("want empty display err")
	}
}

func TestRehydrateIdentityVersionGuard(t *testing.T) {
	t.Parallel()
	if _, err := RehydrateIdentity(RehydrateIdentityInput{
		ID: "user:x", Kind: KindUser, Version: 0,
	}); err == nil {
		t.Fatal("want err on version<1")
	}
	if _, err := RehydrateIdentity(RehydrateIdentityInput{
		ID: "user:x", Kind: "weird", Version: 1,
	}); !errors.Is(err, ErrIdentityInvalidKind) {
		t.Fatalf("want ErrIdentityInvalidKind, got %v", err)
	}
}

func TestNewChannelBindingHappyAndErrors(t *testing.T) {
	t.Parallel()
	now := time.Date(2026, 5, 21, 10, 0, 0, 0, time.UTC)
	b, err := NewChannelBinding(NewChannelBindingInput{
		ID: "01J0000000000000000000000B", IdentityID: "user:x", Channel: "feishu",
		VendorUserID: "ou_xxx", Preferred: true, BoundAt: now,
	})
	if err != nil {
		t.Fatal(err)
	}
	if !b.Preferred() {
		t.Fatal("preferred lost")
	}
	if _, err := NewChannelBinding(NewChannelBindingInput{
		ID: "", IdentityID: "user:x", Channel: "feishu", VendorUserID: "ou", BoundAt: now,
	}); err == nil {
		t.Fatal("want id err")
	}
	if _, err := NewChannelBinding(NewChannelBindingInput{
		ID: "x", IdentityID: "bad", Channel: "feishu", VendorUserID: "ou", BoundAt: now,
	}); err == nil {
		t.Fatal("want identity_id err")
	}
	if _, err := NewChannelBinding(NewChannelBindingInput{
		ID: "x", IdentityID: "user:x", Channel: "", VendorUserID: "ou", BoundAt: now,
	}); err == nil {
		t.Fatal("want channel err")
	}
	if _, err := NewChannelBinding(NewChannelBindingInput{
		ID: "x", IdentityID: "user:x", Channel: "feishu", VendorUserID: "", BoundAt: now,
	}); err == nil {
		t.Fatal("want vendor_user_id err")
	}
	if _, err := NewChannelBinding(NewChannelBindingInput{
		ID: "x", IdentityID: "user:x", Channel: "feishu", VendorUserID: "ou",
	}); err == nil {
		t.Fatal("want bound_at err")
	}
}
