package mergecheck

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// fakeRunner records the git invocations and returns canned outputs/errors keyed
// by the git subcommand (args[0]). It never hits the network or a real repo.
type fakeRunner struct {
	calls   [][]string
	out     map[string]string
	errs    map[string]error
	missing map[string]bool // refs that rev-parse --verify should fail on (the last arg)
}

func newFakeRunner() *fakeRunner {
	return &fakeRunner{out: map[string]string{}, errs: map[string]error{}, missing: map[string]bool{}}
}

func (f *fakeRunner) Run(_ context.Context, _ string, _ []string, args ...string) (string, error) {
	f.calls = append(f.calls, args)
	sub := ""
	if len(args) > 0 {
		sub = args[0]
	}
	// rev-parse --verify --quiet <ref>: fail when the ref is registered missing.
	if sub == "rev-parse" {
		ref := args[len(args)-1]
		if f.missing[ref] {
			return "", &exec.ExitError{} // git rev-parse --verify exits non-zero on missing ref
		}
		return "deadbeef\n", nil
	}
	return f.out[sub], f.errs[sub]
}

func (f *fakeRunner) sawSub(sub string) bool {
	for _, c := range f.calls {
		if len(c) > 0 && c[0] == sub {
			return true
		}
	}
	return false
}

// exitErr returns an error that errors.As-matches *exec.ExitError with the given
// code (via a real `false`/`true` shell-out is overkill — we fabricate one by
// running a process that exits with the code).
func exitErr(t *testing.T, code int) error {
	t.Helper()
	var cmd *exec.Cmd
	if code == 0 {
		cmd = exec.Command("true")
	} else {
		// `sh -c 'exit N'` yields an *exec.ExitError with ExitCode()==N.
		cmd = exec.Command("sh", "-c", "exit "+itoa(code))
	}
	err := cmd.Run()
	if code == 0 {
		if err != nil {
			t.Fatalf("setup exit0: %v", err)
		}
		return nil
	}
	if _, ok := err.(*exec.ExitError); !ok {
		t.Fatalf("setup exit%d: got %T %v", code, err, err)
	}
	return err
}

func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	neg := n < 0
	if neg {
		n = -n
	}
	var b []byte
	for n > 0 {
		b = append([]byte{byte('0' + n%10)}, b...)
		n /= 10
	}
	if neg {
		b = append([]byte{'-'}, b...)
	}
	return string(b)
}

// On a fresh cache dir the checker must CLONE --mirror (no mirror exists yet), then
// resolve refs and parse is-ancestor exit 0 ⇒ merged==true.
func TestBranchMergedToOrigin_ClonesThenMergedTrue(t *testing.T) {
	fr := newFakeRunner()
	fr.errs["merge-base"] = nil // exit 0 ⇒ ancestor ⇒ merged
	c := New(t.TempDir(), fr)

	merged, err := c.BranchMergedToOrigin(context.Background(), "https://example.com/repo.git", "T7", "dev/v2.13.0")
	if err != nil {
		t.Fatalf("unexpected err: %v", err)
	}
	if !merged {
		t.Fatalf("merged = false, want true (is-ancestor exit 0)")
	}
	if !fr.sawSub("clone") {
		t.Errorf("expected a clone --mirror on a fresh cache, calls=%v", fr.calls)
	}
	// clone --mirror must carry the mirror flag + URL.
	var cloneArgs []string
	for _, ca := range fr.calls {
		if ca[0] == "clone" {
			cloneArgs = ca
		}
	}
	if len(cloneArgs) < 3 || cloneArgs[1] != "--mirror" {
		t.Errorf("clone args = %v, want [clone --mirror <url> <dir>]", cloneArgs)
	}
}

// is-ancestor exit 1 ⇒ merged==false with NO error (definitive "not merged").
func TestBranchMergedToOrigin_NotMerged(t *testing.T) {
	fr := newFakeRunner()
	fr.errs["merge-base"] = exitErr(t, 1)
	c := New(t.TempDir(), fr)

	merged, err := c.BranchMergedToOrigin(context.Background(), "u", "b", "base")
	if err != nil {
		t.Fatalf("exit-1 should be (false,nil), got err=%v", err)
	}
	if merged {
		t.Fatalf("merged = true, want false (is-ancestor exit 1)")
	}
}

// A missing branch ref ⇒ error (the branch doesn't exist on origin).
func TestBranchMergedToOrigin_MissingBranchRef(t *testing.T) {
	fr := newFakeRunner()
	fr.missing["refs/heads/ghost"] = true
	c := New(t.TempDir(), fr)

	_, err := c.BranchMergedToOrigin(context.Background(), "u", "ghost", "base")
	if err == nil || !strings.Contains(err.Error(), "not found on origin") {
		t.Fatalf("missing branch should error with not-found, got %v", err)
	}
}

// is-ancestor exit 2 (or any non-0/1) ⇒ surfaced as an error (couldn't determine).
func TestBranchMergedToOrigin_AncestryError(t *testing.T) {
	fr := newFakeRunner()
	fr.errs["merge-base"] = exitErr(t, 2)
	c := New(t.TempDir(), fr)

	_, err := c.BranchMergedToOrigin(context.Background(), "u", "b", "base")
	if err == nil || !strings.Contains(err.Error(), "is-ancestor") {
		t.Fatalf("exit-2 should surface an is-ancestor error, got %v", err)
	}
}

// On an existing mirror the checker must FETCH (not clone) before answering.
func TestBranchMergedToOrigin_ExistingMirrorFetches(t *testing.T) {
	dir := t.TempDir()
	fr := newFakeRunner()
	fr.errs["merge-base"] = nil
	c := New(dir, fr)

	// Pre-create the mirror subdir so syncMirror takes the fetch path.
	if err := os.MkdirAll(filepath.Join(dir, mirrorSubdir("u")), 0o755); err != nil {
		t.Fatal(err)
	}
	if _, err := c.BranchMergedToOrigin(context.Background(), "u", "b", "base"); err != nil {
		t.Fatalf("unexpected err: %v", err)
	}
	if fr.sawSub("clone") {
		t.Errorf("should NOT clone when a mirror already exists; calls=%v", fr.calls)
	}
	if !fr.sawSub("fetch") {
		t.Errorf("expected a fetch on an existing mirror; calls=%v", fr.calls)
	}
}
