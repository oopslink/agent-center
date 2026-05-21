package cli

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/oopslink/agent-center/internal/workforce"
)

// stubProjectRepoForChecker satisfies workforce.ProjectRepository — only
// FindByID is exercised by projectCheckerAdapter; the rest are no-ops.
type stubProjectRepoForChecker struct {
	existing map[workforce.ProjectID]*workforce.Project
	err      error
}

func (s *stubProjectRepoForChecker) Save(context.Context, *workforce.Project) error { return nil }
func (s *stubProjectRepoForChecker) FindByID(_ context.Context, id workforce.ProjectID) (*workforce.Project, error) {
	if s.err != nil {
		return nil, s.err
	}
	if p, ok := s.existing[id]; ok {
		return p, nil
	}
	return nil, workforce.ErrProjectNotFound
}
func (s *stubProjectRepoForChecker) FindAll(context.Context, workforce.ProjectFilter) ([]*workforce.Project, error) {
	return nil, nil
}
func (s *stubProjectRepoForChecker) Update(context.Context, workforce.ProjectID, workforce.ProjectUpdateFields, int, time.Time) (*workforce.Project, error) {
	return nil, nil
}
func (s *stubProjectRepoForChecker) Delete(context.Context, workforce.ProjectID) error { return nil }

func TestProjectCheckerAdapter_FoundExisting(t *testing.T) {
	repo := &stubProjectRepoForChecker{
		existing: map[workforce.ProjectID]*workforce.Project{},
	}
	p, err := workforce.NewProject(workforce.NewProjectInput{
		ID: "p-1", Name: "n", CreatedByIdentityID: "user:hayang", CreatedAt: time.Now(),
	})
	if err != nil {
		t.Fatal(err)
	}
	repo.existing["p-1"] = p
	adp := projectCheckerAdapter{repo: repo}
	ok, err := adp.ProjectExists(context.Background(), "p-1")
	if err != nil {
		t.Fatalf("ProjectExists: %v", err)
	}
	if !ok {
		t.Fatal("expected true")
	}
}

func TestProjectCheckerAdapter_NotFound(t *testing.T) {
	repo := &stubProjectRepoForChecker{existing: map[workforce.ProjectID]*workforce.Project{}}
	adp := projectCheckerAdapter{repo: repo}
	ok, err := adp.ProjectExists(context.Background(), "missing")
	if err != nil {
		t.Fatalf("ProjectExists: %v", err)
	}
	if ok {
		t.Fatal("expected false")
	}
}

func TestProjectCheckerAdapter_RepoError(t *testing.T) {
	wantErr := errors.New("boom")
	repo := &stubProjectRepoForChecker{err: wantErr}
	adp := projectCheckerAdapter{repo: repo}
	ok, err := adp.ProjectExists(context.Background(), "p")
	if !errors.Is(err, wantErr) {
		t.Fatalf("expected wrapped boom, got %v", err)
	}
	if ok {
		t.Fatal("expected false")
	}
}
