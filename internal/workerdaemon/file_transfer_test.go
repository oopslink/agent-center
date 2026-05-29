package workerdaemon

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/oopslink/agent-center/internal/idgen"
)

// newHTTPFileClient builds a FileTransferClient over a plain httptest.Server
// (TCP), borrowing the AdminClient transport via NewAdminClient pointed at the
// server's URL. We construct the AdminClient by hand (no public URL ctor) and
// set its unexported fields — same package, so this is the idiomatic borrow of
// the existing transport. token is attached so requests carry the bearer like
// production.
func newHTTPFileClient(t *testing.T, srv *httptest.Server) *FileTransferClient {
	t.Helper()
	ac := NewAdminClient("", 5*time.Second)
	ac.baseURL = srv.URL
	ac.httpc = srv.Client()
	ac = ac.WithToken("test-token")
	return NewFileTransferClient(ac)
}

// =============================================================================
// Containment guard (the critical part).
// =============================================================================

func TestResolveContainedPath_Upload_MustExist(t *testing.T) {
	root := t.TempDir()
	// Eval root so comparisons hold on macOS where /tmp is a symlink.
	evalRoot, err := filepath.EvalSymlinks(root)
	if err != nil {
		t.Fatal(err)
	}

	// A valid nested file inside root.
	nestedDir := filepath.Join(root, "sub", "dir")
	if err := os.MkdirAll(nestedDir, 0o755); err != nil {
		t.Fatal(err)
	}
	good := filepath.Join(nestedDir, "f.txt")
	if err := os.WriteFile(good, []byte("hi"), 0o644); err != nil {
		t.Fatal(err)
	}

	// A file outside root, target of an in-workspace symlink.
	outsideDir := t.TempDir()
	outsideFile := filepath.Join(outsideDir, "secret.txt")
	if err := os.WriteFile(outsideFile, []byte("secret"), 0o644); err != nil {
		t.Fatal(err)
	}
	// Symlink inside root → outside file (the symlink-escape attack).
	escapeLink := filepath.Join(root, "escape.txt")
	if err := os.Symlink(outsideFile, escapeLink); err != nil {
		t.Fatal(err)
	}

	tests := []struct {
		name    string
		path    string
		wantErr bool
		wantEsc bool // expect ErrPathEscapesWorkspace specifically
	}{
		{name: "nested valid file", path: good},
		{name: "relative nested valid", path: "sub/dir/f.txt"},
		{name: "dotdot escape", path: "../../etc/passwd", wantErr: true},
		{name: "absolute outside root", path: outsideFile, wantErr: true, wantEsc: true},
		{name: "symlink inside pointing outside", path: escapeLink, wantErr: true, wantEsc: true},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got, err := resolveContainedPath(root, tc.path, true)
			if tc.wantErr {
				if err == nil {
					t.Fatalf("want error, got resolved=%q", got)
				}
				if tc.wantEsc && !errors.Is(err, ErrPathEscapesWorkspace) {
					t.Fatalf("want ErrPathEscapesWorkspace, got %v", err)
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if !strings.HasPrefix(got, evalRoot) {
				t.Fatalf("resolved %q not under root %q", got, evalRoot)
			}
		})
	}
}

func TestResolveContainedPath_Download_ParentExists(t *testing.T) {
	root := t.TempDir()
	evalRoot, err := filepath.EvalSymlinks(root)
	if err != nil {
		t.Fatal(err)
	}

	// Parent dir exists, leaf does not (the download create case).
	subdir := filepath.Join(root, "downloads")
	if err := os.MkdirAll(subdir, 0o755); err != nil {
		t.Fatal(err)
	}

	// Symlinked parent escaping the root.
	outsideDir := t.TempDir()
	linkedDir := filepath.Join(root, "linkdir")
	if err := os.Symlink(outsideDir, linkedDir); err != nil {
		t.Fatal(err)
	}

	tests := []struct {
		name    string
		path    string
		wantErr bool
		wantEsc bool
	}{
		{name: "new leaf in existing subdir", path: filepath.Join(subdir, "new.bin")},
		{name: "new leaf relative", path: "downloads/new2.bin"},
		{name: "path equal to root", path: root},
		{name: "dotdot escape leaf", path: "../outside.bin", wantErr: true, wantEsc: true},
		{name: "symlinked parent escape", path: filepath.Join(linkedDir, "x.bin"), wantErr: true, wantEsc: true},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got, err := resolveContainedPath(root, tc.path, false)
			if tc.wantErr {
				if err == nil {
					t.Fatalf("want error, got resolved=%q", got)
				}
				if tc.wantEsc && !errors.Is(err, ErrPathEscapesWorkspace) {
					t.Fatalf("want ErrPathEscapesWorkspace, got %v", err)
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if got != evalRoot && !strings.HasPrefix(got, evalRoot+string(os.PathSeparator)) {
				t.Fatalf("resolved %q not within root %q", got, evalRoot)
			}
		})
	}
}

// Guard against the naive-prefix bug: /rootEvil must NOT count as within /root.
func TestResolveContainedPath_SiblingPrefixRejected(t *testing.T) {
	base := t.TempDir()
	root := filepath.Join(base, "root")
	sibling := filepath.Join(base, "rootEvil")
	if err := os.MkdirAll(root, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(sibling, 0o755); err != nil {
		t.Fatal(err)
	}
	evil := filepath.Join(sibling, "f.bin")
	if _, err := resolveContainedPath(root, evil, false); !errors.Is(err, ErrPathEscapesWorkspace) {
		t.Fatalf("sibling /rootEvil should escape /root; got %v", err)
	}
}

// =============================================================================
// Upload orchestration.
// =============================================================================

type uploadRecorder struct {
	mu          sync.Mutex
	createBody  map[string]any
	putBytes    []byte
	putQuery    string
	completeRaw []byte
	transferID  string
	fileURI     string
}

func (rec *uploadRecorder) handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("POST /admin/agent-tools/upload_file", func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		rec.mu.Lock()
		_ = json.Unmarshal(b, &rec.createBody)
		rec.mu.Unlock()
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"transfer_id":"` + rec.transferID + `","transfer_uri":"ac://transfers/` + rec.transferID + `","file_uri":"` + rec.fileURI + `"}`))
	})
	mux.HandleFunc("PUT /admin/files/transfer/{transfer_id}", func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		rec.mu.Lock()
		rec.putBytes = b
		rec.putQuery = r.URL.RawQuery
		rec.mu.Unlock()
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"written":true}`))
	})
	mux.HandleFunc("POST /admin/files/transfer/{transfer_id}/complete", func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		rec.mu.Lock()
		rec.completeRaw = b
		rec.mu.Unlock()
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"file_uri":"` + rec.fileURI + `"}`))
	})
	return mux
}

func TestUploadFile_ThreeStepFlow(t *testing.T) {
	rec := &uploadRecorder{transferID: "TR-1", fileURI: "ac://files/" + idgen.MustNewULID()}
	srv := httptest.NewServer(rec.handler())
	defer srv.Close()
	client := newHTTPFileClient(t, srv)

	root := t.TempDir()
	content := []byte("the exact bytes to upload\n")
	local := filepath.Join(root, "payload.txt")
	if err := os.WriteFile(local, content, 0o644); err != nil {
		t.Fatal(err)
	}

	gotURI, err := client.UploadFile(context.Background(), root, "agent-7", local, "agent", "agent-7")
	if err != nil {
		t.Fatalf("UploadFile: %v", err)
	}
	if gotURI != rec.fileURI {
		t.Fatalf("file_uri=%q want %q", gotURI, rec.fileURI)
	}

	rec.mu.Lock()
	defer rec.mu.Unlock()

	// create body shape
	if rec.createBody["agent_id"] != "agent-7" {
		t.Fatalf("create agent_id=%v", rec.createBody["agent_id"])
	}
	if int64(rec.createBody["size"].(float64)) != int64(len(content)) {
		t.Fatalf("create size=%v want %d", rec.createBody["size"], len(content))
	}
	if rec.createBody["scope"] != "agent" || rec.createBody["scope_id"] != "agent-7" {
		t.Fatalf("create scope=%v scope_id=%v", rec.createBody["scope"], rec.createBody["scope_id"])
	}
	if rec.createBody["content_type"] == "" {
		t.Fatalf("create content_type empty")
	}

	// PUT got the EXACT bytes + agent_id query.
	if string(rec.putBytes) != string(content) {
		t.Fatalf("put bytes=%q want %q", rec.putBytes, content)
	}
	if !strings.Contains(rec.putQuery, "agent_id=agent-7") {
		t.Fatalf("put query=%q", rec.putQuery)
	}

	// complete called with size + sha256.
	var comp map[string]any
	if err := json.Unmarshal(rec.completeRaw, &comp); err != nil {
		t.Fatalf("complete body: %v", err)
	}
	if int64(comp["size"].(float64)) != int64(len(content)) {
		t.Fatalf("complete size=%v", comp["size"])
	}
	if comp["sha256"] == "" {
		t.Fatalf("complete sha256 empty")
	}
}

func TestUploadFile_RejectsEscapingPath(t *testing.T) {
	// No server should be hit; use a server that fails the test if called.
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Errorf("server must not be called on containment failure: %s", r.URL.Path)
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer srv.Close()
	client := newHTTPFileClient(t, srv)

	root := t.TempDir()
	outside := t.TempDir()
	outFile := filepath.Join(outside, "x.txt")
	if err := os.WriteFile(outFile, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := client.UploadFile(context.Background(), root, "a1", outFile, "", ""); !errors.Is(err, ErrPathEscapesWorkspace) {
		t.Fatalf("want ErrPathEscapesWorkspace, got %v", err)
	}
}

func TestUploadFile_RejectsDirectory(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Errorf("server must not be called for a directory: %s", r.URL.Path)
	}))
	defer srv.Close()
	client := newHTTPFileClient(t, srv)

	root := t.TempDir()
	dir := filepath.Join(root, "adir")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if _, err := client.UploadFile(context.Background(), root, "a1", dir, "", ""); err == nil {
		t.Fatal("want error for directory upload")
	}
}

// =============================================================================
// Download orchestration.
// =============================================================================

func TestDownloadFile_StreamsBytesToContainedDest(t *testing.T) {
	want := []byte("downloaded blob contents")
	ulid := idgen.MustNewULID()
	var gotQuery string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !strings.HasPrefix(r.URL.Path, "/admin/files/") {
			http.NotFound(w, r)
			return
		}
		gotQuery = r.URL.RawQuery
		w.Header().Set("Content-Type", "text/plain")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write(want)
	}))
	defer srv.Close()
	client := newHTTPFileClient(t, srv)

	root := t.TempDir()
	dest := filepath.Join(root, "out", "got.bin")
	if err := os.MkdirAll(filepath.Dir(dest), 0o755); err != nil {
		t.Fatal(err)
	}

	if err := client.DownloadFile(context.Background(), root, "agent-9", ulid, dest); err != nil {
		t.Fatalf("DownloadFile: %v", err)
	}
	got, err := os.ReadFile(dest)
	if err != nil {
		t.Fatalf("read dest: %v", err)
	}
	if string(got) != string(want) {
		t.Fatalf("dest bytes=%q want %q", got, want)
	}
	if !strings.Contains(gotQuery, "agent_id=agent-9") {
		t.Fatalf("download query=%q", gotQuery)
	}
}

func TestDownloadFile_AcceptsFullURI(t *testing.T) {
	want := []byte("xyz")
	ulid := idgen.MustNewULID()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// The path segment must be the BARE ulid.
		if r.URL.Path != "/admin/files/"+ulid {
			t.Errorf("path=%q want bare ulid segment", r.URL.Path)
		}
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write(want)
	}))
	defer srv.Close()
	client := newHTTPFileClient(t, srv)

	root := t.TempDir()
	dest := filepath.Join(root, "f.bin")
	if err := client.DownloadFile(context.Background(), root, "a1", "ac://files/"+ulid, dest); err != nil {
		t.Fatalf("DownloadFile full uri: %v", err)
	}
}

func TestDownloadFile_ForbiddenWritesNothing(t *testing.T) {
	ulid := idgen.MustNewULID()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, `{"error":"file_not_reachable"}`, http.StatusForbidden)
	}))
	defer srv.Close()
	client := newHTTPFileClient(t, srv)

	root := t.TempDir()
	dest := filepath.Join(root, "should_not_exist.bin")
	err := client.DownloadFile(context.Background(), root, "a1", ulid, dest)
	if err == nil {
		t.Fatal("want forbidden error")
	}
	if !strings.Contains(err.Error(), "403") {
		t.Fatalf("error should mention 403: %v", err)
	}
	if _, statErr := os.Stat(dest); !os.IsNotExist(statErr) {
		t.Fatalf("dest must not be created on 403; stat err=%v", statErr)
	}
}

// TestDownloadFile_RejectsSymlinkLeafEscape guards the subtle escape: the dest
// leaf is a pre-existing symlink inside the workspace pointing to a file OUTSIDE
// it. A naive parent-only check would pass and the open would follow the symlink
// and overwrite the outside target. The download must refuse and leave the
// outside target untouched.
func TestDownloadFile_RejectsSymlinkLeafEscape(t *testing.T) {
	served := false
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		served = true
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("PWNED"))
	}))
	defer srv.Close()
	client := newHTTPFileClient(t, srv)

	root := t.TempDir()
	outside := t.TempDir()
	secret := filepath.Join(outside, "secret.txt")
	if err := os.WriteFile(secret, []byte("ORIGINAL"), 0o600); err != nil {
		t.Fatal(err)
	}
	// Plant a symlink INSIDE the workspace pointing OUTSIDE it.
	link := filepath.Join(root, "leak")
	if err := os.Symlink(secret, link); err != nil {
		t.Skipf("symlink unsupported: %v", err)
	}

	err := client.DownloadFile(context.Background(), root, "a1", idgen.MustNewULID(), link)
	if err == nil {
		t.Fatal("want error downloading onto a symlink-leaf that escapes the workspace")
	}
	// Whether caught at the containment check (resolved outside root) or by the
	// O_NOFOLLOW open, the outside target must be untouched.
	got, rerr := os.ReadFile(secret)
	if rerr != nil {
		t.Fatalf("read secret: %v", rerr)
	}
	if string(got) != "ORIGINAL" {
		t.Fatalf("outside target was overwritten: %q (served=%v)", got, served)
	}
}

func TestDownloadFile_RejectsEscapingDestBeforeHTTP(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Errorf("server must not be called when dest escapes: %s", r.URL.Path)
	}))
	defer srv.Close()
	client := newHTTPFileClient(t, srv)

	root := t.TempDir()
	outside := t.TempDir()
	dest := filepath.Join(outside, "escape.bin")
	err := client.DownloadFile(context.Background(), root, "a1", idgen.MustNewULID(), dest)
	if !errors.Is(err, ErrPathEscapesWorkspace) {
		t.Fatalf("want ErrPathEscapesWorkspace, got %v", err)
	}
}
