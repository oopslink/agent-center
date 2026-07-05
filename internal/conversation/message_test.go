package conversation

import (
	"errors"
	"testing"
	"time"
)

func TestNewMessage_Happy(t *testing.T) {
	m, err := NewMessage(NewMessageInput{
		ID: "m-1", ConversationID: "c-1",
		SenderIdentityID: "user:hayang", ContentKind: MessageContentText,
		Content: "hi", Direction: DirectionInbound, PostedAt: time.Now(),
	})
	if err != nil {
		t.Fatal(err)
	}
	if m.ID() != "m-1" || m.ContentKind() != MessageContentText {
		t.Fatalf("got %+v", m)
	}
}

func TestNewMessage_BadSender(t *testing.T) {
	_, err := NewMessage(NewMessageInput{
		ID: "m", ConversationID: "c",
		SenderIdentityID: "", ContentKind: MessageContentText,
		Direction: DirectionInbound, PostedAt: time.Now(),
	})
	if err != ErrMessageInvalidSender {
		t.Fatalf("got %v", err)
	}
}

func TestNewMessage_BadKind(t *testing.T) {
	_, err := NewMessage(NewMessageInput{
		ID: "m", ConversationID: "c", SenderIdentityID: "system",
		ContentKind: "x", Direction: DirectionInbound, PostedAt: time.Now(),
	})
	if err == nil {
		t.Fatal()
	}
}

func TestNewMessage_BadDirection(t *testing.T) {
	_, err := NewMessage(NewMessageInput{
		ID: "m", ConversationID: "c", SenderIdentityID: "system",
		ContentKind: MessageContentText, Direction: "x", PostedAt: time.Now(),
	})
	if err == nil {
		t.Fatal()
	}
}

func TestNewMessage_MissingIDs(t *testing.T) {
	if _, err := NewMessage(NewMessageInput{
		ContentKind: MessageContentText, Direction: DirectionInbound,
		SenderIdentityID: "system", PostedAt: time.Now(),
	}); err == nil {
		t.Fatal("id required")
	}
	if _, err := NewMessage(NewMessageInput{
		ID: "m", ContentKind: MessageContentText, Direction: DirectionInbound,
		SenderIdentityID: "system", PostedAt: time.Now(),
	}); err == nil {
		t.Fatal("conv id required")
	}
}

func TestNewMessage_ZeroPostedAt(t *testing.T) {
	_, err := NewMessage(NewMessageInput{
		ID: "m", ConversationID: "c", SenderIdentityID: "system",
		ContentKind: MessageContentText, Direction: DirectionInbound,
	})
	if err == nil {
		t.Fatal()
	}
}

func TestRehydrateMessage_Happy(t *testing.T) {
	m, err := RehydrateMessage(RehydrateMessageInput{
		ID: "m", ConversationID: "c", SenderIdentityID: "user:a",
		ContentKind: MessageContentText, Direction: DirectionInbound,
		PostedAt: time.Now(), CreatedAt: time.Now(),
	})
	if err != nil {
		t.Fatal(err)
	}
	if m.SenderIdentityID() != "user:a" {
		t.Fatal()
	}
}

func TestRehydrateMessage_BadKind(t *testing.T) {
	if _, err := RehydrateMessage(RehydrateMessageInput{
		ContentKind: "x", Direction: DirectionInbound,
	}); err == nil {
		t.Fatal()
	}
}

func TestRehydrateMessage_BadDirection(t *testing.T) {
	if _, err := RehydrateMessage(RehydrateMessageInput{
		ContentKind: MessageContentText, Direction: "x",
	}); err == nil {
		t.Fatal()
	}
}

func TestIdentityRefValidate(t *testing.T) {
	cases := []struct {
		in IdentityRef
		ok bool
	}{
		{"", false},
		{"system", true},
		{"user:hayang", true},
		{"agent:s-1", true},
		{"supervisor:x", false},
		{"bot", false},
		{"user:", false},
	}
	for _, c := range cases {
		err := c.in.Validate()
		if (err == nil) != c.ok {
			t.Errorf("ref %q ok=%v err=%v", c.in, c.ok, err)
		}
	}
}

func TestMessageContentKindEnum(t *testing.T) {
	for _, k := range []MessageContentKind{MessageContentText, MessageContentSystem,
		MessageContentAgentFinding, MessageContentSupervisorSummary,
		MessageContentConclusionDraft, MessageContentTaskProposal} {
		if !k.IsValid() {
			t.Fatalf("%s should be valid", k)
		}
	}
	if MessageContentKind("nope").IsValid() {
		t.Fatal()
	}
}

func TestMessageDirectionEnum(t *testing.T) {
	for _, d := range []MessageDirection{DirectionInbound, DirectionOutbound, DirectionInternal} {
		if !d.IsValid() {
			t.Fatalf("%s should be valid", d)
		}
	}
	if MessageDirection("nope").IsValid() {
		t.Fatal()
	}
}

func TestParticipantElement_IsActive(t *testing.T) {
	p := ParticipantElement{IdentityID: "user:a", Role: "owner"}
	if !p.IsActive() {
		t.Fatal()
	}
	p.LeftAt = "t"
	if p.IsActive() {
		t.Fatal()
	}
}

// --- v2.9.1 Thread (P1): parent/root refs + depth-1 invariant ---

// A top-level message has no parent/root; it is its own thread root, so
// ThreadID() is its own id.
func TestNewMessage_RootMessage_ThreadDefaults(t *testing.T) {
	m, err := NewMessage(NewMessageInput{
		ID: "m-root", ConversationID: "c-1", SenderIdentityID: "user:a",
		ContentKind: MessageContentText, Direction: DirectionInbound, PostedAt: time.Now(),
	})
	if err != nil {
		t.Fatal(err)
	}
	if m.ParentMessageID() != "" || m.RootMessageID() != "" {
		t.Fatalf("root message must have empty parent/root, got parent=%q root=%q", m.ParentMessageID(), m.RootMessageID())
	}
	if !m.IsThreadRoot() {
		t.Fatal("top-level message should be a thread root")
	}
	if m.ThreadID() != "m-root" {
		t.Fatalf("thread id of a root is its own id, got %q", m.ThreadID())
	}
}

// A reply attaches to a root: parent == root, both non-empty, both != self.
func TestNewMessage_ThreadReply_Happy(t *testing.T) {
	m, err := NewMessage(NewMessageInput{
		ID: "m-reply", ConversationID: "c-1", SenderIdentityID: "user:a",
		ContentKind: MessageContentText, Direction: DirectionInbound, PostedAt: time.Now(),
		ParentMessageID: "m-root", RootMessageID: "m-root",
	})
	if err != nil {
		t.Fatal(err)
	}
	if m.ParentMessageID() != "m-root" || m.RootMessageID() != "m-root" {
		t.Fatalf("got parent=%q root=%q", m.ParentMessageID(), m.RootMessageID())
	}
	if m.IsThreadRoot() {
		t.Fatal("a reply is not a thread root")
	}
	if m.ThreadID() != "m-root" {
		t.Fatalf("reply thread id is its root, got %q", m.ThreadID())
	}
}

// Depth-1 invariant: parent must equal root (a reply can only hang off a root).
func TestNewMessage_ThreadReply_DepthViolation(t *testing.T) {
	_, err := NewMessage(NewMessageInput{
		ID: "m-2", ConversationID: "c-1", SenderIdentityID: "user:a",
		ContentKind: MessageContentText, Direction: DirectionInbound, PostedAt: time.Now(),
		ParentMessageID: "m-reply", RootMessageID: "m-root", // parent != root → 2nd level
	})
	if !errors.Is(err, ErrMessageInvalidThread) {
		t.Fatalf("got %v", err)
	}
}

// A root without a parent is inconsistent (root is derived only for replies).
func TestNewMessage_ThreadReply_RootWithoutParent(t *testing.T) {
	_, err := NewMessage(NewMessageInput{
		ID: "m-2", ConversationID: "c-1", SenderIdentityID: "user:a",
		ContentKind: MessageContentText, Direction: DirectionInbound, PostedAt: time.Now(),
		RootMessageID: "m-root", // parent empty but root set
	})
	if !errors.Is(err, ErrMessageInvalidThread) {
		t.Fatalf("got %v", err)
	}
}

// A message cannot be its own parent/root.
func TestNewMessage_ThreadReply_SelfReply(t *testing.T) {
	_, err := NewMessage(NewMessageInput{
		ID: "m-self", ConversationID: "c-1", SenderIdentityID: "user:a",
		ContentKind: MessageContentText, Direction: DirectionInbound, PostedAt: time.Now(),
		ParentMessageID: "m-self", RootMessageID: "m-self",
	})
	if !errors.Is(err, ErrMessageSelfReply) {
		t.Fatalf("got %v", err)
	}
}

// Replying to a ROOT places the new reply directly under that root.
func TestResolveReplyPlacement_RootTarget(t *testing.T) {
	root, _ := NewMessage(NewMessageInput{
		ID: "A", ConversationID: "c-1", SenderIdentityID: "user:a",
		ContentKind: MessageContentText, Direction: DirectionInbound, PostedAt: time.Now(),
	})
	parentID, rootID := ResolveReplyPlacement(root)
	if parentID != "A" || rootID != "A" {
		t.Fatalf("got parent=%q root=%q, want A/A", parentID, rootID)
	}
}

// Replying to a REPLY merges into the same thread: parent/root redirect to the
// target's root (no 2nd level is ever produced).
func TestResolveReplyPlacement_ReplyTarget_RedirectsToRoot(t *testing.T) {
	reply, _ := NewMessage(NewMessageInput{
		ID: "B", ConversationID: "c-1", SenderIdentityID: "user:a",
		ContentKind: MessageContentText, Direction: DirectionInbound, PostedAt: time.Now(),
		ParentMessageID: "A", RootMessageID: "A",
	})
	parentID, rootID := ResolveReplyPlacement(reply)
	if parentID != "A" || rootID != "A" {
		t.Fatalf("got parent=%q root=%q, want redirect to root A/A", parentID, rootID)
	}
}

// --- 引用 (quote): quoted_message_id is orthogonal to the thread refs ---

// A message may quote an earlier message; the ref is carried verbatim and is
// independent of the (empty) thread parent/root.
func TestNewMessage_Quote_Happy(t *testing.T) {
	m, err := NewMessage(NewMessageInput{
		ID: "m-2", ConversationID: "c-1", SenderIdentityID: "user:a",
		ContentKind: MessageContentText, Direction: DirectionInbound, PostedAt: time.Now(),
		QuotedMessageID: "m-1",
	})
	if err != nil {
		t.Fatal(err)
	}
	if m.QuotedMessageID() != "m-1" {
		t.Fatalf("got quoted=%q, want m-1", m.QuotedMessageID())
	}
	// A quote does not make the message a thread reply.
	if !m.IsThreadRoot() || m.ParentMessageID() != "" {
		t.Fatalf("quote must not touch thread refs, got parent=%q root=%q", m.ParentMessageID(), m.RootMessageID())
	}
}

// A message cannot quote itself.
func TestNewMessage_Quote_Self(t *testing.T) {
	_, err := NewMessage(NewMessageInput{
		ID: "m-1", ConversationID: "c-1", SenderIdentityID: "user:a",
		ContentKind: MessageContentText, Direction: DirectionInbound, PostedAt: time.Now(),
		QuotedMessageID: "m-1",
	})
	if !errors.Is(err, ErrMessageInvalidQuote) {
		t.Fatalf("got %v, want ErrMessageInvalidQuote", err)
	}
}

// A quote combines cleanly with a thread reply — both refs are preserved.
func TestNewMessage_Quote_WithThreadReply(t *testing.T) {
	m, err := NewMessage(NewMessageInput{
		ID: "m-reply", ConversationID: "c-1", SenderIdentityID: "user:a",
		ContentKind: MessageContentText, Direction: DirectionInbound, PostedAt: time.Now(),
		ParentMessageID: "m-root", RootMessageID: "m-root", QuotedMessageID: "m-other",
	})
	if err != nil {
		t.Fatal(err)
	}
	if m.QuotedMessageID() != "m-other" || m.RootMessageID() != "m-root" {
		t.Fatalf("got quoted=%q root=%q", m.QuotedMessageID(), m.RootMessageID())
	}
}

// The quote ref survives a repository round-trip (rehydrate).
func TestRehydrateMessage_Quote(t *testing.T) {
	m, err := RehydrateMessage(RehydrateMessageInput{
		ID: "m-2", ConversationID: "c-1", SenderIdentityID: "user:a",
		ContentKind: MessageContentText, Direction: DirectionInbound,
		PostedAt: time.Now(), CreatedAt: time.Now(), QuotedMessageID: "m-1",
	})
	if err != nil {
		t.Fatal(err)
	}
	if m.QuotedMessageID() != "m-1" {
		t.Fatalf("got quoted=%q, want m-1", m.QuotedMessageID())
	}
}
