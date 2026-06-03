package service

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"

	"github.com/oopslink/agent-center/internal/clock"
	"github.com/oopslink/agent-center/internal/conversation"
	"github.com/oopslink/agent-center/internal/observability"
	"github.com/oopslink/agent-center/internal/persistence"
)

// ParticipantManagementService implements CV2b — invite / leave / kick on
// the JSON `participants` column (ADR-0034).
//
// All mutations go through a read-modify-write loop bounded by the
// Conversation's `version` (optimistic lock). Each emit happens in the
// same tx as the participants UPDATE.
type ParticipantManagementService struct {
	db       *sql.DB
	convRepo conversation.ConversationRepository
	sink     *observability.EventSink
	clock    clock.Clock
}

// NewParticipantManagementService constructs the service.
func NewParticipantManagementService(
	db *sql.DB,
	convRepo conversation.ConversationRepository,
	sink *observability.EventSink,
	clk clock.Clock,
) *ParticipantManagementService {
	if clk == nil {
		clk = clock.SystemClock{}
	}
	return &ParticipantManagementService{
		db:       db,
		convRepo: convRepo,
		sink:     sink,
		clock:    clk,
	}
}

// Sentinel errors for participant ops.
var (
	ErrParticipantAlreadyActive = errors.New("participant: identity is already an active participant")
	ErrParticipantNotActive     = errors.New("participant: identity is not an active participant")
	ErrParticipantNotOwner      = errors.New("participant: actor is not the channel owner (kick requires owner)")
)

// InviteCommand wraps the invite call.
type InviteCommand struct {
	ConversationName string
	// OrganizationID scopes the channel-name lookup (v2.7 #195: channel name is
	// org-scoped unique). Empty = global (org-agnostic admin path).
	OrganizationID string
	IdentityID     conversation.IdentityRef
	Role           string // owner|member|observer; default member
	InvitedBy      conversation.IdentityRef
	Actor          observability.Actor
}

// resolveChannelByName resolves a channel by name, ORG-SCOPED when orgID is set
// (v2.7 #195), else GLOBAL (the org-agnostic admin path).
func (s *ParticipantManagementService) resolveChannelByName(ctx context.Context, orgID, name string) (*conversation.Conversation, error) {
	if strings.TrimSpace(orgID) != "" {
		return s.convRepo.FindByNameInOrg(ctx, orgID, name)
	}
	return s.convRepo.FindByName(ctx, name)
}

// Invite adds a participant. Returns ErrParticipantAlreadyActive when
// the identity is already in the list and has not left.
func (s *ParticipantManagementService) Invite(ctx context.Context, cmd InviteCommand) (observability.EventID, error) {
	if err := cmd.Actor.Validate(); err != nil {
		return "", err
	}
	if err := cmd.IdentityID.Validate(); err != nil {
		return "", fmt.Errorf("invite: identity_id: %w", err)
	}
	if err := cmd.InvitedBy.Validate(); err != nil {
		return "", fmt.Errorf("invite: invited_by: %w", err)
	}
	if strings.TrimSpace(cmd.ConversationName) == "" {
		return "", errors.New("invite: conversation_name required")
	}
	role := cmd.Role
	if role == "" {
		role = "member"
	}
	if !validRole(role) {
		return "", fmt.Errorf("invite: invalid role %q (must be owner|member|observer)", role)
	}
	var evID observability.EventID
	err := persistence.RunInTx(ctx, s.db, func(txCtx context.Context) error {
		conv, err := s.resolveChannelByName(txCtx, cmd.OrganizationID, cmd.ConversationName)
		if err != nil {
			return err
		}
		if conv.IsTerminal() {
			return conversation.ErrConversationArchived
		}
		if conv.HasActiveParticipant(cmd.IdentityID) {
			return ErrParticipantAlreadyActive
		}
		now := s.clock.Now()
		updated := conv.Participants()
		updated = append(updated, conversation.ParticipantElement{
			IdentityID: cmd.IdentityID,
			Role:       role,
			JoinedAt:   now.UTC().Format("2006-01-02T15:04:05.999999999Z07:00"),
			JoinedBy:   cmd.InvitedBy,
		})
		if err := s.convRepo.UpdateParticipants(txCtx, conv.ID(), updated, conv.Version(), now); err != nil {
			return err
		}
		id, err := s.sink.Emit(txCtx, observability.EmitCommand{
			EventType: "conversation.participant_joined",
			Refs:      observability.EventRefs{ConversationID: string(conv.ID())},
			Actor:     cmd.Actor,
			Payload: map[string]any{
				"conversation_id": string(conv.ID()),
				"identity_id":     string(cmd.IdentityID),
				"role":            role,
				"invited_by":      string(cmd.InvitedBy),
			},
		})
		if err != nil {
			return err
		}
		evID = id
		return nil
	})
	return evID, err
}

// LeaveCommand wraps the self-leave call.
type LeaveCommand struct {
	ConversationName string
	OrganizationID   string // v2.7 #195: org-scopes the name lookup (empty = global)
	IdentityID       conversation.IdentityRef
	Reason           string
	Actor            observability.Actor
}

// Leave marks the identity as having left. Actor == IdentityID is the
// expected pattern but not enforced at this layer.
func (s *ParticipantManagementService) Leave(ctx context.Context, cmd LeaveCommand) (observability.EventID, error) {
	reason := orDefault(cmd.Reason, "self_leave")
	return s.markLeft(ctx, markLeftInput{
		ConversationName: cmd.ConversationName,
		OrganizationID:   cmd.OrganizationID,
		IdentityID:       cmd.IdentityID,
		Reason:           reason,
		Actor:            cmd.Actor,
		EventType:        "conversation.participant_left",
		PayloadExtra: map[string]any{
			"reason":  reason,
			"message": fmt.Sprintf("%s left voluntarily", cmd.IdentityID),
		},
		requireOwner: false,
	})
}

// KickCommand wraps the channel-owner-driven kick call.
type KickCommand struct {
	ConversationName string
	OrganizationID   string // v2.7 #195: org-scopes the name lookup (empty = global)
	IdentityID       conversation.IdentityRef
	KickedBy         conversation.IdentityRef
	Reason           string
	Actor            observability.Actor
}

// Kick removes a participant. KickedBy must be an owner-role participant
// of the conversation.
func (s *ParticipantManagementService) Kick(ctx context.Context, cmd KickCommand) (observability.EventID, error) {
	if err := cmd.KickedBy.Validate(); err != nil {
		return "", fmt.Errorf("kick: kicked_by: %w", err)
	}
	reason := orDefault(cmd.Reason, "kicked")
	return s.markLeft(ctx, markLeftInput{
		ConversationName: cmd.ConversationName,
		OrganizationID:   cmd.OrganizationID,
		IdentityID:       cmd.IdentityID,
		Reason:           reason,
		Actor:            cmd.Actor,
		EventType:        "conversation.participant_left",
		PayloadExtra: map[string]any{
			"reason":    reason,
			"message":   fmt.Sprintf("%s kicked by %s", cmd.IdentityID, cmd.KickedBy),
			"kicked_by": string(cmd.KickedBy),
		},
		requireOwner: true,
		owner:        cmd.KickedBy,
	})
}

type markLeftInput struct {
	ConversationName string
	OrganizationID   string
	IdentityID       conversation.IdentityRef
	Reason           string
	Actor            observability.Actor
	EventType        observability.EventType
	PayloadExtra     map[string]any
	requireOwner     bool
	owner            conversation.IdentityRef
}

func (s *ParticipantManagementService) markLeft(ctx context.Context, in markLeftInput) (observability.EventID, error) {
	if err := in.Actor.Validate(); err != nil {
		return "", err
	}
	if err := in.IdentityID.Validate(); err != nil {
		return "", fmt.Errorf("%s: identity_id: %w", in.EventType, err)
	}
	if strings.TrimSpace(in.ConversationName) == "" {
		return "", fmt.Errorf("%s: conversation_name required", in.EventType)
	}
	var evID observability.EventID
	err := persistence.RunInTx(ctx, s.db, func(txCtx context.Context) error {
		conv, err := s.resolveChannelByName(txCtx, in.OrganizationID, in.ConversationName)
		if err != nil {
			return err
		}
		if conv.IsTerminal() {
			return conversation.ErrConversationArchived
		}
		parts := conv.Participants()
		if in.requireOwner && !hasRole(parts, in.owner, "owner") {
			return ErrParticipantNotOwner
		}
		idx := indexOfActive(parts, in.IdentityID)
		if idx == -1 {
			return ErrParticipantNotActive
		}
		now := s.clock.Now()
		parts[idx].LeftAt = now.UTC().Format("2006-01-02T15:04:05.999999999Z07:00")
		parts[idx].LeftReason = in.Reason
		if err := s.convRepo.UpdateParticipants(txCtx, conv.ID(), parts, conv.Version(), now); err != nil {
			return err
		}
		payload := map[string]any{
			"conversation_id": string(conv.ID()),
			"identity_id":     string(in.IdentityID),
		}
		for k, v := range in.PayloadExtra {
			payload[k] = v
		}
		id, err := s.sink.Emit(txCtx, observability.EmitCommand{
			EventType: in.EventType,
			Refs:      observability.EventRefs{ConversationID: string(conv.ID())},
			Actor:     in.Actor,
			Payload:   payload,
		})
		if err != nil {
			return err
		}
		evID = id
		return nil
	})
	return evID, err
}

func validRole(role string) bool {
	switch role {
	case "owner", "member", "observer":
		return true
	}
	return false
}

func hasRole(parts []conversation.ParticipantElement, id conversation.IdentityRef, role string) bool {
	for _, p := range parts {
		if p.IdentityID == id && p.IsActive() && p.Role == role {
			return true
		}
	}
	return false
}

func indexOfActive(parts []conversation.ParticipantElement, id conversation.IdentityRef) int {
	for i, p := range parts {
		if p.IdentityID == id && p.IsActive() {
			return i
		}
	}
	return -1
}

func orDefault(s, def string) string {
	if strings.TrimSpace(s) == "" {
		return def
	}
	return s
}
