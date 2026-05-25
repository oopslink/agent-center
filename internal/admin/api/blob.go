// Package api — blob.go: POST /admin/blob/put.
//
// v2.3-3b (task #29). Worker daemons write artifact blobs to the center's
// BlobStore via this endpoint before calling artifact/append with the
// blob_ref. Mirrors the local-on-host write path; transport is the
// admin unix socket + bearer auth.
//
// Path safety: relPath must not be empty, must not start with `/`, and
// must not contain `..` (no path escape). Rejected requests get 400
// invalid_rel_path. Bytes are base64-encoded on the wire (std encoding,
// not URL).
package api

import (
	"bytes"
	"encoding/base64"
	"errors"
	"net/http"
	"strings"

	"github.com/oopslink/agent-center/internal/admintoken"
)

// blobPutScopeRequired gates /admin/blob/put. Real worker tokens carry it;
// CLI tokens generally do not.
const blobPutScopeRequired admintoken.Scope = "blob:put"

// blobPutReq mirrors the worker-daemon BlobPut payload.
type blobPutReq struct {
	RelPath       string `json:"rel_path"`
	ContentBase64 string `json:"content_base64"`
}

// blobPutResp echoes the stored rel_path so the caller can verify the
// server-side normalisation (currently identity — kept for future-proofing).
type blobPutResp struct {
	RelPath string `json:"rel_path"`
}

// blobPutHandler decodes + validates + writes the blob bytes through
// BlobStore.Put. Returns 400 on invalid rel_path / base64, 503 when
// BlobStore is not wired, 200 with the stored rel_path on success.
func (s *Server) blobPutHandler(w http.ResponseWriter, r *http.Request) {
	if !RequireScope(w, r, blobPutScopeRequired) {
		return
	}
	d := hd(r)
	if d.BlobStore == nil {
		writeError(w, http.StatusServiceUnavailable, "blob_store_not_wired",
			"BlobStore is wired only when blob_store.root is configured")
		return
	}
	var req blobPutReq
	if err := decodeJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_json", err.Error())
		return
	}
	if err := validateRelPath(req.RelPath); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_rel_path", err.Error())
		return
	}
	b, err := base64.StdEncoding.DecodeString(req.ContentBase64)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid_base64", err.Error())
		return
	}
	if err := d.BlobStore.Put(r.Context(), req.RelPath, bytes.NewReader(b), int64(len(b))); err != nil {
		mapDomainError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, blobPutResp{RelPath: req.RelPath})
}

// validateRelPath enforces:
//   - non-empty
//   - no leading `/` (paths are relative to BlobStore root)
//   - no `..` segments (no path escape)
//
// The check is intentionally syntactic; LocalDir.Put also clean-joins
// against the root, so this is defense-in-depth.
func validateRelPath(p string) error {
	if strings.TrimSpace(p) == "" {
		return errors.New("rel_path required")
	}
	if strings.HasPrefix(p, "/") {
		return errors.New("rel_path must be relative (no leading '/')")
	}
	// Split on both `/` and `\` so Windows-style separators don't slip
	// `..` past the check on an OS-agnostic store.
	for _, seg := range strings.FieldsFunc(p, func(r rune) bool { return r == '/' || r == '\\' }) {
		if seg == ".." {
			return errors.New("rel_path must not contain '..' segments")
		}
	}
	return nil
}
