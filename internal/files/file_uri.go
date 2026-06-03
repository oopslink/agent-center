// Package files is the horizontal, business-agnostic file/blob module for
// v2.7 (ADR-0048, plan §2.7 / §10 OQ8). It owns:
//
//   - FileURI: the scope-free `ac://files/{ulid}` value object that every
//     business context (Conversation/Task/Issue/Agent) uses to reference a
//     blob. Identity is an opaque, server-generated ULID — NOT content-addressed.
//   - Resolver: maps a FileURI to a physical bucket path, bucketing by
//     hash(ulid) so the time-ordered ULID prefix does not hot-spot directories.
//   - BlobStore: the storage-layer seam (write-once blobs, ULID identity,
//     content sha256 held only as integrity metadata, never for addressing).
//   - FileReference: the placement layer ({scope, scope_id} -> file_uri),
//     many references to one blob; sharing = adding references, never copying.
//
// A0 ships only the value object + resolver/store/reference SEAMS (interfaces +
// types + repo skeleton). The upload/download/transfer mechanics and the
// reference-count GC land with Environment/FileTransfer in phase D
// (plan §5 D, ADR-0048 §6).
package files

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"strings"

	"github.com/oklog/ulid/v2"
)

// URI scheme + hosts for the `ac://` namespace (ADR-0048 §2).
const (
	uriScheme   = "ac"
	hostFiles   = "files"
	hostTrans   = "transfers"
	filesPrefix = uriScheme + "://" + hostFiles + "/" // ac://files/
	transPrefix = uriScheme + "://" + hostTrans + "/" // ac://transfers/
)

// Sentinel errors (conventions § 0.3 style).
var (
	ErrEmptyURI  = errors.New("files: file uri is empty")
	ErrBadScheme = errors.New("files: file uri must be ac://files/{ulid}")
	ErrBadULID   = errors.New("files: file uri ulid segment is not a valid ULID")
)

// FileURI is the stable, scope-free reference to a blob: `ac://files/{ulid}`.
// The ULID is server-generated and opaque; the URI is returned at
// upload-session create time so callers always have the final reference up
// front (ADR-0048 §2). FileURI is a value object — compare by value.
type FileURI string

func (u FileURI) String() string { return string(u) }

// NewFileURI builds a FileURI from a bare ULID. The ULID must already be a
// valid 26-char Crockford Base32 ULID (generate via internal/idgen).
func NewFileURI(fileULID string) (FileURI, error) {
	if _, err := ulid.Parse(fileULID); err != nil {
		return "", ErrBadULID
	}
	return FileURI(filesPrefix + fileULID), nil
}

// ParseFileURI validates an `ac://files/{ulid}` string and returns it as a
// FileURI. It rejects empty input, the wrong scheme/host, and a non-ULID id.
func ParseFileURI(s string) (FileURI, error) {
	if s == "" {
		return "", ErrEmptyURI
	}
	if !strings.HasPrefix(s, filesPrefix) {
		return "", ErrBadScheme
	}
	id := strings.TrimPrefix(s, filesPrefix)
	if id == "" || strings.Contains(id, "/") {
		return "", ErrBadScheme
	}
	if _, err := ulid.Parse(id); err != nil {
		return "", ErrBadULID
	}
	return FileURI(s), nil
}

// Validate reports whether the receiver is a well-formed file URI.
func (u FileURI) Validate() error {
	_, err := ParseFileURI(string(u))
	return err
}

// ULID returns the opaque file identity carried by the URI. It assumes the
// receiver is valid; callers that did not construct it via New/Parse should
// Validate first.
func (u FileURI) ULID() string {
	return strings.TrimPrefix(string(u), filesPrefix)
}

// bucketHash returns the hex sha256 of the ULID. Bucketing keys off this
// (NOT off the ULID directly) so the time-ordered ULID prefix does not
// concentrate writes in one directory (ADR-0048 §1).
func bucketHash(fileULID string) string {
	sum := sha256.Sum256([]byte(fileULID))
	return hex.EncodeToString(sum[:])
}
