package workerdaemon

import (
	"bytes"
	"context"
	"encoding/base64"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/oopslink/agent-center/internal/runtimefs"
)

// These tests exercise the WORKER-side runtime-fs ops directly (the pure list/read/
// gitlog functions) — the security red lines (path escape, credential redaction, .git
// hidden, special-file metadata-only) and the limits, which the Accept stage verifies.

func writeFile(t *testing.T, p, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

func entryByName(es []runtimefs.Entry, name string) (runtimefs.Entry, bool) {
	for _, e := range es {
		if e.Name == name {
			return e, true
		}
	}
	return runtimefs.Entry{}, false
}

func TestRuntimeFsList_HidesGitAndFlagsSensitive(t *testing.T) {
	home := t.TempDir()
	writeFile(t, filepath.Join(home, "events.jsonl"), "{}")
	writeFile(t, filepath.Join(home, "mcp_config.runtime.json"), `{"token":"SECRET"}`)
	writeFile(t, filepath.Join(home, "supervisor.lock"), "")
	writeFile(t, filepath.Join(home, "memory", "CLAUDE.md"), "# notes")
	if err := os.MkdirAll(filepath.Join(home, "memory", ".git"), 0o755); err != nil {
		t.Fatal(err)
	}

	// Root listing: sensitive flags on creds + lock; ordinary file unflagged.
	root, opErr := runtimeFsList(home, "")
	if opErr != nil {
		t.Fatalf("list root: %v", opErr)
	}
	if root.Type != "directory" {
		t.Fatalf("root type=%q want directory", root.Type)
	}
	if e, ok := entryByName(root.Entries, "mcp_config.runtime.json"); !ok || !e.Sensitive {
		t.Fatalf("runtime.json sensitive=%v ok=%v, want sensitive", e.Sensitive, ok)
	}
	if e, ok := entryByName(root.Entries, "supervisor.lock"); !ok || !e.Sensitive {
		t.Fatalf("supervisor.lock sensitive=%v ok=%v, want sensitive", e.Sensitive, ok)
	}
	if e, ok := entryByName(root.Entries, "events.jsonl"); !ok || e.Sensitive {
		t.Fatalf("events.jsonl sensitive=%v ok=%v, want not-sensitive", e.Sensitive, ok)
	}
	if e, ok := entryByName(root.Entries, "memory"); !ok || e.Type != "directory" {
		t.Fatalf("memory entry type=%q ok=%v, want directory", e.Type, ok)
	}

	// memory listing must NEVER include .git (red line: hidden).
	mem, opErr := runtimeFsList(home, "memory")
	if opErr != nil {
		t.Fatalf("list memory: %v", opErr)
	}
	if _, ok := entryByName(mem.Entries, ".git"); ok {
		t.Fatal(".git must never be listed")
	}
	if _, ok := entryByName(mem.Entries, "CLAUDE.md"); !ok {
		t.Fatal("memory/CLAUDE.md should be listed")
	}
	// entry paths are home-relative, forward-slashed.
	if e, _ := entryByName(mem.Entries, "CLAUDE.md"); e.Path != "memory/CLAUDE.md" {
		t.Fatalf("entry path=%q want memory/CLAUDE.md", e.Path)
	}
}

func TestRuntimeFsList_RejectsGitDirTarget(t *testing.T) {
	home := t.TempDir()
	writeFile(t, filepath.Join(home, "memory", ".git", "config"), "[core]")
	writeFile(t, filepath.Join(home, "memory", ".git", "refs", "heads", "main"), "deadbeef")

	// Listing the .git dir directly must be refused (not_found — hidden), even though
	// it resolves cleanly inside the home.
	for _, p := range []string{"memory/.git", "memory/.git/refs", "memory/.git/refs/heads"} {
		if _, opErr := runtimeFsList(home, p); opErr == nil || opErr.code != runtimefs.ErrCodeNotFound {
			gotCode := ""
			if opErr != nil {
				gotCode = opErr.code
			}
			t.Fatalf("list %q code=%q, want not_found (.git hidden)", p, gotCode)
		}
	}
}

func TestRuntimeFsRead_SymlinkIntoGitRejected(t *testing.T) {
	home := t.TempDir()
	writeFile(t, filepath.Join(home, "memory", ".git", "config"), "url = secret")

	// A direct read into .git is hidden.
	if _, opErr := runtimeFsRead(home, "memory/.git/config"); opErr == nil || opErr.code != runtimefs.ErrCodeNotFound {
		gotCode := ""
		if opErr != nil {
			gotCode = opErr.code
		}
		t.Fatalf("read memory/.git/config code=%q, want not_found", gotCode)
	}

	// A symlink that stays inside the home but dereferences INTO .git must also be
	// caught by the POST-resolution guard (the pre-resolution name check would miss it).
	link := filepath.Join(home, "sneaky")
	if err := os.Symlink(filepath.Join(home, "memory", ".git", "config"), link); err != nil {
		t.Skipf("symlink unsupported: %v", err)
	}
	if _, opErr := runtimeFsRead(home, "sneaky"); opErr == nil || opErr.code != runtimefs.ErrCodeNotFound {
		gotCode := ""
		if opErr != nil {
			gotCode = opErr.code
		}
		t.Fatalf("read symlink-into-.git code=%q, want not_found (resolved path hidden)", gotCode)
	}
}

func TestRuntimeFsRead_RedactsCredentials(t *testing.T) {
	home := t.TempDir()
	writeFile(t, filepath.Join(home, "mcp_config.runtime.json"), `{"token":"PLAINTEXT-SECRET"}`)

	res, opErr := runtimeFsRead(home, "mcp_config.runtime.json")
	if opErr != nil {
		t.Fatalf("read runtime.json: %v", opErr)
	}
	if !res.Redacted {
		t.Fatal("credential file must be redacted=true")
	}
	if res.Content != nil {
		t.Fatalf("credential content must be withheld (nil), got %q", *res.Content)
	}
	if res.Size == 0 {
		t.Fatal("metadata (size) should still be present for a redacted file")
	}
}

func TestRuntimeFsRead_HardlinkCredentialStillRedacted(t *testing.T) {
	home := t.TempDir()
	cred := filepath.Join(home, "mcp_config.runtime.json")
	writeFile(t, cred, `{"token":"PLAINTEXT-SECRET"}`)

	// A hardlink gives the credential file an innocuous name. EvalSymlinks does NOT
	// normalise a hardlink (same inode, not a symlink), so the name check alone would
	// leak it — the inode-identity (os.SameFile) check must still redact.
	alias := filepath.Join(home, "notes.txt")
	if err := os.Link(cred, alias); err != nil {
		t.Skipf("hardlink unsupported: %v", err)
	}
	res, opErr := runtimeFsRead(home, "notes.txt")
	if opErr != nil {
		t.Fatalf("read hardlink alias: %v", opErr)
	}
	if !res.Redacted || res.Content != nil {
		t.Fatalf("hardlink alias of the credential file must be redacted (redacted=%v content-present=%v) — plaintext must never leak", res.Redacted, res.Content != nil)
	}
}

func TestRuntimeFsRead_SpecialAndBinaryAreMetadataOnly(t *testing.T) {
	home := t.TempDir()
	writeFile(t, filepath.Join(home, "supervisor.lock"), "pid 123")
	writeFile(t, filepath.Join(home, "blob.bin"), "abc\x00\x01\x02def")
	writeFile(t, filepath.Join(home, "events.jsonl"), `{"type":"x"}`)

	lock, opErr := runtimeFsRead(home, "supervisor.lock")
	if opErr != nil || !lock.Binary || lock.Content != nil {
		t.Fatalf("lock read = (binary=%v content!=nil:%v err=%v), want binary + content nil", lock.Binary, lock.Content != nil, opErr)
	}
	bin, opErr := runtimeFsRead(home, "blob.bin")
	if opErr != nil || !bin.Binary || bin.Content != nil {
		t.Fatalf("binary read = (binary=%v content!=nil:%v err=%v), want binary + content nil", bin.Binary, bin.Content != nil, opErr)
	}
	txt, opErr := runtimeFsRead(home, "events.jsonl")
	if opErr != nil || txt.Binary || txt.Content == nil || *txt.Content != `{"type":"x"}` {
		t.Fatalf("text read = (binary=%v content=%v err=%v), want text content", txt.Binary, txt.Content, opErr)
	}
}

func TestRuntimeFsRead_TruncatesLargeFile(t *testing.T) {
	home := t.TempDir()
	big := strings.Repeat("a", runtimeFsMaxFileSize+4096)
	writeFile(t, filepath.Join(home, "big.txt"), big)
	res, opErr := runtimeFsRead(home, "big.txt")
	if opErr != nil {
		t.Fatalf("read big: %v", opErr)
	}
	if !res.Truncated {
		t.Fatal("a >1MB file must be truncated")
	}
	if res.Content == nil || len(*res.Content) != runtimeFsMaxFileSize {
		t.Fatalf("truncated content len=%v want %d", contentLen(res.Content), runtimeFsMaxFileSize)
	}
}

func contentLen(c *string) int {
	if c == nil {
		return -1
	}
	return len(*c)
}

func TestRuntimeFsRead_PathEscapeRejected(t *testing.T) {
	home := t.TempDir()
	// A secret OUTSIDE the home.
	outside := filepath.Join(filepath.Dir(home), "outside-secret.txt")
	writeFile(t, outside, "TOP SECRET")
	t.Cleanup(func() { _ = os.Remove(outside) })

	for _, bad := range []string{"../outside-secret.txt", "../../etc/passwd", outside} {
		if _, opErr := runtimeFsRead(home, bad); opErr == nil || opErr.code != runtimefs.ErrCodePathEscape {
			gotCode := ""
			if opErr != nil {
				gotCode = opErr.code
			}
			// An absolute path to a nonexistent file may surface not_found; the key
			// invariant is it is NEVER served. Accept path_escape OR not_found, but
			// never a successful read.
			if gotCode != runtimefs.ErrCodeNotFound {
				t.Fatalf("read %q code=%q, want path_escape (or not_found) — never served", bad, gotCode)
			}
		}
	}
}

func TestRuntimeFsRead_SymlinkEscapeRejected(t *testing.T) {
	home := t.TempDir()
	outside := filepath.Join(filepath.Dir(home), "outside-link-target.txt")
	writeFile(t, outside, "TOP SECRET")
	t.Cleanup(func() { _ = os.Remove(outside) })
	link := filepath.Join(home, "escape")
	if err := os.Symlink(outside, link); err != nil {
		t.Skipf("symlink unsupported: %v", err)
	}
	if _, opErr := runtimeFsRead(home, "escape"); opErr == nil || opErr.code != runtimefs.ErrCodePathEscape {
		gotCode := ""
		if opErr != nil {
			gotCode = opErr.code
		}
		t.Fatalf("read symlink-escape code=%q, want path_escape (symlink dereferenced + rejected)", gotCode)
	}
}

func TestRuntimeFsList_TruncatesEntries(t *testing.T) {
	home := t.TempDir()
	sub := filepath.Join(home, "many")
	for i := 0; i < runtimeFsMaxEntries+50; i++ {
		writeFile(t, filepath.Join(sub, "f"+pad(i)), "x")
	}
	res, opErr := runtimeFsList(home, "many")
	if opErr != nil {
		t.Fatalf("list many: %v", opErr)
	}
	if !res.Truncated {
		t.Fatal("listing >1000 entries must be truncated")
	}
	if len(res.Entries) != runtimeFsMaxEntries {
		t.Fatalf("entries=%d want %d", len(res.Entries), runtimeFsMaxEntries)
	}
}

func pad(i int) string {
	s := ""
	for _, d := range []int{1000, 100, 10, 1} {
		s += string(rune('0' + (i/d)%10))
	}
	return s
}

func TestRuntimeFsGitLog(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not available")
	}
	home := t.TempDir()
	mem := filepath.Join(home, "memory")
	if err := os.MkdirAll(mem, 0o755); err != nil {
		t.Fatal(err)
	}
	gitInit(t, mem)
	gitCommit(t, mem, "first commit")
	gitCommit(t, mem, "second commit")

	res, opErr := runtimeFsGitLog(context.Background(), home, "memory", 10)
	if opErr != nil {
		t.Fatalf("gitlog: %v", opErr)
	}
	if len(res.Commits) != 2 {
		t.Fatalf("commits=%d want 2", len(res.Commits))
	}
	// Newest first.
	if res.Commits[0].Message != "second commit" || res.Commits[1].Message != "first commit" {
		t.Fatalf("messages=%v want [second, first]", []string{res.Commits[0].Message, res.Commits[1].Message})
	}
	if res.Commits[0].SHA == "" || res.Commits[0].Author == "" || res.Commits[0].Date == "" {
		t.Fatalf("commit fields incomplete: %+v", res.Commits[0])
	}

	// limit truncation.
	lim, opErr := runtimeFsGitLog(context.Background(), home, "memory", 1)
	if opErr != nil {
		t.Fatalf("gitlog limit: %v", opErr)
	}
	if len(lim.Commits) != 1 || !lim.Truncated {
		t.Fatalf("limit=1 → commits=%d truncated=%v, want 1/true", len(lim.Commits), lim.Truncated)
	}

	// Non-repo dir → empty history, not an error.
	writeFile(t, filepath.Join(home, "workspace", "x.txt"), "y")
	non, opErr := runtimeFsGitLog(context.Background(), home, "workspace", 10)
	if opErr != nil {
		t.Fatalf("gitlog non-repo: %v", opErr)
	}
	if len(non.Commits) != 0 {
		t.Fatalf("non-repo commits=%d want 0", len(non.Commits))
	}
}

// A minimal 1x1 transparent PNG (valid header so http.DetectContentType → image/png).
var onePxPNG = []byte{
	0x89, 0x50, 0x4e, 0x47, 0x0d, 0x0a, 0x1a, 0x0a, 0x00, 0x00, 0x00, 0x0d, 0x49, 0x48, 0x44, 0x52,
	0x00, 0x00, 0x00, 0x01, 0x00, 0x00, 0x00, 0x01, 0x08, 0x06, 0x00, 0x00, 0x00, 0x1f, 0x15, 0xc4,
	0x89, 0x00, 0x00, 0x00, 0x0a, 0x49, 0x44, 0x41, 0x54, 0x78, 0x9c, 0x63, 0x00, 0x01, 0x00, 0x00,
	0x05, 0x00, 0x01, 0x0d, 0x0a, 0x2d, 0xb4, 0x00, 0x00, 0x00, 0x00, 0x49, 0x45, 0x4e, 0x44, 0xae,
	0x42, 0x60, 0x82,
}

func writeBytes(t *testing.T, p string, b []byte) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p, b, 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestRuntimeFsRead_ImageInlinedAsBase64(t *testing.T) {
	home := t.TempDir()
	writeBytes(t, filepath.Join(home, "workspace", "pic.png"), onePxPNG)

	res, opErr := runtimeFsRead(home, "workspace/pic.png")
	if opErr != nil {
		t.Fatalf("read image: %v", opErr)
	}
	if !res.Image || res.Binary {
		t.Fatalf("image flags: image=%v binary=%v, want image=true binary=false", res.Image, res.Binary)
	}
	if res.Encoding != "base64" || res.Content == nil {
		t.Fatalf("encoding=%q content==nil:%v, want base64 + content", res.Encoding, res.Content == nil)
	}
	if res.ContentType != "image/png" {
		t.Fatalf("content_type=%q want image/png", res.ContentType)
	}
	dec, err := base64.StdEncoding.DecodeString(*res.Content)
	if err != nil {
		t.Fatalf("content is not valid base64: %v", err)
	}
	if !bytes.Equal(dec, onePxPNG) {
		t.Fatalf("decoded image bytes differ from source (%d vs %d)", len(dec), len(onePxPNG))
	}
}

func TestRuntimeFsRead_OversizedImageIsMetadataOnly(t *testing.T) {
	home := t.TempDir()
	// PNG header + padding past the image cap → not inlined (binary, content null).
	big := append(append([]byte{}, onePxPNG...), make([]byte, runtimeFsMaxImageSize+1)...)
	writeBytes(t, filepath.Join(home, "huge.png"), big)

	res, opErr := runtimeFsRead(home, "huge.png")
	if opErr != nil {
		t.Fatalf("read oversized image: %v", opErr)
	}
	if res.Image || !res.Binary || res.Content != nil {
		t.Fatalf("oversized image: image=%v binary=%v content!=nil:%v, want metadata-only", res.Image, res.Binary, res.Content != nil)
	}
}

func TestRuntimeFsGitDiff(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not available")
	}
	home := t.TempDir()
	mem := filepath.Join(home, "memory")
	if err := os.MkdirAll(mem, 0o755); err != nil {
		t.Fatal(err)
	}
	gitInit(t, mem)
	gitCommit(t, mem, "first commit")

	log, opErr := runtimeFsGitLog(context.Background(), home, "memory", 10)
	if opErr != nil || len(log.Commits) != 1 {
		t.Fatalf("setup gitlog: %v commits=%d", opErr, len(log.Commits))
	}
	sha := log.Commits[0].SHA

	diff, opErr := runtimeFsGitDiff(context.Background(), home, "memory", sha)
	if opErr != nil {
		t.Fatalf("gitdiff: %v", opErr)
	}
	if diff.SHA != sha {
		t.Fatalf("diff.SHA=%q want %q", diff.SHA, sha)
	}
	if !strings.Contains(diff.Diff, "first commit") || !strings.Contains(diff.Diff, "f.txt") {
		t.Fatalf("diff missing expected content:\n%s", diff.Diff)
	}

	// Unknown commit → not_found.
	if _, opErr := runtimeFsGitDiff(context.Background(), home, "memory", "0000000000000000000000000000000000000000"); opErr == nil || opErr.code != runtimefs.ErrCodeNotFound {
		got := "nil"
		if opErr != nil {
			got = opErr.code
		}
		t.Fatalf("unknown commit code=%q want not_found", got)
	}
}

func TestRuntimeFsGitDiff_RejectsNonSHARef(t *testing.T) {
	// Injection guard: a ref that is not a bare hex object name never reaches git.
	for _, bad := range []string{"", "abc", "HEAD", "--output=/tmp/x", "deadbeef..cafe", "main; rm -rf /"} {
		if _, opErr := runtimeFsGitDiff(context.Background(), t.TempDir(), "memory", bad); opErr == nil || opErr.code != runtimefs.ErrCodeNotFound {
			got := "nil"
			if opErr != nil {
				got = opErr.code
			}
			t.Fatalf("ref %q code=%q, want not_found (rejected before git)", bad, got)
		}
	}
}

func gitInit(t *testing.T, dir string) {
	t.Helper()
	runGit(t, dir, "init")
	runGit(t, dir, "config", "user.email", "t@example.com")
	runGit(t, dir, "config", "user.name", "Tester")
}

func gitCommit(t *testing.T, dir, msg string) {
	t.Helper()
	writeFile(t, filepath.Join(dir, "f.txt"), msg)
	runGit(t, dir, "add", ".")
	runGit(t, dir, "commit", "-m", msg)
}

func runGit(t *testing.T, dir string, args ...string) {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	cmd.Env = append(os.Environ(),
		"GIT_CONFIG_GLOBAL=/dev/null", "GIT_CONFIG_SYSTEM=/dev/null",
		"GIT_AUTHOR_NAME=Tester", "GIT_AUTHOR_EMAIL=t@example.com",
		"GIT_COMMITTER_NAME=Tester", "GIT_COMMITTER_EMAIL=t@example.com",
	)
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git %s: %v: %s", strings.Join(args, " "), err, out)
	}
}
