package sqlite

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/oopslink/agent-center/internal/conversation"
	"github.com/oopslink/agent-center/internal/discussion"
)

// nullString returns nil for "", else the string. SQLite stores NULL
// instead of "" so reads can distinguish absent-from-empty values.
func nullString(s string) any {
	if s == "" {
		return nil
	}
	return s
}

// nullTimePtr returns nil for nil pointer, else the RFC3339Nano string.
func nullTimePtr(t *time.Time) any {
	if t == nil {
		return nil
	}
	return t.UTC().Format(time.RFC3339Nano)
}

// isUniqueConstraint detects SQLite UNIQUE constraint failures.
func isUniqueConstraint(err error) bool {
	if err == nil {
		return false
	}
	msg := err.Error()
	return strings.Contains(msg, "UNIQUE constraint failed") ||
		strings.Contains(msg, "constraint failed: UNIQUE")
}

// parseNullTime parses an optional RFC3339Nano timestamp.
func parseNullTime(s sql.NullString) (*time.Time, error) {
	if !s.Valid || s.String == "" {
		return nil, nil
	}
	t, err := time.Parse(time.RFC3339Nano, s.String)
	if err != nil {
		return nil, fmt.Errorf("parse time %q: %w", s.String, err)
	}
	tt := t.UTC()
	return &tt, nil
}

// marshalStringList canonicalises a string slice to a JSON array. nil /
// empty → "[]".
func marshalStringList(ss []string) (string, error) {
	if len(ss) == 0 {
		return "[]", nil
	}
	b, err := json.Marshal(ss)
	if err != nil {
		return "", err
	}
	return string(b), nil
}

func scanIssues(rows *sql.Rows) ([]*discussion.Issue, error) {
	var out []*discussion.Issue
	for rows.Next() {
		i, err := scanIssue(rows.Scan)
		if err != nil {
			return nil, err
		}
		out = append(out, i)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return out, nil
}

func scanIssue(scan func(...any) error) (*discussion.Issue, error) {
	var (
		id, projectID, title, description string
		descBlobRef                       sql.NullString
		openedBy, origin                  string
		openedAtRaw, status               string
		concludedAtRaw                    sql.NullString
		conclusionSummary                 sql.NullString
		concludedByIdentity               sql.NullString
		withdrawReason, withdrawMessage   sql.NullString
		conversationID                    sql.NullString
		relatedJSON                       string
		createdAtRaw, updatedAtRaw        string
		version                           int
	)
	if err := scan(&id, &projectID, &title, &description, &descBlobRef,
		&openedBy, &origin, &openedAtRaw, &status,
		&concludedAtRaw, &conclusionSummary, &concludedByIdentity,
		&withdrawReason, &withdrawMessage,
		&conversationID, &relatedJSON,
		&createdAtRaw, &updatedAtRaw, &version); err != nil {
		return nil, err
	}
	openedAt, err := time.Parse(time.RFC3339Nano, openedAtRaw)
	if err != nil {
		return nil, fmt.Errorf("parse opened_at: %w", err)
	}
	createdAt, err := time.Parse(time.RFC3339Nano, createdAtRaw)
	if err != nil {
		return nil, fmt.Errorf("parse created_at: %w", err)
	}
	updatedAt, err := time.Parse(time.RFC3339Nano, updatedAtRaw)
	if err != nil {
		return nil, fmt.Errorf("parse updated_at: %w", err)
	}
	concAt, err := parseNullTime(concludedAtRaw)
	if err != nil {
		return nil, fmt.Errorf("parse concluded_at: %w", err)
	}
	related, err := discussion.UnmarshalRelatedConversationIDsJSON(relatedJSON)
	if err != nil {
		return nil, err
	}
	return discussion.RehydrateIssue(discussion.RehydrateIssueInput{
		ID:                     discussion.IssueID(id),
		ProjectID:              projectID,
		Title:                  title,
		Description:            description,
		DescriptionBlobRef:     descBlobRef.String,
		OpenedByIdentityID:     openedBy,
		Origin:                 discussion.Origin(origin),
		OpenedAt:               openedAt,
		Status:                 discussion.Status(status),
		ConcludedAt:            concAt,
		ConclusionSummary:      conclusionSummary.String,
		ConcludedByIdentityID:  concludedByIdentity.String,
		WithdrawReason:         withdrawReason.String,
		WithdrawMessage:        withdrawMessage.String,
		ConversationID:         conversation.ConversationID(conversationID.String),
		RelatedConversationIDs: related,
		CreatedAt:              createdAt,
		UpdatedAt:              updatedAt,
		Version:                version,
	})
}
