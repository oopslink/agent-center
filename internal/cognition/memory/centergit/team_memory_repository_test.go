package centergit

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/oopslink/agent-center/internal/cognition/memory/teammemory"
)

func fixedRepoClock() func() time.Time {
	return func() time.Time { return time.Date(2026, 8, 9, 1, 2, 3, 0, time.UTC) }
}

func newTestTeamMemoryRepo(host *Host, ids ...string) *TeamMemoryRepository {
	next := mustDeterministicIDs(ids...)
	return NewTeamMemoryRepository(host, nil,
		WithProposalIDGen(func() string { return next() }),
		WithRepositoryClock(fixedRepoClock()),
	)
}

func TestTeamMemoryRepository_ProposeIdempotentAndPromoteAddRule(t *testing.T) {
	ctx := context.Background()
	host := NewHost(t.TempDir(), nil)
	repo := newTestTeamMemoryRepo(host, "p-add")

	cmd := teammemory.ProposeCommand{
		ActorRef:       "agent:a1",
		IdempotencyKey: "task-1/finding-1",
		Operation:      teammemory.OperationAdd,
		TargetKind:     teammemory.TargetRule,
		Candidate: &teammemory.Candidate{
			Slug: "review-rigor", Description: "review with evidence", Body: "Require cited evidence.",
			Enabled: boolPtr(true), AppliesTo: []string{"review"},
		},
		Rationale:    "Repeated review misses.",
		EvidenceRefs: []string{"task:task-1"},
	}
	res, err := repo.Propose(ctx, "team-propose", cmd)
	if err != nil {
		t.Fatalf("Propose: %v", err)
	}
	if res.ProposalID != "tmprop-p-add" || res.Status != teammemory.StatusPending || res.RepoCommit == "" {
		t.Fatalf("bad propose result: %+v", res)
	}
	again, err := repo.Propose(ctx, "team-propose", cmd)
	if err != nil {
		t.Fatalf("Propose retry: %v", err)
	}
	if again.ProposalID != res.ProposalID || again.RepoCommit != res.RepoCommit {
		t.Fatalf("idempotent retry returned %+v, want same proposal/commit as %+v", again, res)
	}

	snap, err := NewTeamMemoryConsumer(host, nil).ReadTeamRules(ctx, "team-propose", "review")
	if err != nil {
		t.Fatalf("ReadTeamRules before promote: %v", err)
	}
	if len(snap.Rules) != 0 {
		t.Fatalf("proposal leaked into runtime rules: %+v", snap.Rules)
	}

	co := cloneTeamRepo(t, host, "team-propose")
	if _, err := os.Stat(filepath.Join(co, "proposals", "tmprop-p-add.md")); err != nil {
		t.Fatalf("proposal file missing: %v", err)
	}
	author := strings.TrimSpace(runGit(t, t.TempDir(), co, "log", "-1", "--format=%an <%ae>"))
	if author != "agent-center <team-memory@agent-center.local>" {
		t.Fatalf("git author=%q", author)
	}

	promoted, err := repo.Review(ctx, "team-propose", teammemory.ReviewCommand{
		ActorRef:               "agent:curator",
		ProposalID:             res.ProposalID,
		Action:                 teammemory.ActionPromote,
		ExpectedRepoCommit:     res.RepoCommit,
		ExpectedProposalStatus: teammemory.StatusPending,
		Comment:                "Promote after review.",
	})
	if err != nil {
		t.Fatalf("Review promote: %v", err)
	}
	if promoted.Status != teammemory.StatusPromoted || promoted.SourcePath != "rules/review-rigor-p-add.md" ||
		promoted.EffectiveFor != teammemory.EffectiveForNewSessionsAndForks || promoted.NewCommit == promoted.OldCommit {
		t.Fatalf("bad promote result: %+v", promoted)
	}
	snap, err = NewTeamMemoryConsumer(host, nil).ReadTeamRules(ctx, "team-propose", "review")
	if err != nil {
		t.Fatalf("ReadTeamRules after promote: %v", err)
	}
	if len(snap.Rules) != 1 || snap.Rules[0].SourcePath != promoted.SourcePath || snap.Rules[0].Body != "Require cited evidence." {
		t.Fatalf("promoted rule snapshot: %+v", snap.Rules)
	}
	co = cloneTeamRepo(t, host, "team-propose")
	index, _ := os.ReadFile(filepath.Join(co, indexFile))
	if !strings.Contains(string(index), "[review-rigor](rules/review-rigor-p-add.md)") {
		t.Fatalf("MEMORY.md was not regenerated from rules:\n%s", index)
	}
	p, err := readProposal(co, res.ProposalID)
	if err != nil {
		t.Fatalf("read promoted proposal: %v", err)
	}
	if p.Status != teammemory.StatusPromoted || p.ReviewerRef != "agent:curator" {
		t.Fatalf("proposal status not updated: %+v", p)
	}
	view, err := repo.Get(ctx, "team-propose", res.ProposalID)
	if err != nil {
		t.Fatalf("Get promoted proposal: %v", err)
	}
	if view.Proposal.PromotionCommit != promoted.NewCommit {
		t.Fatalf("promotion commit view=%q want %q", view.Proposal.PromotionCommit, promoted.NewCommit)
	}
}

func TestTeamMemoryRepository_UpdateDisableDeleteCAS(t *testing.T) {
	ctx := context.Background()
	host := NewHost(t.TempDir(), nil)
	repo := newTestTeamMemoryRepo(host, "p-update", "p-disable", "p-delete-bad", "p-delete")
	_, paths, commit, err := repo.BootstrapWithPaths(ctx, "team-cas", TrustedBootstrapCommand{
		ActorRef: "system:bootstrap",
		Source:   "test",
		Entries:  []Entry{{Slug: "lesson", Description: "old lesson", Body: "old body", Type: "team"}},
		Rules:    []Rule{{Slug: "execute-rule", Description: "execute hook", Body: "Do it.", Enabled: true, AppliesTo: []string{"execute"}}},
	})
	if err != nil {
		t.Fatalf("BootstrapWithPaths: %v", err)
	}
	if len(paths) != 2 || commit == "" {
		t.Fatalf("bootstrap paths=%v commit=%q", paths, commit)
	}
	entryPath, rulePath := paths[0], paths[1]
	co := cloneTeamRepo(t, host, "team-cas")
	entryUUID := mustEntryUUID(t, co, entryPath)
	ruleUUID := mustRuleUUID(t, co, rulePath)
	entryHash := gitBlobHash(t, co, entryPath)
	ruleHash := gitBlobHash(t, co, rulePath)

	upd, err := repo.Propose(ctx, "team-cas", teammemory.ProposeCommand{
		ActorRef:       "agent:a1",
		IdempotencyKey: "update-entry",
		Operation:      teammemory.OperationUpdate,
		TargetKind:     teammemory.TargetEntry,
		Target:         &teammemory.TargetRef{SourcePath: entryPath, UUID: entryUUID, ExpectedBlobHash: entryHash},
		Candidate:      &teammemory.Candidate{Description: "new lesson", Body: "new body", Type: "team"},
		Rationale:      "Improve the lesson.",
	})
	if err != nil {
		t.Fatalf("Propose update: %v", err)
	}
	if _, err := repo.Review(ctx, "team-cas", teammemory.ReviewCommand{
		ActorRef: "agent:curator", ProposalID: upd.ProposalID, Action: teammemory.ActionPromote,
		ExpectedRepoCommit: upd.RepoCommit, Comment: "CAS matches.",
	}); err != nil {
		t.Fatalf("Promote update: %v", err)
	}
	co = cloneTeamRepo(t, host, "team-cas")
	_, body, err := parseEntry(filepath.Join(co, filepath.FromSlash(entryPath)))
	if err != nil {
		t.Fatalf("parse updated entry: %v", err)
	}
	if body != "new body" || mustEntryUUID(t, co, entryPath) != entryUUID {
		t.Fatalf("update did not preserve identity/body: uuid=%s body=%q", mustEntryUUID(t, co, entryPath), body)
	}

	dis, err := repo.Propose(ctx, "team-cas", teammemory.ProposeCommand{
		ActorRef:       "agent:a1",
		IdempotencyKey: "disable-rule",
		Operation:      teammemory.OperationDisable,
		TargetKind:     teammemory.TargetRule,
		Target:         &teammemory.TargetRef{SourcePath: rulePath, UUID: ruleUUID, ExpectedBlobHash: ruleHash},
		Rationale:      "Rule is no longer correct.",
	})
	if err != nil {
		t.Fatalf("Propose disable: %v", err)
	}
	if _, err := repo.Review(ctx, "team-cas", teammemory.ReviewCommand{
		ActorRef: "agent:curator", ProposalID: dis.ProposalID, Action: teammemory.ActionPromote,
		ExpectedRepoCommit: dis.RepoCommit, Comment: "Disable instead of delete.",
	}); err != nil {
		t.Fatalf("Promote disable: %v", err)
	}
	snap, err := NewTeamMemoryConsumer(host, nil).ReadTeamRules(ctx, "team-cas", "execute")
	if err != nil {
		t.Fatalf("ReadTeamRules: %v", err)
	}
	if len(snap.Rules) != 0 {
		t.Fatalf("disabled rule still loaded: %+v", snap.Rules)
	}

	bad, err := repo.Propose(ctx, "team-cas", teammemory.ProposeCommand{
		ActorRef:       "agent:a1",
		IdempotencyKey: "delete-stale",
		Operation:      teammemory.OperationDelete,
		TargetKind:     teammemory.TargetEntry,
		Target:         &teammemory.TargetRef{SourcePath: entryPath, UUID: entryUUID, ExpectedBlobHash: entryHash},
		Rationale:      "Delete stale target.",
	})
	if err != nil {
		t.Fatalf("Propose stale delete: %v", err)
	}
	_, err = repo.Review(ctx, "team-cas", teammemory.ReviewCommand{
		ActorRef: "agent:curator", ProposalID: bad.ProposalID, Action: teammemory.ActionPromote,
		ExpectedRepoCommit: bad.RepoCommit, Comment: "Should fail CAS.",
	})
	if !errors.Is(err, teammemory.ErrTargetChanged) {
		t.Fatalf("stale delete err=%v want target_changed", err)
	}

	co = cloneTeamRepo(t, host, "team-cas")
	currentHash := gitBlobHash(t, co, entryPath)
	del, err := repo.Propose(ctx, "team-cas", teammemory.ProposeCommand{
		ActorRef:       "agent:a1",
		IdempotencyKey: "delete-current",
		Operation:      teammemory.OperationDelete,
		TargetKind:     teammemory.TargetEntry,
		Target:         &teammemory.TargetRef{SourcePath: entryPath, UUID: entryUUID, ExpectedBlobHash: currentHash},
		Rationale:      "Remove obsolete lesson.",
	})
	if err != nil {
		t.Fatalf("Propose delete: %v", err)
	}
	if _, err := repo.Review(ctx, "team-cas", teammemory.ReviewCommand{
		ActorRef: "agent:curator", ProposalID: del.ProposalID, Action: teammemory.ActionPromote,
		ExpectedRepoCommit: del.RepoCommit, Comment: "Explicit delete accepted.",
	}); err != nil {
		t.Fatalf("Promote delete: %v", err)
	}
	co = cloneTeamRepo(t, host, "team-cas")
	if _, err := os.Stat(filepath.Join(co, filepath.FromSlash(entryPath))); !os.IsNotExist(err) {
		t.Fatalf("entry file should be deleted, stat err=%v", err)
	}
}

func TestTeamMemoryRepository_SecurityAndIdempotencyGates(t *testing.T) {
	ctx := context.Background()
	host := NewHost(t.TempDir(), nil)
	repo := newTestTeamMemoryRepo(host, "p-warn", "p-conflict")

	warn, err := repo.Propose(ctx, "team-gates", teammemory.ProposeCommand{
		ActorRef:       "agent:a1",
		IdempotencyKey: "warn",
		Operation:      teammemory.OperationAdd,
		TargetKind:     teammemory.TargetRule,
		Candidate: &teammemory.Candidate{
			Slug: "external-ref", Description: "documents external reference", Body: "See https://example.test/ref",
			Enabled: boolPtr(true), AppliesTo: []string{"review"},
		},
		Rationale: "External reference needs human acknowledgement.",
	})
	if err != nil {
		t.Fatalf("Propose warning: %v", err)
	}
	if len(warn.Warnings) != 1 || warn.Warnings[0] != "contains_external_reference" {
		t.Fatalf("warnings=%v", warn.Warnings)
	}
	_, err = repo.Review(ctx, "team-gates", teammemory.ReviewCommand{
		ActorRef: "agent:curator", ProposalID: warn.ProposalID, Action: teammemory.ActionPromote,
		ExpectedRepoCommit: warn.RepoCommit, Comment: "No warning ack.",
	})
	if !errors.Is(err, teammemory.ErrWarningUnacknowledged) {
		t.Fatalf("unacknowledged warning err=%v want warning_unacknowledged", err)
	}
	if _, err := repo.Review(ctx, "team-gates", teammemory.ReviewCommand{
		ActorRef: "agent:curator", ProposalID: warn.ProposalID, Action: teammemory.ActionPromote,
		ExpectedRepoCommit: warn.RepoCommit, Comment: "Acknowledged external reference.",
		AcknowledgeWarnings: []string{"contains_external_reference"},
	}); err != nil {
		t.Fatalf("acknowledged warning promote: %v", err)
	}

	base := teammemory.ProposeCommand{
		ActorRef:       "agent:a1",
		IdempotencyKey: "conflict",
		Operation:      teammemory.OperationAdd,
		TargetKind:     teammemory.TargetEntry,
		Candidate:      &teammemory.Candidate{Slug: "first", Description: "first", Body: "first"},
		Rationale:      "First payload.",
	}
	if _, err := repo.Propose(ctx, "team-idempotency", base); err != nil {
		t.Fatalf("Propose base: %v", err)
	}
	changed := base
	changed.Candidate = &teammemory.Candidate{Slug: "second", Description: "second", Body: "second"}
	if _, err := repo.Propose(ctx, "team-idempotency", changed); !errors.Is(err, teammemory.ErrIdempotencyConflict) {
		t.Fatalf("idempotency conflict err=%v want idempotency_conflict", err)
	}

	secret := base
	secret.IdempotencyKey = "secret"
	secret.Candidate = &teammemory.Candidate{Slug: "secret", Description: "leaks token", Body: "acat_should_not_land"}
	if _, err := repo.Propose(ctx, "team-idempotency", secret); !errors.Is(err, teammemory.ErrSecretDetected) {
		t.Fatalf("secret err=%v want secret_detected", err)
	}

	traversal := base
	traversal.IdempotencyKey = "path"
	traversal.Operation = teammemory.OperationUpdate
	traversal.Target = &teammemory.TargetRef{SourcePath: "../rules/x.md", UUID: "x", ExpectedBlobHash: "abc"}
	traversal.Candidate = &teammemory.Candidate{Description: "bad path", Body: "bad"}
	if _, err := repo.Propose(ctx, "team-idempotency", traversal); !errors.Is(err, teammemory.ErrInvalidCandidate) {
		t.Fatalf("path traversal err=%v want invalid_candidate", err)
	}
}

func TestTeamMemoryRepository_ConcurrentProposalsBothSurvive(t *testing.T) {
	ctx := context.Background()
	host := NewHost(t.TempDir(), nil)
	repos := []*TeamMemoryRepository{
		newTestTeamMemoryRepo(host, "p-one"),
		newTestTeamMemoryRepo(host, "p-two"),
	}
	var wg sync.WaitGroup
	errs := make(chan error, 2)
	for i, repo := range repos {
		i, repo := i, repo
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, err := repo.Propose(ctx, "team-race", teammemory.ProposeCommand{
				ActorRef:       "agent:a1",
				IdempotencyKey: []string{"race-one", "race-two"}[i],
				Operation:      teammemory.OperationAdd,
				TargetKind:     teammemory.TargetEntry,
				Candidate:      &teammemory.Candidate{Slug: []string{"one", "two"}[i], Description: "lesson", Body: "body"},
				Rationale:      "Concurrent proposal.",
			})
			errs <- err
		}()
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		if err != nil {
			t.Fatalf("concurrent propose failed: %v", err)
		}
	}
	list, err := repos[0].List(ctx, "team-race", teammemory.Filter{})
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if list.Total != 2 {
		t.Fatalf("pending proposals total=%d want 2: %+v", list.Total, list.Proposals)
	}
}

func TestTeamMemoryRepository_SameProposalConcurrentPromotionSingleWinner(t *testing.T) {
	ctx := context.Background()
	host := NewHost(t.TempDir(), nil)
	repo := newTestTeamMemoryRepo(host, "p-promote")
	prop, err := repo.Propose(ctx, "team-promote-race", teammemory.ProposeCommand{
		ActorRef:       "agent:a1",
		IdempotencyKey: "promote-race",
		Operation:      teammemory.OperationAdd,
		TargetKind:     teammemory.TargetRule,
		Candidate:      &teammemory.Candidate{Slug: "winner", Description: "single winner", Body: "Only one.", Enabled: boolPtr(true), AppliesTo: []string{"execute"}},
		Rationale:      "Race test.",
	})
	if err != nil {
		t.Fatalf("Propose: %v", err)
	}
	start := make(chan struct{})
	errs := make(chan error, 2)
	for i := 0; i < 2; i++ {
		wgRepo := NewTeamMemoryRepository(host, nil, WithRepositoryClock(fixedRepoClock()))
		go func() {
			<-start
			_, err := wgRepo.Review(ctx, "team-promote-race", teammemory.ReviewCommand{
				ActorRef: "agent:curator", ProposalID: prop.ProposalID, Action: teammemory.ActionPromote,
				ExpectedRepoCommit: prop.RepoCommit, Comment: "Approve once.",
			})
			errs <- err
		}()
	}
	close(start)
	var successes, failures int
	for i := 0; i < 2; i++ {
		err := <-errs
		if err == nil {
			successes++
			continue
		}
		if errors.Is(err, teammemory.ErrTeamMemoryVersionConflict) || errors.Is(err, teammemory.ErrProposalNotPending) {
			failures++
			continue
		}
		t.Fatalf("unexpected promotion race err=%v", err)
	}
	if successes != 1 || failures != 1 {
		t.Fatalf("successes=%d failures=%d, want 1/1", successes, failures)
	}
	snap, err := NewTeamMemoryConsumer(host, nil).ReadTeamRules(ctx, "team-promote-race", "execute")
	if err != nil {
		t.Fatalf("ReadTeamRules: %v", err)
	}
	if len(snap.Rules) != 1 {
		t.Fatalf("rules=%+v want single promoted rule", snap.Rules)
	}
}

func cloneTeamRepo(t *testing.T, host *Host, teamID string) string {
	t.Helper()
	bareDir, err := host.RepoDir(TeamRepo(teamID))
	if err != nil {
		t.Fatalf("RepoDir: %v", err)
	}
	work := t.TempDir()
	runGit(t, t.TempDir(), work, "clone", bareDir, "co")
	return filepath.Join(work, "co")
}

func gitBlobHash(t *testing.T, repoDir, rel string) string {
	t.Helper()
	return strings.TrimSpace(runGit(t, t.TempDir(), repoDir, "hash-object", "--", rel))
}

func mustEntryUUID(t *testing.T, repoDir, rel string) string {
	t.Helper()
	fm, _, err := parseEntry(filepath.Join(repoDir, filepath.FromSlash(rel)))
	if err != nil {
		t.Fatalf("parseEntry %s: %v", rel, err)
	}
	return fm.UUID
}

func mustRuleUUID(t *testing.T, repoDir, rel string) string {
	t.Helper()
	fm, _, err := parseRule(filepath.Join(repoDir, filepath.FromSlash(rel)))
	if err != nil {
		t.Fatalf("parseRule %s: %v", rel, err)
	}
	return fm.UUID
}

func boolPtr(v bool) *bool { return &v }
