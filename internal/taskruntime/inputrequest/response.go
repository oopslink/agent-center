package inputrequest

import (
	"errors"
	"strings"
	"time"
)

// InputResponse is the VO carrying response payload.
type InputResponse struct {
	Answer     string
	DecidedBy  string
	DecidedAt  time.Time
}

// Validate enforces non-empty Answer + DecidedBy.
func (r InputResponse) Validate() error {
	if strings.TrimSpace(r.Answer) == "" {
		return errors.New("input_response: answer required")
	}
	if strings.TrimSpace(r.DecidedBy) == "" {
		return errors.New("input_response: decided_by required")
	}
	if r.DecidedAt.IsZero() {
		return errors.New("input_response: decided_at required")
	}
	return nil
}
