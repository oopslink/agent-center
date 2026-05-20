package sqlite

import (
	"database/sql"
	"errors"
	"testing"
	"time"
)

func TestNullStringFromInt64(t *testing.T) {
	if got := nullStringFromInt64(0, true); got != nil {
		t.Fatalf("expected nil: %v", got)
	}
	if got := nullStringFromInt64(0, false); got == nil {
		t.Fatal("expected non-nil")
	}
	if got := nullStringFromInt64(5, true); got == nil {
		t.Fatal("expected non-nil 5")
	}
}

func TestNullTimeStr_ZeroAndNonZero(t *testing.T) {
	if got := nullTimeStr(time.Time{}); got != nil {
		t.Fatalf("zero should be nil: %v", got)
	}
	if got := nullTimeStr(time.Now()); got == nil {
		t.Fatal("expected non-nil")
	}
}

func TestIsForeignKeyConstraint(t *testing.T) {
	if IsForeignKeyConstraint(nil) {
		t.Fatal("nil err")
	}
	if !IsForeignKeyConstraint(errors.New("FOREIGN KEY constraint failed")) {
		t.Fatal("standard FK err")
	}
	if !IsForeignKeyConstraint(errors.New("constraint failed: FOREIGN KEY")) {
		t.Fatal("alt FK err")
	}
	if IsForeignKeyConstraint(errors.New("random")) {
		t.Fatal("should not match")
	}
}

func TestParseDurationFromInt(t *testing.T) {
	if got := parseDurationFromInt(sql.NullInt64{}); got != nil {
		t.Fatal("invalid → nil")
	}
	got := parseDurationFromInt(sql.NullInt64{Valid: true, Int64: 60})
	if got == nil || *got != 60*time.Second {
		t.Fatalf("expected 60s: %v", got)
	}
}

func TestErrNotMatchedSentinel(t *testing.T) {
	if errNotMatched == nil {
		t.Fatal("expected sentinel")
	}
}

func TestNullDuration(t *testing.T) {
	if got := nullDuration(nil); got != nil {
		t.Fatal("expected nil")
	}
	d := 3 * time.Second
	if got := nullDuration(&d); got == nil {
		t.Fatal("expected non-nil")
	}
}

func TestMarshalAndUnmarshalStringList(t *testing.T) {
	if got, _ := marshalStringList(nil); got != "[]" {
		t.Fatalf("empty: %s", got)
	}
	out, err := marshalStringList([]string{"a", "b"})
	if err != nil {
		t.Fatal(err)
	}
	if out != `["a","b"]` {
		t.Fatalf("%s", out)
	}
	parsed, err := unmarshalStringList(out)
	if err != nil {
		t.Fatal(err)
	}
	if len(parsed) != 2 || parsed[0] != "a" {
		t.Fatalf("%+v", parsed)
	}
	if _, err := unmarshalStringList(""); err != nil {
		t.Fatal(err)
	}
	if _, err := unmarshalStringList("not-json"); err == nil {
		t.Fatal("expected error")
	}
}

func TestIsUniqueConstraint(t *testing.T) {
	if IsUniqueConstraint(nil) {
		t.Fatal("nil")
	}
	if !IsUniqueConstraint(errors.New("UNIQUE constraint failed")) {
		t.Fatal("std")
	}
	if !IsUniqueConstraint(errors.New("constraint failed: UNIQUE on x")) {
		t.Fatal("alt")
	}
	if IsUniqueConstraint(errors.New("random")) {
		t.Fatal("no match")
	}
}

func TestParseTimeStr_InvalidAndEmpty(t *testing.T) {
	got, err := parseTimeStr(sql.NullString{Valid: false})
	if err != nil || !got.IsZero() {
		t.Fatal("invalid expects zero")
	}
	got, err = parseTimeStr(sql.NullString{Valid: true, String: "  "})
	if err != nil || !got.IsZero() {
		t.Fatal("blank expects zero")
	}
	if _, err := parseTimeStr(sql.NullString{Valid: true, String: "not-time"}); err == nil {
		t.Fatal("expected err")
	}
}

func TestParseTimePtrStr_EmptyAndError(t *testing.T) {
	got, err := parseTimePtrStr(sql.NullString{Valid: false})
	if err != nil || got != nil {
		t.Fatal("invalid expects nil")
	}
	got, err = parseTimePtrStr(sql.NullString{Valid: true, String: ""})
	if err != nil || got != nil {
		t.Fatal("blank expects nil")
	}
	if _, err := parseTimePtrStr(sql.NullString{Valid: true, String: "bogus"}); err == nil {
		t.Fatal("expected err")
	}
}
