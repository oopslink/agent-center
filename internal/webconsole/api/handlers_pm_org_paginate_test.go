package api

import (
	"net/http/httptest"
	"testing"
)

// Server-side pagination + sort for the org list handlers (issues/tasks/plans)
// and the reminders list. parsePageParams reads sort/dir/limit/offset (and the
// page/page_size convenience), applyPageItems sorts + slices + reports total.

func TestParsePageParams_Defaults(t *testing.T) {
	r := httptest.NewRequest("GET", "/api/tasks", nil)
	pp := parsePageParams(r)
	if pp.sortKey != "" || pp.sortDir != "" || pp.limit != 0 || pp.offset != 0 {
		t.Fatalf("empty query → zero params, got %+v", pp)
	}
}

func TestParsePageParams_LimitOffsetAndSort(t *testing.T) {
	r := httptest.NewRequest("GET", "/api/tasks?sort=title&dir=asc&limit=10&offset=20", nil)
	pp := parsePageParams(r)
	if pp.sortKey != "title" || pp.sortDir != "asc" || pp.limit != 10 || pp.offset != 20 {
		t.Fatalf("parsed = %+v", pp)
	}
}

func TestParsePageParams_PageSizeConvenience(t *testing.T) {
	r := httptest.NewRequest("GET", "/api/tasks?page=3&page_size=25", nil)
	pp := parsePageParams(r)
	if pp.limit != 25 || pp.offset != 50 { // (3-1)*25
		t.Fatalf("page/page_size → limit=25 offset=50, got %+v", pp)
	}
}

func TestParsePageParams_RejectsUnknownSortKey(t *testing.T) {
	r := httptest.NewRequest("GET", "/api/tasks?sort=evil_column&dir=sideways", nil)
	pp := parsePageParams(r)
	if pp.sortKey != "" {
		t.Fatalf("unknown sort key must be dropped, got %q", pp.sortKey)
	}
	if pp.sortDir != "" {
		t.Fatalf("invalid dir must be dropped, got %q", pp.sortDir)
	}
}

func rows() []map[string]any {
	return []map[string]any{
		{"id": "a", "title": "Banana", "status": "open", "updated_at": "2026-06-01T00:00:00Z"},
		{"id": "b", "title": "Apple", "status": "closed", "updated_at": "2026-06-03T00:00:00Z"},
		{"id": "c", "title": "Cherry", "status": "open", "updated_at": "2026-06-02T00:00:00Z"},
	}
}

func TestApplyPageItems_DefaultUpdatedDesc(t *testing.T) {
	page, total := applyPageItems(rows(), pageParams{})
	if total != 3 {
		t.Fatalf("total=%d want 3", total)
	}
	// default = updated_at DESC → b (06-03), c (06-02), a (06-01).
	got := []string{page[0]["id"].(string), page[1]["id"].(string), page[2]["id"].(string)}
	if got[0] != "b" || got[1] != "c" || got[2] != "a" {
		t.Fatalf("default order = %v want [b c a]", got)
	}
}

func TestApplyPageItems_SortTitleAsc(t *testing.T) {
	page, _ := applyPageItems(rows(), pageParams{sortKey: "title", sortDir: "asc"})
	got := []string{page[0]["title"].(string), page[1]["title"].(string), page[2]["title"].(string)}
	if got[0] != "Apple" || got[1] != "Banana" || got[2] != "Cherry" {
		t.Fatalf("title asc = %v", got)
	}
}

func TestApplyPageItems_Paginates(t *testing.T) {
	// title asc, page size 2, second page → just "Cherry"; total stays 3.
	page, total := applyPageItems(rows(), pageParams{sortKey: "title", sortDir: "asc", limit: 2, offset: 2})
	if total != 3 {
		t.Fatalf("total=%d want 3 (pre-slice count)", total)
	}
	if len(page) != 1 || page[0]["title"].(string) != "Cherry" {
		t.Fatalf("second page = %v want [Cherry]", page)
	}
}

func TestApplyPageItems_OffsetPastEnd(t *testing.T) {
	page, total := applyPageItems(rows(), pageParams{limit: 10, offset: 99})
	if total != 3 || len(page) != 0 {
		t.Fatalf("offset past end → empty page, total preserved; got len=%d total=%d", len(page), total)
	}
}
