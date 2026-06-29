package service

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/oopslink/agent-center/internal/clock"
	"github.com/oopslink/agent-center/internal/coderepo"
	"github.com/oopslink/agent-center/internal/coderepo/provider"
	coderepsql "github.com/oopslink/agent-center/internal/coderepo/sqlite"
	"github.com/oopslink/agent-center/internal/idgen"
	"github.com/oopslink/agent-center/internal/persistence"
	"github.com/oopslink/agent-center/internal/secretmgmt"
)

// captureProvider records the Target it received so the test can assert the
// service decrypted the credential and passed the right descriptor.
type captureProvider struct {
	got provider.Target
}

func (c *captureProvider) ListCommits(_ context.Context, t provider.Target, _ string, _ int) ([]provider.Commit, error) {
	c.got = t
	return []provider.Commit{{SHA: "abc"}}, nil
}

func (c *captureProvider) ListBranches(_ context.Context, t provider.Target) ([]provider.Branch, error) {
	c.got = t
	return []provider.Branch{{Name: "main", IsDefault: true}}, nil
}

func newViewSvc(t *testing.T, prov provider.Provider) *Service {
	t.Helper()
	db, err := persistence.Open(persistence.MemoryDSN())
	if err != nil {
		t.Fatal(err)
	}
	if err := persistence.NewMigrator(db).Up(context.Background()); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	mk, _ := secretmgmt.GenerateMasterKey()
	clk := clock.NewFakeClock(time.Unix(1_700_000_000, 0).UTC())
	return New(Deps{
		DB: db, Repos: coderepsql.NewRepoRepo(db), IDGen: idgen.NewGenerator(clk),
		Clock: clk, MasterKey: mk, Providers: prov,
	})
}

func TestListCommits_DecryptsCredentialIntoTarget(t *testing.T) {
	cp := &captureProvider{}
	svc := newViewSvc(t, cp)
	ctx := context.Background()
	id, err := svc.CreateRepo(ctx, CreateRepoCommand{
		OrgID: "org1", Label: "app", URL: "https://github.com/o/app", Provider: coderepo.ProviderGitHub,
		DefaultBranch: "main", Credential: "ghp_secret", CreatedBy: "user:a",
	})
	if err != nil {
		t.Fatal(err)
	}
	commits, err := svc.ListCommits(ctx, id, "", 10)
	if err != nil {
		t.Fatalf("ListCommits: %v", err)
	}
	if len(commits) != 1 || commits[0].SHA != "abc" {
		t.Errorf("commits = %+v", commits)
	}
	// The service must have decrypted the credential and built the Target.
	if cp.got.Credential != "ghp_secret" {
		t.Errorf("Target.Credential = %q, want decrypted plaintext ghp_secret", cp.got.Credential)
	}
	if cp.got.URL != "https://github.com/o/app" || cp.got.Provider != "github" || cp.got.DefaultBranch != "main" {
		t.Errorf("Target = %+v", cp.got)
	}
}

func TestListBranches_NoCredential_EmptyTargetCredential(t *testing.T) {
	cp := &captureProvider{}
	svc := newViewSvc(t, cp)
	ctx := context.Background()
	id, err := svc.CreateRepo(ctx, CreateRepoCommand{
		OrgID: "org1", Label: "pub", URL: "https://github.com/o/pub", Provider: coderepo.ProviderGitHub,
		DefaultBranch: "main", CreatedBy: "user:a",
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := svc.ListBranches(ctx, id); err != nil {
		t.Fatalf("ListBranches: %v", err)
	}
	if cp.got.Credential != "" {
		t.Errorf("Target.Credential = %q, want empty (no credential configured)", cp.got.Credential)
	}
}

func TestViewing_NotConfigured(t *testing.T) {
	svc := newViewSvc(t, nil) // no provider wired
	ctx := context.Background()
	id, _ := svc.CreateRepo(ctx, CreateRepoCommand{
		OrgID: "org1", Label: "app", URL: "https://github.com/o/app", Provider: coderepo.ProviderGitHub, CreatedBy: "user:a",
	})
	if _, err := svc.ListCommits(ctx, id, "", 0); !errors.Is(err, ErrViewingNotConfigured) {
		t.Errorf("ListCommits err = %v, want ErrViewingNotConfigured", err)
	}
	if _, err := svc.ListBranches(ctx, id); !errors.Is(err, ErrViewingNotConfigured) {
		t.Errorf("ListBranches err = %v, want ErrViewingNotConfigured", err)
	}
}

func TestViewing_RepoNotFound(t *testing.T) {
	svc := newViewSvc(t, &captureProvider{})
	if _, err := svc.ListCommits(context.Background(), "nope", "", 0); !errors.Is(err, coderepo.ErrRepoNotFound) {
		t.Errorf("err = %v, want ErrRepoNotFound", err)
	}
}
