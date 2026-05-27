package identity

import (
	"strings"
	"testing"
)

func TestIdentityFactory_NewUser(t *testing.T) {
	f := IdentityFactory{}

	t.Run("valid user", func(t *testing.T) {
		id, err := f.NewUser("Hayang", "argon2hash")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if id.Kind() != KindUser {
			t.Errorf("expected kind=user, got %s", id.Kind())
		}
		if !strings.HasPrefix(id.ID(), "user-") {
			t.Errorf("expected id prefix 'user-', got %s", id.ID())
		}
		if id.AccountStatus() != AccountActive {
			t.Errorf("expected active, got %s", id.AccountStatus())
		}
		if id.PasscodeHash() != "argon2hash" {
			t.Errorf("expected passcode hash set")
		}
		if id.PasscodeSetAt() == nil {
			t.Error("expected passcode_set_at non-nil for user")
		}
	})

	t.Run("empty display_name rejected", func(t *testing.T) {
		_, err := f.NewUser("", "hash")
		if err == nil {
			t.Error("expected error for empty display_name")
		}
	})

	t.Run("display_name too long rejected", func(t *testing.T) {
		_, err := f.NewUser(strings.Repeat("a", 41), "hash")
		if err == nil {
			t.Error("expected error for too long display_name")
		}
	})

	t.Run("empty passcode rejected", func(t *testing.T) {
		_, err := f.NewUser("Bob", "")
		if err == nil {
			t.Error("expected error for empty passcode_hash")
		}
	})
}

func TestIdentityFactory_NewAgent(t *testing.T) {
	f := IdentityFactory{}

	t.Run("valid agent", func(t *testing.T) {
		id, err := f.NewAgent("MyBot", "a helpful agent")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if id.Kind() != KindAgent {
			t.Errorf("expected kind=agent, got %s", id.Kind())
		}
		if !strings.HasPrefix(id.ID(), "agent-") {
			t.Errorf("expected id prefix 'agent-', got %s", id.ID())
		}
		if id.PasscodeHash() != "" {
			t.Error("agent should have empty passcode_hash")
		}
		if id.PasscodeSetAt() != nil {
			t.Error("agent should have nil passcode_set_at")
		}
	})
}

func TestIdentity_Disable_ReEnable(t *testing.T) {
	f := IdentityFactory{}
	id, _ := f.NewUser("Test", "hash")

	id.Disable()
	if id.AccountStatus() != AccountDisabled {
		t.Error("expected disabled after Disable()")
	}

	id.ReEnable()
	if id.AccountStatus() != AccountActive {
		t.Error("expected active after ReEnable()")
	}
}

func TestIdentity_SetPasscode(t *testing.T) {
	f := IdentityFactory{}

	t.Run("user can set passcode", func(t *testing.T) {
		id, _ := f.NewUser("Test", "oldhash")
		if err := id.SetPasscode("newhash"); err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if id.PasscodeHash() != "newhash" {
			t.Error("expected new passcode hash")
		}
	})

	t.Run("agent cannot set passcode", func(t *testing.T) {
		id, _ := f.NewAgent("Bot", "")
		if err := id.SetPasscode("hash"); err == nil {
			t.Error("expected error setting passcode on agent")
		}
	})
}
