package files

import (
	"path/filepath"
)

// Resolver maps a FileURI to a physical location. A0 ships the local-filesystem
// resolver skeleton only — it computes paths but performs no I/O. The first
// real backend (read/write) lands with FileTransfer in phase D (ADR-0048 §6);
// because addressing is by opaque ULID, the backend can be swapped without
// changing any URI.
type Resolver interface {
	// ObjectPath returns the absolute on-disk path a blob would live at for
	// the given file URI. It does not check existence.
	ObjectPath(uri FileURI) (string, error)
}

// LocalResolver resolves `ac://files/{ulid}` to
//
//	{root}/objects/{h1}/{h2}/{ulid}
//
// where {h1}{h2} are 2-char hex prefixes of hash(ulid) — bucketing on the
// hash (not the time-ordered ULID) spreads writes across directories and
// avoids hot-spotting (ADR-0048 §1, plan §2.7).
type LocalResolver struct {
	// Root is the files base dir, e.g. ~/.agent-center/files.
	Root string
}

// NewLocalResolver builds a LocalResolver rooted at root (e.g.
// ~/.agent-center/files).
func NewLocalResolver(root string) *LocalResolver {
	return &LocalResolver{Root: root}
}

// ObjectPath implements Resolver.
func (r *LocalResolver) ObjectPath(uri FileURI) (string, error) {
	if err := uri.Validate(); err != nil {
		return "", err
	}
	id := uri.ULID()
	h := bucketHash(id)
	h1, h2 := h[0:2], h[2:4]
	return filepath.Join(r.Root, "objects", h1, h2, id), nil
}
