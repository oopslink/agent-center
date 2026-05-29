// Package workerdaemon: file_transfer.go is the daemon-side byte-mover for the
// v2.7 post-D3 agent MCP file tools (server contract in
// internal/admin/api/agent_tools_files.go). It moves bytes between an agent's
// local workspace and the center's admin file endpoints, behind a hard
// workspace path-containment guardrail.
//
// This is the SEPARATE follow-up slice the server file noted as "NOT built
// here": the daemon-side byte-mover + path containment. It is a standalone,
// fully unit-tested unit — analog to D1's control_loop.go with a pluggable
// handler — and is NOT yet wired into the MCP tool surface (that is b3 /
// MCPHost). It does NOT touch the dispatch/control loops.
//
// Transport reuse: FileTransferClient borrows the SAME authenticated transport
// as AdminClient (its *http.Client over the unix socket / TCP+TLS endpoint, its
// baseURL, and its bearer token). It does NOT open a fresh http.Client. The
// JSON steps go through AdminClient.doJSON; the streaming PUT (upload bytes) and
// streaming GET (download bytes) are built here with the same
// Authorization: Bearer <token> header, because doJSON is JSON-only and cannot
// stream a file body or write a response body to disk.
//
// Containment (the security core): resolveContainedPath resolves a user path to
// an absolute, symlink-evaluated path and verifies it stays inside the agent's
// allowed root. The allowed root is supplied by the caller (b3, which resolves
// workers/{worker_id}/agents/{agent_id}/workspace per C1 OQ7) so the guard stays
// pure + testable and this slice does not duplicate the layout resolver.
package workerdaemon

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"syscall"

	"github.com/oopslink/agent-center/internal/files"
)

// ErrPathEscapesWorkspace is the sentinel returned when a user-supplied path
// resolves outside the agent's allowed workspace root (".." traversal, an
// absolute path outside root, or a symlink that points outside). Callers can
// errors.Is against it to map to a clear 403-style refusal.
var ErrPathEscapesWorkspace = errors.New("file_transfer: path escapes workspace root")

// FileTransferClient moves bytes for one daemon. It borrows AdminClient's
// authenticated transport (httpc + baseURL + token) rather than constructing a
// new one — every request it issues carries the same bearer the rest of the
// daemon's admin calls use.
type FileTransferClient struct {
	ac *AdminClient
}

// NewFileTransferClient wraps an existing, already-authenticated AdminClient.
// The returned client issues all of its HTTP through that same transport.
func NewFileTransferClient(ac *AdminClient) *FileTransferClient {
	return &FileTransferClient{ac: ac}
}

// resolveContainedPath resolves userPath (which may be relative to root or
// absolute) to an absolute path and verifies it stays inside root. Returns an
// error on any escape. Defends against ".." traversal, absolute paths outside
// root, and symlink escape.
//
// mustExist selects the eval strategy:
//   - true  (upload): the target file must exist; we EvalSymlinks the full
//     path, so a symlink inside the workspace pointing to /etc/passwd resolves
//     to /etc/passwd and is rejected.
//   - false (download dest): the file may not exist yet; we EvalSymlinks the
//     parent directory (which must exist) and re-join the cleaned base name, so
//     a symlinked parent escaping the root is still caught while a not-yet-
//     created leaf is allowed.
//
// In both cases root itself is eval'd + cleaned, and the resolved path must
// equal root or sit under root + os.PathSeparator (NOT a naive string prefix,
// which would wrongly admit /rootEvil for root /root).
func resolveContainedPath(root, userPath string, mustExist bool) (string, error) {
	if strings.TrimSpace(root) == "" {
		return "", fmt.Errorf("file_transfer: empty workspace root")
	}
	if strings.TrimSpace(userPath) == "" {
		return "", fmt.Errorf("file_transfer: empty path")
	}

	// Eval + clean the root so a symlinked root compares against eval'd targets.
	absRoot, err := filepath.Abs(root)
	if err != nil {
		return "", fmt.Errorf("file_transfer: abs root: %w", err)
	}
	evalRoot, err := filepath.EvalSymlinks(absRoot)
	if err != nil {
		return "", fmt.Errorf("file_transfer: eval root %q: %w", absRoot, err)
	}
	evalRoot = filepath.Clean(evalRoot)

	// Resolve userPath relative to the ORIGINAL absolute root (so a relative
	// path is anchored to the workspace), then absolutize + clean.
	candidate := userPath
	if !filepath.IsAbs(candidate) {
		candidate = filepath.Join(absRoot, candidate)
	}
	candidate = filepath.Clean(candidate)

	var resolved string
	if mustExist {
		// Upload: the file must exist — eval the whole path so an in-workspace
		// symlink to an outside target is dereferenced and then rejected.
		resolved, err = filepath.EvalSymlinks(candidate)
		if err != nil {
			return "", fmt.Errorf("file_transfer: eval path %q: %w", candidate, err)
		}
	} else {
		// Download dest: the leaf may not exist. First try to fully resolve the
		// candidate — if the leaf ALREADY exists as a symlink, this dereferences
		// it so a symlink-leaf pointing outside the workspace is caught by the
		// containment check below (not silently followed by the later open). If
		// the leaf simply does not exist yet, fall back to eval'ing the parent dir
		// (which must exist) and re-joining the base — still catching a
		// symlinked-parent escape while tolerating a not-yet-created file.
		if r, e := filepath.EvalSymlinks(candidate); e == nil {
			resolved = r
		} else if errors.Is(e, os.ErrNotExist) {
			parent := filepath.Dir(candidate)
			evalParent, perr := filepath.EvalSymlinks(parent)
			if perr != nil {
				return "", fmt.Errorf("file_transfer: eval parent %q: %w", parent, perr)
			}
			resolved = filepath.Join(filepath.Clean(evalParent), filepath.Base(candidate))
		} else {
			return "", fmt.Errorf("file_transfer: eval path %q: %w", candidate, e)
		}
	}
	resolved = filepath.Clean(resolved)

	if !pathWithinRoot(evalRoot, resolved) {
		return "", fmt.Errorf("%w: %q not within %q", ErrPathEscapesWorkspace, resolved, evalRoot)
	}
	return resolved, nil
}

// pathWithinRoot reports whether p is root itself or strictly under root,
// using a separator-aware prefix check (NOT a naive string prefix: /rootEvil
// must NOT count as within /root).
func pathWithinRoot(root, p string) bool {
	if p == root {
		return true
	}
	return strings.HasPrefix(p, root+string(os.PathSeparator))
}

// =============================================================================
// Upload: upload_file → put bytes → complete
// =============================================================================

// UploadFile streams the regular file at localPath (which MUST resolve inside
// agentRoot) up to the center, in the server's three-step protocol, and returns
// the resulting ac://files/{ulid} URI. scope/scope_id are optional placement
// hints echoed to create + complete; when empty the blob is created
// unreferenced.
//
// Flow:
//  1. POST /admin/agent-tools/upload_file {agent_id, content_type, size, scope,
//     scope_id} → {transfer_id, file_uri}.
//  2. PUT  /admin/files/transfer/{transfer_id}?agent_id=<agentID> — the raw file
//     bytes, ContentLength set to the file size.
//  3. POST /admin/files/transfer/{transfer_id}/complete {agent_id, sha256, size,
//     scope, scope_id}.
//
// On a non-2xx at any step a wrapped error naming the step + status is returned.
func (c *FileTransferClient) UploadFile(ctx context.Context, agentRoot, agentID, localPath string, scope, scopeID string) (fileURI string, err error) {
	contained, err := resolveContainedPath(agentRoot, localPath, true)
	if err != nil {
		return "", err
	}
	info, err := os.Stat(contained)
	if err != nil {
		return "", fmt.Errorf("file_transfer: stat %q: %w", contained, err)
	}
	if !info.Mode().IsRegular() {
		return "", fmt.Errorf("file_transfer: %q is not a regular file (mode %s)", contained, info.Mode())
	}
	size := info.Size()

	contentType := sniffContentType(contained)
	sum, err := sha256File(contained)
	if err != nil {
		return "", fmt.Errorf("file_transfer: sha256 %q: %w", contained, err)
	}

	// Step 1: create the upload session.
	var created struct {
		TransferID  string `json:"transfer_id"`
		TransferURI string `json:"transfer_uri"`
		FileURI     string `json:"file_uri"`
	}
	createBody := map[string]any{
		"agent_id":     agentID,
		"content_type": contentType,
		"size":         size,
		"scope":        scope,
		"scope_id":     scopeID,
	}
	if err := c.ac.doJSON(ctx, http.MethodPost, "/admin/agent-tools/upload_file", createBody, &created); err != nil {
		return "", fmt.Errorf("file_transfer: upload create: %w", err)
	}
	if created.TransferID == "" {
		return "", fmt.Errorf("file_transfer: upload create returned no transfer_id")
	}

	// Step 2: stream the bytes.
	if err := c.putBytes(ctx, created.TransferID, agentID, contained, size); err != nil {
		return "", err
	}

	// Step 3: complete.
	completeBody := map[string]any{
		"agent_id": agentID,
		"sha256":   sum,
		"size":     size,
		"scope":    scope,
		"scope_id": scopeID,
	}
	completePath := "/admin/files/transfer/" + url.PathEscape(created.TransferID) + "/complete"
	if err := c.ac.doJSON(ctx, http.MethodPost, completePath, completeBody, nil); err != nil {
		return "", fmt.Errorf("file_transfer: upload complete: %w", err)
	}
	return created.FileURI, nil
}

// putBytes streams the file at path to PUT /admin/files/transfer/{id}?agent_id=,
// setting ContentLength to size so the server's WriteBlob sees the exact length
// (not a chunked -1). Builds the request on AdminClient's transport with the
// same bearer header doJSON uses.
func (c *FileTransferClient) putBytes(ctx context.Context, transferID, agentID, path string, size int64) error {
	f, err := os.Open(path)
	if err != nil {
		return fmt.Errorf("file_transfer: open %q: %w", path, err)
	}
	defer f.Close()

	base := c.ac.baseURL
	if base == "" {
		base = "http://unix"
	}
	u := base + "/admin/files/transfer/" + url.PathEscape(transferID) +
		"?agent_id=" + url.QueryEscape(agentID)
	req, err := http.NewRequestWithContext(ctx, http.MethodPut, u, f)
	if err != nil {
		return fmt.Errorf("file_transfer: build put: %w", err)
	}
	req.ContentLength = size
	req.Header.Set("Content-Type", "application/octet-stream")
	if c.ac.token != "" {
		req.Header.Set("Authorization", "Bearer "+c.ac.token)
	}
	resp, err := c.ac.httpc.Do(req)
	if err != nil {
		return fmt.Errorf("file_transfer: put bytes: %w", err)
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return fmt.Errorf("file_transfer: put bytes: status=%d body=%s", resp.StatusCode, string(body))
	}
	return nil
}

// =============================================================================
// Download
// =============================================================================

// DownloadFile fetches the blob identified by ulidOrURI (a bare ULID or a full
// ac://files/{ulid} — both accepted; the server route carries the bare ULID
// segment) and streams it to destPath, which MUST resolve inside agentRoot
// (preventing an overwrite outside the workspace). 403 → forbidden / not
// reachable; 404 → not found. The dest is created 0600 (truncating) only after
// a 200, so a 403/404 leaves nothing on disk outside the workspace.
func (c *FileTransferClient) DownloadFile(ctx context.Context, agentRoot, agentID, ulidOrURI, destPath string) (err error) {
	// Containment FIRST — reject an escaping dest before any HTTP call.
	contained, err := resolveContainedPath(agentRoot, destPath, false)
	if err != nil {
		return err
	}
	ulid, err := extractULID(ulidOrURI)
	if err != nil {
		return err
	}

	base := c.ac.baseURL
	if base == "" {
		base = "http://unix"
	}
	u := base + "/admin/files/" + url.PathEscape(ulid) + "?agent_id=" + url.QueryEscape(agentID)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
	if err != nil {
		return fmt.Errorf("file_transfer: build download: %w", err)
	}
	if c.ac.token != "" {
		req.Header.Set("Authorization", "Bearer "+c.ac.token)
	}
	resp, err := c.ac.httpc.Do(req)
	if err != nil {
		return fmt.Errorf("file_transfer: download: %w", err)
	}
	defer resp.Body.Close()

	switch {
	case resp.StatusCode == http.StatusForbidden:
		body, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("file_transfer: download forbidden (not reachable): status=403 body=%s", string(body))
	case resp.StatusCode == http.StatusNotFound:
		body, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("file_transfer: download not found: status=404 body=%s", string(body))
	case resp.StatusCode < 200 || resp.StatusCode >= 300:
		body, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("file_transfer: download: status=%d body=%s", resp.StatusCode, string(body))
	}

	// 200 — stream to the contained dest (create/truncate 0600). O_NOFOLLOW is a
	// race-free kernel backstop: if the leaf is (or becomes, between the
	// containment check and here) a symlink, the open fails rather than following
	// it out of the workspace.
	out, err := os.OpenFile(contained, os.O_WRONLY|os.O_CREATE|os.O_TRUNC|syscall.O_NOFOLLOW, 0o600)
	if err != nil {
		return fmt.Errorf("file_transfer: create %q: %w", contained, err)
	}
	if _, cErr := io.Copy(out, resp.Body); cErr != nil {
		_ = out.Close()
		return fmt.Errorf("file_transfer: write %q: %w", contained, cErr)
	}
	if cErr := out.Close(); cErr != nil {
		return fmt.Errorf("file_transfer: close %q: %w", contained, cErr)
	}
	return nil
}

// extractULID accepts a bare ULID or a full ac://files/{ulid} and returns the
// bare ULID, validating it via the files package so a malformed reference is
// rejected before any HTTP call. The server's download route expects the bare
// ULID in its {ulid} path segment.
func extractULID(s string) (string, error) {
	s = strings.TrimSpace(s)
	if s == "" {
		return "", fmt.Errorf("file_transfer: empty file reference")
	}
	if strings.HasPrefix(s, "ac://") {
		uri, err := files.ParseFileURI(s)
		if err != nil {
			return "", fmt.Errorf("file_transfer: bad file uri %q: %w", s, err)
		}
		return uri.ULID(), nil
	}
	// Bare ulid — validate by round-tripping through NewFileURI.
	uri, err := files.NewFileURI(s)
	if err != nil {
		return "", fmt.Errorf("file_transfer: bad ulid %q: %w", s, err)
	}
	return uri.ULID(), nil
}

// sniffContentType reads up to 512 bytes (http.DetectContentType's window) to
// guess the MIME type, falling back to application/octet-stream on any read
// error or empty file.
func sniffContentType(path string) string {
	f, err := os.Open(path)
	if err != nil {
		return "application/octet-stream"
	}
	defer f.Close()
	buf := make([]byte, 512)
	n, _ := f.Read(buf)
	if n == 0 {
		return "application/octet-stream"
	}
	return http.DetectContentType(buf[:n])
}

// sha256File streams the file through sha256 and returns the hex digest. Cheap
// enough (single sequential read) to always include on complete.
func sha256File(path string) (string, error) {
	f, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer f.Close()
	h := sha256.New()
	if _, err := io.Copy(h, f); err != nil {
		return "", err
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}
