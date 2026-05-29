package identity

import (
	"strings"
	"testing"
)

func TestMemberFactory_New(t *testing.T) {
	f := MemberFactory{}

	t.Run("owner member", func(t *testing.T) {
		m, err := f.New("organization-abc123", "user-def456", RoleOwner, nil)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if !strings.HasPrefix(m.ID(), "mem-") {
			t.Errorf("expected mem- prefix, got %s", m.ID())
		}
		if m.Role() != RoleOwner {
			t.Errorf("expected role=owner")
		}
		if m.Status() != MemberJoined {
			t.Errorf("expected status=joined")
		}
		if m.InvitedByIdentityID() != nil {
			t.Error("expected invitedBy=nil for signup bootstrap")
		}
	})

	t.Run("member with invitedBy", func(t *testing.T) {
		invitedBy := "user-admin123"
		m, err := f.New("organization-abc123", "user-new456", RoleMember, &invitedBy)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if m.InvitedByIdentityID() == nil || *m.InvitedByIdentityID() != invitedBy {
			t.Error("expected invitedBy set correctly")
		}
		if m.InvitedAt() == nil {
			t.Error("expected invited_at set")
		}
	})
}

func TestMemberRole_AtLeast(t *testing.T) {
	if !RoleOwner.AtLeast(RoleAdmin) {
		t.Error("owner should be at least admin")
	}
	if !RoleOwner.AtLeast(RoleMember) {
		t.Error("owner should be at least member")
	}
	if !RoleAdmin.AtLeast(RoleMember) {
		t.Error("admin should be at least member")
	}
	if RoleMember.AtLeast(RoleAdmin) {
		t.Error("member should not be at least admin")
	}
	if RoleAdmin.AtLeast(RoleOwner) {
		t.Error("admin should not be at least owner")
	}
}

func TestMember_DisableReEnable(t *testing.T) {
	f := MemberFactory{}
	m, _ := f.New("organization-abc123", "user-def456", RoleMember, nil)

	m.Disable("test reason")
	if m.Status() != MemberDisabled {
		t.Error("expected disabled status")
	}
	if m.DisabledAt() == nil {
		t.Error("expected disabled_at set")
	}
	if m.DisabledReason() != "test reason" {
		t.Errorf("expected reason 'test reason', got %s", m.DisabledReason())
	}

	m.ReEnable()
	if m.Status() != MemberJoined {
		t.Error("expected joined status after re-enable")
	}
	if m.DisabledAt() != nil {
		t.Error("expected disabled_at cleared")
	}
}
