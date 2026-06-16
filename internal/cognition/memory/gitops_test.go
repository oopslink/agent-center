package memory_test

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/oopslink/agent-center/internal/cognition/memory"
)

// fakeRunner records calls and returns scripted results.
type fakeRunner struct {
	mu     sync.Mutex
	calls  []fakeCall
	script []fakeResult
}

type fakeCall struct {
	args    []string
	workdir string
	env     []string
}

type fakeResult struct {
	out string
	err error
}

func (f *fakeRunner) Run(ctx context.Context, workdir string, env []string, args ...string) (string, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	cp := append([]string(nil), args...)
	cpenv := append([]string(nil), env...)
	f.calls = append(f.calls, fakeCall{args: cp, workdir: workdir, env: cpenv})
	if len(f.script) == 0 {
		return "", nil
	}
	r := f.script[0]
	f.script = f.script[1:]
	return r.out, r.err
}

func TestGitOps_EmptyDir(t *testing.T) {
	g := memory.NewGitOps("", &fakeRunner{}, "")
	ctx := context.Background()
	if _, err := g.IsGitRepo(ctx); !errors.Is(err, memory.ErrMemoryDirEmpty) {
		t.Fatalf("expected ErrMemoryDirEmpty, got %v", err)
	}
	if err := g.Init(ctx); !errors.Is(err, memory.ErrMemoryDirEmpty) {
		t.Fatalf("init: %v", err)
	}
	if err := g.CommitFile(ctx, "x", "a", "b", "m"); !errors.Is(err, memory.ErrMemoryDirEmpty) {
		t.Fatalf("commit: %v", err)
	}
	if err := g.AutoCommitDirty(ctx, "a", "b", "m"); !errors.Is(err, memory.ErrMemoryDirEmpty) {
		t.Fatalf("auto: %v", err)
	}
	if _, err := g.LogOneline(ctx); !errors.Is(err, memory.ErrMemoryDirEmpty) {
		t.Fatalf("log: %v", err)
	}
}

func TestGitOps_CommitNeedsAuthor(t *testing.T) {
	r := &fakeRunner{}
	g := memory.NewGitOps(t.TempDir(), r, "")
	if err := g.CommitFile(context.Background(), "x", "", "", "m"); err == nil {
		t.Fatal("expected author err")
	}
	if err := g.AutoCommitDirty(context.Background(), "", "", "m"); err == nil {
		t.Fatal("expected author err")
	}
}

func TestGitOps_FakeRunner_AuthorEnvInjected(t *testing.T) {
	dir := t.TempDir()
	r := &fakeRunner{}
	// scripts for: add (ok), diff --cached --quiet (nonzero = needs commit),
	// commit (ok)
	r.script = []fakeResult{
		{out: ""},                       // add
		{out: "", err: errExit("diff")}, // diff -> exit 1, needs commit
		{out: ""},                       // commit
	}
	g := memory.NewGitOps(dir, r, "/tmp/fakehome")
	if err := g.CommitFile(context.Background(), "CLAUDE.md", "supervisor", "supervisor@x.local", "init"); err != nil {
		t.Fatalf("commit: %v", err)
	}
	if len(r.calls) != 3 {
		t.Fatalf("expected 3 calls, got %d", len(r.calls))
	}
	for _, c := range r.calls {
		envMap := envSlice(c.env)
		if envMap["GIT_AUTHOR_NAME"] != "supervisor" {
			t.Errorf("author name = %q", envMap["GIT_AUTHOR_NAME"])
		}
		if envMap["GIT_AUTHOR_EMAIL"] != "supervisor@x.local" {
			t.Errorf("author email = %q", envMap["GIT_AUTHOR_EMAIL"])
		}
		if envMap["GIT_CONFIG_GLOBAL"] != "/dev/null" {
			t.Errorf("global config not muted")
		}
		if envMap["HOME"] != "/tmp/fakehome" {
			t.Errorf("HOME = %q", envMap["HOME"])
		}
		// gpgsign disabled via -c
		if len(c.args) < 2 || c.args[0] != "-c" || c.args[1] != "commit.gpgsign=false" {
			t.Errorf("missing -c commit.gpgsign=false in args %v", c.args)
		}
	}
}

func TestGitOps_FakeRunner_CleanCommitSkipped(t *testing.T) {
	dir := t.TempDir()
	r := &fakeRunner{
		script: []fakeResult{
			{out: ""}, // add
			{out: ""}, // diff --cached --quiet → exit 0 means nothing to commit
		},
	}
	g := memory.NewGitOps(dir, r, "")
	if err := g.CommitFile(context.Background(), "CLAUDE.md", "a", "a@x", "m"); err != nil {
		t.Fatal(err)
	}
	if len(r.calls) != 2 {
		t.Fatalf("expected 2 calls, got %d", len(r.calls))
	}
}

func TestGitOps_AutoCommit_Clean(t *testing.T) {
	r := &fakeRunner{
		script: []fakeResult{
			{out: ""}, // status --porcelain (clean)
		},
	}
	g := memory.NewGitOps(t.TempDir(), r, "")
	if err := g.AutoCommitDirty(context.Background(), "a", "a@x", "m"); err != nil {
		t.Fatal(err)
	}
	if len(r.calls) != 1 {
		t.Errorf("expected 1 status call, got %d", len(r.calls))
	}
}

func TestGitOps_AutoCommit_Dirty(t *testing.T) {
	r := &fakeRunner{
		script: []fakeResult{
			{out: "M CLAUDE.md\n"}, // status dirty
			{out: ""},              // add -A
			{out: ""},              // commit
		},
	}
	g := memory.NewGitOps(t.TempDir(), r, "")
	if err := g.AutoCommitDirty(context.Background(), "a", "a@x", "m"); err != nil {
		t.Fatal(err)
	}
	if len(r.calls) != 3 {
		t.Errorf("expected 3 calls, got %d", len(r.calls))
	}
}

func TestGitOps_RealGitInit(t *testing.T) {
	dir := t.TempDir()
	home := t.TempDir()
	g := memory.NewGitOps(dir, memory.NewExecGitRunner(), home)
	ctx := context.Background()
	ok, err := g.IsGitRepo(ctx)
	if err != nil {
		t.Fatalf("isrepo: %v", err)
	}
	if ok {
		t.Fatal("fresh dir reports as repo")
	}
	if err := g.Init(ctx); err != nil {
		t.Fatalf("init: %v", err)
	}
	ok, err = g.IsGitRepo(ctx)
	if err != nil || !ok {
		t.Fatalf("after init isrepo: %v %v", ok, err)
	}
	// write a file + commit it
	if err := os.WriteFile(filepath.Join(dir, "CLAUDE.md"), []byte("# x\n"), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}
	if err := g.CommitFile(ctx, "CLAUDE.md", "supervisor", "supervisor@local", "init"); err != nil {
		t.Fatalf("commit: %v", err)
	}
	// commit again should be idempotent (no changes)
	if err := g.CommitFile(ctx, "CLAUDE.md", "supervisor", "supervisor@local", "init"); err != nil {
		t.Fatalf("commit idempotent: %v", err)
	}
	// modify + auto-commit
	if err := os.WriteFile(filepath.Join(dir, "CLAUDE.md"), []byte("# x\n## more\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := g.AutoCommitDirty(ctx, "supervisor:1", "supervisor:1@local", "update"); err != nil {
		t.Fatalf("auto: %v", err)
	}
	log, err := g.LogOneline(ctx)
	if err != nil {
		t.Fatalf("log: %v", err)
	}
	if !strings.Contains(log, "init") || !strings.Contains(log, "update") {
		t.Errorf("log missing commits: %s", log)
	}
}

func TestGitOps_FakeRunner_AddFails(t *testing.T) {
	r := &fakeRunner{
		script: []fakeResult{
			{out: "boom", err: errExit("add fail")},
		},
	}
	g := memory.NewGitOps(t.TempDir(), r, "")
	err := g.CommitFile(context.Background(), "x", "a", "a@x", "m")
	if !errors.Is(err, memory.ErrMemoryGitOpFailed) {
		t.Fatalf("expected ErrMemoryGitOpFailed, got %v", err)
	}
}

// errExit emulates exec.ExitError-like value for fake runs.
type fakeExit struct{ msg string }

func (e *fakeExit) Error() string { return e.msg }

func errExit(s string) error { return &fakeExit{msg: s} }

func envSlice(env []string) map[string]string {
	out := map[string]string{}
	for _, kv := range env {
		k, v, _ := strings.Cut(kv, "=")
		out[k] = v
	}
	return out
}
