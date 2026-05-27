package identity

import (
	"context"
	"strings"
	"testing"
)

func TestAgentIdentityProvisionService_Provision(t *testing.T) {
	db, idRepo, _, memberRepo, org, owner := setupSignedUpOrg(t)
	defer db.Close()
	ctx := context.Background()

	svc := NewAgentIdentityProvisionService(db, idRepo, memberRepo)

	t.Run("owner can provision agent", func(t *testing.T) {
		result, err := svc.Provision(ctx, AgentProvisionForm{
			DisplayName: "MyBot", Description: "A helpful bot", Role: RoleMember,
		}, org.ID(), owner.ID())
		if err != nil {
			t.Fatalf("Provision: %v", err)
		}
		if result.Identity.Kind() != KindAgent {
			t.Error("expected kind=agent")
		}
		if !strings.HasPrefix(result.Identity.ID(), "agent-") {
			t.Errorf("expected agent- prefix, got %s", result.Identity.ID())
		}
		if result.Member.Role() != RoleMember {
			t.Errorf("expected role=member, got %s", result.Member.Role())
		}
		if *result.Member.InvitedByIdentityID() != owner.ID() {
			t.Error("expected invitedBy set to owner")
		}
	})

	t.Run("member cannot provision agent", func(t *testing.T) {
		// Create a regular member.
		idf := IdentityFactory{}
		regularUser, _ := idf.NewUser("Regular", "hash")
		idRepo.Save(ctx, regularUser)
		m, _ := MemberFactory{}.New(org.ID(), regularUser.ID(), RoleMember, nil)
		memberRepo.Save(ctx, m)

		_, err := svc.Provision(ctx, AgentProvisionForm{
			DisplayName: "Bot2", Role: RoleMember,
		}, org.ID(), regularUser.ID())
		if err != ErrForbidden {
			t.Errorf("expected ErrForbidden, got %v", err)
		}
	})

	t.Run("non-member cannot provision agent", func(t *testing.T) {
		_, err := svc.Provision(ctx, AgentProvisionForm{
			DisplayName: "Bot3", Role: RoleMember,
		}, org.ID(), "user-nonexistent")
		if err != ErrForbidden {
			t.Errorf("expected ErrForbidden, got %v", err)
		}
	})
}

func TestIdentityBCFacade(t *testing.T) {
	db, idRepo, orgRepo, memberRepo, org, owner := setupSignedUpOrg(t)
	defer db.Close()
	ctx := context.Background()

	facade := NewIdentityBCFacade(idRepo, orgRepo, memberRepo)

	t.Run("IdentityExists true", func(t *testing.T) {
		if !facade.IdentityExists(ctx, owner.ID()) {
			t.Error("expected existing identity to be found")
		}
	})

	t.Run("IdentityExists false", func(t *testing.T) {
		if facade.IdentityExists(ctx, "user-nonexistent") {
			t.Error("expected non-existent identity to return false")
		}
	})

	t.Run("GetActiveOrganization found", func(t *testing.T) {
		got, err := facade.GetActiveOrganization(ctx, org.ID())
		if err != nil {
			t.Fatalf("GetActiveOrganization: %v", err)
		}
		if got.ID() != org.ID() {
			t.Error("id mismatch")
		}
	})

	t.Run("GetActiveOrganization deleted", func(t *testing.T) {
		lock := NewOrganizationLockManager()
		lifeSvc := NewOrganizationLifecycleService(db, orgRepo, memberRepo, lock)
		lifeSvc.Delete(ctx, org.ID())

		_, err := facade.GetActiveOrganization(ctx, org.ID())
		if err != ErrOrganizationDeleted {
			t.Errorf("expected ErrOrganizationDeleted, got %v", err)
		}
	})
}
