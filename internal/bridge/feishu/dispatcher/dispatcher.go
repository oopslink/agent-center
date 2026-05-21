package dispatcher

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"sync"
	"time"

	"github.com/oopslink/agent-center/internal/bridge/feishu/client"
	"github.com/oopslink/agent-center/internal/bridge/feishu/ledger"
	"github.com/oopslink/agent-center/internal/bridge/feishu/renderer"
	"github.com/oopslink/agent-center/internal/clock"
	"github.com/oopslink/agent-center/internal/conversation"
	"github.com/oopslink/agent-center/internal/conversation/identity"
	"github.com/oopslink/agent-center/internal/idgen"
	"github.com/oopslink/agent-center/internal/observability"
	"github.com/oopslink/agent-center/internal/taskruntime/inputrequest"
)

// SubscriberName is the cursor table key the dispatcher uses.
const SubscriberName = "feishu_outbound"

// EventQuerier is the narrow view the dispatcher needs of the
// observability EventRepository. The real EventRepository fulfills this
// without modification.
type EventQuerier interface {
	Find(ctx context.Context, filter observability.EventQueryFilter) ([]*observability.Event, error)
}

// Deps wraps the dispatcher's collaborators. Construct via NewService.
type Deps struct {
	DB             *sql.DB
	Clock          clock.Clock
	IDGen          idgen.Generator
	Events         EventQuerier
	Sink           *observability.EventSink
	Cursor         CursorStore
	Conversations  conversation.ConversationRepository
	Messages       conversation.MessageRepository
	Bindings       identity.ChannelBindingRepository
	InputRequests  inputrequest.Repository
	Ledger         ledger.Repository
	Client         client.Client
	Renderer       *renderer.Renderer
	// TaskByConversation / IssueByConversation are optional lookup hooks
	// used to derive the root-card SubjectRef. The dispatcher accepts nil
	// (falls back to the conversation id).
	TaskByConversation  func(ctx context.Context, conversationID conversation.ConversationID) (subjectRef, title string, err error)
	IssueByConversation func(ctx context.Context, conversationID conversation.ConversationID) (subjectRef, title string, err error)
}

// Config tunes polling + retry behaviour.
type Config struct {
	// PollInterval between event-table polls (default 250ms).
	PollInterval time.Duration
	// BatchSize per poll (default 100).
	BatchSize int
	// Channel string emitted into events / ledger rows.
	Channel string // defaults to "feishu"
	// Actor stamped on emitted events.
	Actor observability.Actor
}

// Service is the FeishuOutboundDispatcher long-running goroutine. Start()
// spawns the loop; Stop() joins after the current batch finishes.
type Service struct {
	deps Deps
	cfg  Config

	mu      sync.Mutex
	running bool
	stopCh  chan struct{}
	doneCh  chan struct{}
}

// NewService constructs a Service applying defaults.
func NewService(deps Deps, cfg Config) (*Service, error) {
	if deps.DB == nil {
		return nil, errors.New("dispatcher: DB required")
	}
	if deps.IDGen == nil {
		return nil, errors.New("dispatcher: IDGen required")
	}
	if deps.Events == nil || deps.Sink == nil {
		return nil, errors.New("dispatcher: Events + Sink required")
	}
	if deps.Cursor == nil {
		return nil, errors.New("dispatcher: Cursor store required")
	}
	if deps.Conversations == nil || deps.Messages == nil {
		return nil, errors.New("dispatcher: Conversations + Messages repos required")
	}
	if deps.Ledger == nil {
		return nil, errors.New("dispatcher: Ledger repo required")
	}
	if deps.Client == nil {
		return nil, errors.New("dispatcher: feishu Client required")
	}
	if deps.Renderer == nil {
		return nil, errors.New("dispatcher: Renderer required")
	}
	if deps.Clock == nil {
		deps.Clock = clock.SystemClock{}
	}
	if cfg.PollInterval <= 0 {
		cfg.PollInterval = 250 * time.Millisecond
	}
	if cfg.BatchSize <= 0 {
		cfg.BatchSize = 100
	}
	if cfg.Channel == "" {
		cfg.Channel = "feishu"
	}
	if cfg.Actor == "" {
		cfg.Actor = observability.Actor("system")
	}
	return &Service{deps: deps, cfg: cfg}, nil
}

// Start launches the dispatcher loop in a goroutine. Idempotent.
func (s *Service) Start(ctx context.Context) error {
	s.mu.Lock()
	if s.running {
		s.mu.Unlock()
		return nil
	}
	s.running = true
	s.stopCh = make(chan struct{})
	s.doneCh = make(chan struct{})
	s.mu.Unlock()

	go s.loop(ctx)
	return nil
}

// Done returns a channel that is closed when the loop has exited (whether
// via Stop, ctx cancel, or another path). Returns nil before Start. Tests
// use this to deterministically join the goroutine without forcing a stopCh
// signal (which would race the ctx.Done branch and cause coverage flap).
func (s *Service) Done() <-chan struct{} {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.doneCh
}

// Stop signals the loop and waits for the current batch to finish.
func (s *Service) Stop() {
	s.mu.Lock()
	if !s.running {
		s.mu.Unlock()
		return
	}
	close(s.stopCh)
	s.running = false
	done := s.doneCh
	s.mu.Unlock()
	if done != nil {
		<-done
	}
}

// RunOnce processes one batch synchronously. Useful in tests + for the
// integration suite (no need to spawn the goroutine).
func (s *Service) RunOnce(ctx context.Context) (processed int, err error) {
	cursor, err := s.deps.Cursor.Load(ctx, SubscriberName)
	if err != nil {
		return 0, fmt.Errorf("load cursor: %w", err)
	}
	filter := observability.EventQueryFilter{Limit: s.cfg.BatchSize}
	if cursor != "" {
		c := observability.EventID(cursor)
		filter.Cursor = &c
	}
	events, err := s.deps.Events.Find(ctx, filter)
	if err != nil {
		return 0, fmt.Errorf("find events: %w", err)
	}
	var last observability.EventID
	for _, ev := range events {
		if err := s.handleEvent(ctx, ev); err != nil {
			// handleEvent already emitted observability event for the
			// failure; we still advance the cursor so a poisonous event
			// doesn't pin the queue. The emission keeps it discoverable.
			_ = err
		}
		last = ev.ID()
		processed++
	}
	if last != "" {
		if err := s.deps.Cursor.Save(ctx, SubscriberName, string(last)); err != nil {
			return processed, fmt.Errorf("save cursor: %w", err)
		}
	}
	return processed, nil
}

func (s *Service) loop(ctx context.Context) {
	defer close(s.doneCh)
	// Pattern mirrors observability/escalator + taskruntime/timeoutscan: one
	// ticker-driven select with exactly two exit cases. We run one batch
	// immediately so the first RunOnce doesn't wait for the first tick.
	s.runIteration(ctx)
	t := time.NewTicker(s.cfg.PollInterval)
	defer t.Stop()
	for {
		select {
		case <-s.stopCh:
			return
		case <-ctx.Done():
			return
		case <-t.C:
			s.runIteration(ctx)
		}
	}
}

// runIteration runs a single batch and emits the loop-failure observability
// event on error (§ 17 — never silently log).
func (s *Service) runIteration(ctx context.Context) {
	if _, err := s.RunOnce(ctx); err != nil {
		_, _ = s.deps.Sink.Emit(ctx, observability.EmitCommand{
			EventType: "bridge.feishu.dispatch_loop_failed",
			Actor:     s.cfg.Actor,
			Payload: map[string]any{
				"reason":  "loop_iteration_failed",
				"message": err.Error(),
			},
		})
	}
}
