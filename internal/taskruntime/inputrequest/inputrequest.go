package inputrequest

import (
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/oopslink/agent-center/internal/taskruntime"
)

// InputRequest is the independent AR (03-input-request.md).
//
// Invariants per § 9:
//  1. task_execution_id immutable
//  2. terminal state unreachable from terminal
//  3. ended_reason+message paired (conventions § 16)
type InputRequest struct {
	id              taskruntime.InputRequestID
	taskExecutionID taskruntime.TaskExecutionID
	status          Status
	question        string
	options         []string
	urgency         Urgency
	requestedAt     time.Time
	respondedAt     *time.Time
	respondedBy     string
	responseText    string
	endedReason     string
	endedMessage    string
	createdAt       time.Time
	updatedAt       time.Time
	version         int
}

// NewInput captures the constructor args.
type NewInput struct {
	ID              taskruntime.InputRequestID
	TaskExecutionID taskruntime.TaskExecutionID
	Question        string
	Options         []string
	Urgency         Urgency
	Now             time.Time
}

// New constructs a fresh pending InputRequest.
func New(in NewInput) (*InputRequest, error) {
	if strings.TrimSpace(string(in.ID)) == "" {
		return nil, errors.New("input_request: id required")
	}
	if strings.TrimSpace(string(in.TaskExecutionID)) == "" {
		return nil, errors.New("input_request: task_execution_id required")
	}
	if strings.TrimSpace(in.Question) == "" {
		return nil, errors.New("input_request: question required")
	}
	urgency := in.Urgency
	if urgency == "" {
		urgency = UrgencyNormal
	}
	if !urgency.IsValid() {
		return nil, fmt.Errorf("%w: %q", ErrInvalidUrgency, urgency)
	}
	if in.Now.IsZero() {
		return nil, errors.New("input_request: now required")
	}
	now := in.Now.UTC()
	return &InputRequest{
		id:              in.ID,
		taskExecutionID: in.TaskExecutionID,
		status:          StatusPending,
		question:        in.Question,
		options:         append([]string(nil), in.Options...),
		urgency:         urgency,
		requestedAt:     now,
		createdAt:       now,
		updatedAt:       now,
		version:         1,
	}, nil
}

// RehydrateInput is for repository round-trip.
type RehydrateInput struct {
	ID              taskruntime.InputRequestID
	TaskExecutionID taskruntime.TaskExecutionID
	Status          Status
	Question        string
	Options         []string
	Urgency         Urgency
	RequestedAt     time.Time
	RespondedAt     *time.Time
	RespondedBy     string
	ResponseText    string
	EndedReason     string
	EndedMessage    string
	CreatedAt       time.Time
	UpdatedAt       time.Time
	Version         int
}

// Rehydrate reconstructs without invariant checks.
func Rehydrate(in RehydrateInput) (*InputRequest, error) {
	if !in.Status.IsValid() {
		return nil, ErrInvalidStatus
	}
	if !in.Urgency.IsValid() {
		return nil, fmt.Errorf("%w: %q", ErrInvalidUrgency, in.Urgency)
	}
	if in.Version < 1 {
		return nil, errors.New("input_request: version must be >= 1")
	}
	var respCopy *time.Time
	if in.RespondedAt != nil {
		v := in.RespondedAt.UTC()
		respCopy = &v
	}
	return &InputRequest{
		id:              in.ID,
		taskExecutionID: in.TaskExecutionID,
		status:          in.Status,
		question:        in.Question,
		options:         append([]string(nil), in.Options...),
		urgency:         in.Urgency,
		requestedAt:     in.RequestedAt.UTC(),
		respondedAt:     respCopy,
		respondedBy:     in.RespondedBy,
		responseText:    in.ResponseText,
		endedReason:     in.EndedReason,
		endedMessage:    in.EndedMessage,
		createdAt:       in.CreatedAt.UTC(),
		updatedAt:       in.UpdatedAt.UTC(),
		version:         in.Version,
	}, nil
}

// Getters.
func (r *InputRequest) ID() taskruntime.InputRequestID              { return r.id }
func (r *InputRequest) TaskExecutionID() taskruntime.TaskExecutionID { return r.taskExecutionID }
func (r *InputRequest) Status() Status                              { return r.status }
func (r *InputRequest) Question() string                            { return r.question }
func (r *InputRequest) Options() []string                           { return append([]string(nil), r.options...) }
func (r *InputRequest) Urgency() Urgency                            { return r.urgency }
func (r *InputRequest) RequestedAt() time.Time                      { return r.requestedAt }
func (r *InputRequest) RespondedAt() *time.Time {
	if r.respondedAt == nil {
		return nil
	}
	v := r.respondedAt.UTC()
	return &v
}
func (r *InputRequest) RespondedBy() string  { return r.respondedBy }
func (r *InputRequest) ResponseText() string { return r.responseText }
func (r *InputRequest) EndedReason() string  { return r.endedReason }
func (r *InputRequest) EndedMessage() string { return r.endedMessage }
func (r *InputRequest) CreatedAt() time.Time { return r.createdAt }
func (r *InputRequest) UpdatedAt() time.Time { return r.updatedAt }
func (r *InputRequest) Version() int         { return r.version }

// OptionsJSON returns options as JSON or "" if empty.
func (r *InputRequest) OptionsJSON() (string, error) {
	if len(r.options) == 0 {
		return "", nil
	}
	b, err := json.Marshal(r.options)
	if err != nil {
		return "", err
	}
	return string(b), nil
}

// Respond transitions pending→responded with the given response.
func (r *InputRequest) Respond(resp InputResponse) error {
	if r.status != StatusPending {
		return fmt.Errorf("%w: %s → responded not allowed", ErrInvalidTransition, r.status)
	}
	if err := resp.Validate(); err != nil {
		return err
	}
	at := resp.DecidedAt.UTC()
	r.status = StatusResponded
	r.respondedAt = &at
	r.respondedBy = resp.DecidedBy
	r.responseText = resp.Answer
	r.updatedAt = at
	r.version++
	return nil
}

// MarkTimedOut transitions pending→timed_out (TimeoutScanner T2).
func (r *InputRequest) MarkTimedOut(reason, message string, now time.Time) error {
	if r.status != StatusPending {
		return fmt.Errorf("%w: %s → timed_out not allowed", ErrInvalidTransition, r.status)
	}
	if strings.TrimSpace(reason) == "" {
		return errors.New("input_request: timed_out reason required (conventions § 16)")
	}
	if strings.TrimSpace(message) == "" {
		return errors.New("input_request: timed_out message required (conventions § 16)")
	}
	r.status = StatusTimedOut
	r.endedReason = reason
	r.endedMessage = message
	r.updatedAt = now.UTC()
	r.version++
	return nil
}

// MarkCanceled transitions pending→canceled (KillCoordinator联动).
func (r *InputRequest) MarkCanceled(reason, message string, now time.Time) error {
	if r.status != StatusPending {
		return fmt.Errorf("%w: %s → canceled not allowed", ErrInvalidTransition, r.status)
	}
	if strings.TrimSpace(reason) == "" {
		return errors.New("input_request: canceled reason required (conventions § 16)")
	}
	if strings.TrimSpace(message) == "" {
		return errors.New("input_request: canceled message required (conventions § 16)")
	}
	r.status = StatusCanceled
	r.endedReason = reason
	r.endedMessage = message
	r.updatedAt = now.UTC()
	r.version++
	return nil
}
