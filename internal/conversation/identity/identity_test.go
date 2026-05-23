package identity

import (
	"testing"
	"time"
)

func TestNewIdentity_Happy(t *testing.T) {
	now := time.Now().UTC()
	for _, in := range []NewIdentityInput{
		{ID: "user:hayang", Kind: KindUser, DisplayName: "Hayang", CreatedAt: now},
		{ID: "agent:s-1", Kind: KindAgent, DisplayName: "Agent1", CreatedAt: now},
		{ID: "system", Kind: KindSystem, DisplayName: "System", CreatedAt: now},
	} {
		i, err := NewIdentity(in)
		if err != nil {
			t.Fatalf("%s: %v", in.ID, err)
		}
		if i.ID() != in.ID || i.Kind() != in.Kind {
			t.Fatalf("got %v", i)
		}
	}
}

func TestNewIdentity_KindMismatch(t *testing.T) {
	_, err := NewIdentity(NewIdentityInput{
		ID: "user:x", Kind: KindAgent, DisplayName: "x", CreatedAt: time.Now(),
	})
	if err == nil {
		t.Fatal()
	}
}

func TestNewIdentity_BadInputs(t *testing.T) {
	if _, err := NewIdentity(NewIdentityInput{ID: "", Kind: KindUser, DisplayName: "x", CreatedAt: time.Now()}); err == nil {
		t.Fatal("empty id")
	}
	if _, err := NewIdentity(NewIdentityInput{ID: "user:x", Kind: "weird", DisplayName: "x", CreatedAt: time.Now()}); err != ErrIdentityInvalidKind {
		t.Fatal("bad kind")
	}
	if _, err := NewIdentity(NewIdentityInput{ID: "user:x", Kind: KindUser, DisplayName: "", CreatedAt: time.Now()}); err == nil {
		t.Fatal("empty display_name")
	}
	if _, err := NewIdentity(NewIdentityInput{ID: "user:x", Kind: KindUser, DisplayName: "x"}); err == nil {
		t.Fatal("zero created_at")
	}
}

func TestKindFromID(t *testing.T) {
	cases := map[IdentityID]Kind{
		"user:hayang": KindUser,
		"agent:s-1":   KindAgent,
		"system":      KindSystem,
	}
	for id, want := range cases {
		got, err := KindFromID(id)
		if err != nil || got != want {
			t.Fatalf("%s: got (%s, %v)", id, got, err)
		}
	}
	if _, err := KindFromID("bot"); err == nil {
		t.Fatal("bot should not derive a v2 kind")
	}
}

func TestIdentityID_Validate(t *testing.T) {
	cases := map[IdentityID]bool{
		"":            false,
		"system":      true,
		"user:x":      true,
		"agent:y":    true,
		"user:":       false,
		"bot":         false,
		"supervisor:x": false,
	}
	for id, ok := range cases {
		err := id.Validate()
		if (err == nil) != ok {
			t.Errorf("%s: ok=%v err=%v", id, ok, err)
		}
	}
}

func TestKindValid(t *testing.T) {
	for _, k := range []Kind{KindUser, KindAgent, KindSystem} {
		if !k.IsValid() {
			t.Fatalf("%s should be valid", k)
		}
	}
	if Kind("nope").IsValid() {
		t.Fatal()
	}
}

func TestIdentity_Rename(t *testing.T) {
	now := time.Now()
	i, _ := NewIdentity(NewIdentityInput{ID: "user:x", Kind: KindUser, DisplayName: "old", CreatedAt: now})
	if err := i.Rename("new", now); err != nil {
		t.Fatal(err)
	}
	if i.DisplayName() != "new" || i.Version() != 2 {
		t.Fatalf("got name=%s ver=%d", i.DisplayName(), i.Version())
	}
	if err := i.Rename("", now); err == nil {
		t.Fatal("empty name should error")
	}
}

func TestRehydrateIdentity_RejectsBadKind(t *testing.T) {
	_, err := RehydrateIdentity(RehydrateIdentityInput{
		ID: "x", Kind: "weird", DisplayName: "n", Version: 1, CreatedAt: time.Now(), UpdatedAt: time.Now(),
	})
	if err != ErrIdentityInvalidKind {
		t.Fatal()
	}
}

func TestRehydrateIdentity_RejectsZeroVersion(t *testing.T) {
	_, err := RehydrateIdentity(RehydrateIdentityInput{
		ID: "x", Kind: KindUser, DisplayName: "n", Version: 0, CreatedAt: time.Now(), UpdatedAt: time.Now(),
	})
	if err == nil {
		t.Fatal()
	}
}
