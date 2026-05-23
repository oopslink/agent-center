package cli

import (
	"context"

	"github.com/oopslink/agent-center/internal/conversation"
	convservice "github.com/oopslink/agent-center/internal/conversation/service"
	"github.com/oopslink/agent-center/internal/discussion"
	disservice "github.com/oopslink/agent-center/internal/discussion/service"
	trservice "github.com/oopslink/agent-center/internal/taskruntime/service"
)

// issueOpenerShim adapts disservice.IssueLifecycleService to the
// convservice.IssueOpener port consumed by MessageDerivationService.
type issueOpenerShim struct {
	svc *disservice.IssueLifecycleService
}

// OpenFromConversation implements convservice.IssueOpener.
func (s *issueOpenerShim) OpenFromConversation(ctx context.Context, in convservice.OpenFromConversationInput) (convservice.OpenFromConversationResult, error) {
	res, err := s.svc.Open(ctx, disservice.OpenIssueCommand{
		ProjectID:          in.ProjectID,
		Title:              in.Title,
		Description:        in.Description,
		OpenedByIdentityID: string(in.OpenedBy),
		Origin:             discussion.OriginDerivedFromConversation,
		Actor:              in.Actor,
	})
	if err != nil {
		return convservice.OpenFromConversationResult{}, err
	}
	return convservice.OpenFromConversationResult{
		IssueID:        string(res.IssueID),
		ConversationID: res.ConversationID,
		EventID:        res.EventID,
	}, nil
}

// taskCreatorShim adapts trservice.TaskService to the
// convservice.TaskCreator port consumed by MessageDerivationService.
type taskCreatorShim struct {
	svc *trservice.TaskService
}

// CreateFromConversation implements convservice.TaskCreator.
func (s *taskCreatorShim) CreateFromConversation(ctx context.Context, in convservice.CreateFromConversationInput) (convservice.CreateFromConversationResult, error) {
	res, err := s.svc.Create(ctx, trservice.TaskCreateInput{
		ProjectID:         in.ProjectID,
		Title:             in.Title,
		Description:       in.Description,
		WithConversation:  true,
		ConversationTitle: in.Title,
		Actor:             in.Actor,
	})
	if err != nil {
		return convservice.CreateFromConversationResult{}, err
	}
	return convservice.CreateFromConversationResult{
		TaskID:         string(res.TaskID),
		ConversationID: res.ConversationID,
		EventID:        "",
	}, nil
}

// Compile-time guards.
var _ convservice.IssueOpener = (*issueOpenerShim)(nil)
var _ convservice.TaskCreator = (*taskCreatorShim)(nil)
var _ = conversation.ConversationID("")

// parseMessageIDs splits a comma-separated --select-messages flag into
// typed MessageID slice. Empty input returns nil.
func parseMessageIDs(raw string) []conversation.MessageID {
	if raw == "" {
		return nil
	}
	parts := splitCSV(raw)
	out := make([]conversation.MessageID, 0, len(parts))
	for _, p := range parts {
		if p == "" {
			continue
		}
		out = append(out, conversation.MessageID(p))
	}
	return out
}

func splitCSV(s string) []string {
	var out []string
	cur := ""
	for _, r := range s {
		if r == ',' {
			out = append(out, cur)
			cur = ""
			continue
		}
		if r == ' ' || r == '\t' {
			continue
		}
		cur += string(r)
	}
	out = append(out, cur)
	return out
}
