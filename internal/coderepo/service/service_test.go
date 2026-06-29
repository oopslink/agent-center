package service

import (
	"context"
	"database/sql"
	"testing"
	"time"

	"github.com/oopslink/agent-center/internal/clock"
	"github.com/oopslink/agent-center/internal/coderepo"
	coderepsql "github.com/oopslink/agent-center/internal/coderepo/sqlite"
	"github.com/oopslink/agent-center/internal/idgen"
	"github.com/oopslink/agent-center/internal/persistence"
	pmsql "github.com/oopslink/agent-center/internal/projectmanager/sqlite"
	"github.com/oopslink/agent-center/internal/secretmgmt"
)

func newSvc(t *testing.T) (*Service, *sql.DB, *pmsql.CodeRepoRefRepo) {
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
	refRepo := pmsql.NewCodeRepoRefRepo(db)
	svc := New(Deps{
		DB: db, Repos: coderepsql.NewRepoRepo(db), IDGen: idgen.NewGenerator(clk),
		Clock: clk, MasterKey: mk, Unlinker: refRepo,
	})
	return svc, db, refRepo
}

func TestCreateRepo_WithAndWithoutCredential(t *testing.T) {
	svc, _, _ := newSvc(t)
	ctx := context.Background()

	// With credential → encrypted, has_credential true, ciphertext != plaintext.
	id, err := svc.CreateRepo(ctx, CreateRepoCommand{
		OrgID: "org1", Label: "app", URL: "https://github.com/o/app", Provider: coderepo.ProviderGitHub,
		Credential: "ghp_secret_token", CreatedBy: "user:a",
	})
	if err != nil {
		t.Fatal(err)
	}
	repo, err := svc.GetRepo(ctx, id)
	if err != nil {
		t.Fatal(err)
	}
	if !repo.HasCredential() {
		t.Fatal("repo should have a credential")
	}
	if string(repo.CredentialCiphertext()) == "ghp_secret_token" {
		t.Fatal("credential MUST be encrypted at rest, not plaintext")
	}
	if len(repo.CredentialNonce()) == 0 {
		t.Fatal("encrypted credential must carry a nonce")
	}

	// Without credential → none.
	id2, err := svc.CreateRepo(ctx, CreateRepoCommand{
		OrgID: "org1", Label: "pub", URL: "https://github.com/o/pub", Provider: coderepo.ProviderGit, CreatedBy: "user:a",
	})
	if err != nil {
		t.Fatal(err)
	}
	repo2, _ := svc.GetRepo(ctx, id2)
	if repo2.HasCredential() {
		t.Fatal("repo created without credential must have none")
	}
}

func TestCreateRepo_Validation(t *testing.T) {
	svc, _, _ := newSvc(t)
	ctx := context.Background()
	if _, err := svc.CreateRepo(ctx, CreateRepoCommand{OrgID: "o", Label: "x", URL: "u", Provider: "svn", CreatedBy: "user:a"}); err != coderepo.ErrInvalidProvider {
		t.Fatalf("bad provider err = %v, want ErrInvalidProvider", err)
	}
	if _, err := svc.CreateRepo(ctx, CreateRepoCommand{OrgID: "o", Label: "", URL: "u", Provider: coderepo.ProviderGit, CreatedBy: "user:a"}); err != coderepo.ErrLabelRequired {
		t.Fatalf("empty label err = %v, want ErrLabelRequired", err)
	}
}

func TestUpdateRepo_InfoAndCredentialTriState(t *testing.T) {
	svc, _, _ := newSvc(t)
	ctx := context.Background()
	id, _ := svc.CreateRepo(ctx, CreateRepoCommand{
		OrgID: "o", Label: "app", URL: "u1", Provider: coderepo.ProviderGitHub, Credential: "tok", CreatedBy: "user:a",
	})

	// nil credential → unchanged (keeps the existing one); info edited.
	if err := svc.UpdateRepo(ctx, UpdateRepoCommand{ID: id, Label: "app2", URL: "u2", Provider: coderepo.ProviderGitLab, DefaultBranch: "main"}); err != nil {
		t.Fatal(err)
	}
	repo, _ := svc.GetRepo(ctx, id)
	if repo.Label() != "app2" || repo.URL() != "u2" || repo.Provider() != coderepo.ProviderGitLab || repo.DefaultBranch() != "main" {
		t.Fatalf("info not updated: %+v", repo)
	}
	if !repo.HasCredential() {
		t.Fatal("nil credential must leave the existing credential intact")
	}

	// empty string credential → CLEAR.
	empty := ""
	if err := svc.UpdateRepo(ctx, UpdateRepoCommand{ID: id, Label: "app2", URL: "u2", Provider: coderepo.ProviderGitLab, Credential: &empty}); err != nil {
		t.Fatal(err)
	}
	if repo, _ = svc.GetRepo(ctx, id); repo.HasCredential() {
		t.Fatal("empty-string credential must clear it")
	}

	// non-empty credential → replace.
	tok := "newtok"
	if err := svc.UpdateRepo(ctx, UpdateRepoCommand{ID: id, Label: "app2", URL: "u2", Provider: coderepo.ProviderGitLab, Credential: &tok}); err != nil {
		t.Fatal(err)
	}
	if repo, _ = svc.GetRepo(ctx, id); !repo.HasCredential() {
		t.Fatal("non-empty credential must set it")
	}
}

func TestDeleteRepo_UnrefsProjects(t *testing.T) {
	svc, db, refRepo := newSvc(t)
	ctx := context.Background()
	id, _ := svc.CreateRepo(ctx, CreateRepoCommand{OrgID: "o", Label: "app", URL: "u", Provider: coderepo.ProviderGit, CreatedBy: "user:a"})

	// Seed two project refs pointing at the repo (raw, no pm service needed).
	for _, p := range []string{"P1", "P2"} {
		if _, err := db.ExecContext(ctx,
			`INSERT INTO pm_code_repo_refs (id, project_id, url, added_by, created_at, repo_id, is_primary)
			 VALUES (?,?,?,?,?,?,1)`, "ref-"+p, p, "", "user:a", "2026-06-29T00:00:00Z", id); err != nil {
			t.Fatal(err)
		}
	}
	if n, _ := svc.CountReferencingProjects(ctx, id); n != 2 {
		t.Fatalf("referencing count = %d, want 2", n)
	}
	unlinked, err := svc.DeleteRepo(ctx, id)
	if err != nil {
		t.Fatal(err)
	}
	if unlinked != 2 {
		t.Fatalf("unlinked = %d, want 2", unlinked)
	}
	// Repo gone; refs survive but repo_id cleared (url-only fallback), is_primary 0.
	if _, err := svc.GetRepo(ctx, id); err != coderepo.ErrRepoNotFound {
		t.Fatalf("repo should be deleted, err = %v", err)
	}
	ref, _ := refRepo.FindByID(ctx, "ref-P1")
	if ref.RepoID() != "" || ref.IsPrimary() {
		t.Fatalf("ref should be unlinked: repo_id=%q is_primary=%v", ref.RepoID(), ref.IsPrimary())
	}
}

func TestRepoURL_ResolverPort(t *testing.T) {
	svc, _, _ := newSvc(t)
	ctx := context.Background()
	id, _ := svc.CreateRepo(ctx, CreateRepoCommand{OrgID: "o", Label: "app", URL: "https://x/app", Provider: coderepo.ProviderGit, CreatedBy: "user:a"})
	if url, err := svc.RepoURL(ctx, id); err != nil || url != "https://x/app" {
		t.Fatalf("RepoURL = (%q, %v), want (https://x/app, nil)", url, err)
	}
	// Unknown repo → ("", nil) so the merge-check falls back to the ref's own url.
	if url, err := svc.RepoURL(ctx, "nope"); err != nil || url != "" {
		t.Fatalf("RepoURL(unknown) = (%q, %v), want (\"\", nil)", url, err)
	}
}

func TestCreateRepo_CredentialRequiresMasterKey(t *testing.T) {
	db, err := persistence.Open(persistence.MemoryDSN())
	if err != nil {
		t.Fatal(err)
	}
	if err := persistence.NewMigrator(db).Up(context.Background()); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	clk := clock.NewFakeClock(time.Unix(1_700_000_000, 0).UTC())
	svc := New(Deps{DB: db, Repos: coderepsql.NewRepoRepo(db), IDGen: idgen.NewGenerator(clk), Clock: clk}) // no master key
	_, err = svc.CreateRepo(context.Background(), CreateRepoCommand{
		OrgID: "o", Label: "x", URL: "u", Provider: coderepo.ProviderGit, Credential: "tok", CreatedBy: "user:a",
	})
	if err != secretmgmt.ErrMasterKeyNotLoaded {
		t.Fatalf("credential write without master key err = %v, want ErrMasterKeyNotLoaded", err)
	}
}
