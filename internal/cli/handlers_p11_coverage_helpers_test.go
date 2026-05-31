package cli

import (
	"os"
	"testing"

	"github.com/oopslink/agent-center/internal/conversation"
	convservice "github.com/oopslink/agent-center/internal/conversation/service"
	"github.com/oopslink/agent-center/internal/observability"
	"github.com/oopslink/agent-center/internal/secretmgmt"
	secretservice "github.com/oopslink/agent-center/internal/secretmgmt/service"
)

// addMsgCmd builds a minimal AddMessageCommand for tests.
func addMsgCmd(app *App, convID conversation.ConversationID) convservice.AddMessageCommand {
	return convservice.AddMessageCommand{
		ConversationID:   convID,
		SenderIdentityID: conversation.IdentityRef(app.DefaultActor()),
		ContentKind:      conversation.MessageContentText,
		Content:          "hi",
		Direction:        conversation.DirectionInbound,
		Actor:            app.DefaultActor(),
	}
}

// materialiseCmd builds a MaterialiseCommand for one source message.
func materialiseCmd(app *App, childID, sourceID conversation.ConversationID, msgID conversation.MessageID) convservice.MaterialiseCommand {
	return convservice.MaterialiseCommand{
		ChildConversationID:  childID,
		SourceConversationID: sourceID,
		SourceMessageIDs:     []conversation.MessageID{msgID},
		CreatedBy:            conversation.IdentityRef(app.DefaultActor()),
		Actor:                app.DefaultActor(),
	}
}

// generateMK returns a fresh master key for tests.
func generateMK(t *testing.T) (*secretmgmt.MasterKey, error) {
	t.Helper()
	return secretmgmt.GenerateMasterKey()
}

// wireSecret installs a UserSecretService on the App.
func wireSecret(app *App, mk *secretmgmt.MasterKey) {
	app.UserSecretSvc = secretservice.NewUserSecretService(
		app.DB, app.UserSecretRepo, app.IDGen, app.Sink, app.Clock, mk)
}

// osWriteFile is a thin wrapper to keep handlers_p11_coverage_test.go
// import-list small.
func osWriteFile(path string, data []byte, mode os.FileMode) error {
	return os.WriteFile(path, data, mode)
}

// _ keep observability import alive for actor-related test helpers.
var _ = observability.Actor("")
