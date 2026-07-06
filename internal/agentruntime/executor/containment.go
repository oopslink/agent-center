package executor

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// resolveContained resolves userPath (relative to root, or absolute) to an
// absolute, symlink-evaluated path and verifies it stays inside root, returning
// ErrPathEscapesWorkspace on any escape. This is the workspace containment guard
// (design §6.D) — it mirrors the daemon file_transfer guard exactly so the two
// fences behave identically.
//
// mustExist selects the eval strategy:
//   - true:  the leaf must exist; EvalSymlinks the full path so an in-root symlink
//     pointing outside resolves to its target and is then rejected.
//   - false: the leaf may not exist yet; EvalSymlinks the parent (which must
//     exist) and re-join the cleaned base, catching a symlinked-parent escape
//     while tolerating a not-yet-created file.
func resolveContained(root, userPath string, mustExist bool) (string, error) {
	if strings.TrimSpace(root) == "" {
		return "", errors.New("executor: empty workspace root")
	}
	if strings.TrimSpace(userPath) == "" {
		return "", errors.New("executor: empty path")
	}

	absRoot, err := filepath.Abs(root)
	if err != nil {
		return "", fmt.Errorf("executor: abs root: %w", err)
	}
	evalRoot, err := filepath.EvalSymlinks(absRoot)
	if err != nil {
		return "", fmt.Errorf("executor: eval root %q: %w", absRoot, err)
	}
	evalRoot = filepath.Clean(evalRoot)

	candidate := userPath
	if !filepath.IsAbs(candidate) {
		candidate = filepath.Join(absRoot, candidate)
	}
	candidate = filepath.Clean(candidate)

	var resolved string
	if mustExist {
		resolved, err = filepath.EvalSymlinks(candidate)
		if err != nil {
			return "", fmt.Errorf("executor: eval path %q: %w", candidate, err)
		}
	} else {
		if r, e := filepath.EvalSymlinks(candidate); e == nil {
			resolved = r
		} else if errors.Is(e, os.ErrNotExist) {
			parent := filepath.Dir(candidate)
			evalParent, perr := filepath.EvalSymlinks(parent)
			if perr != nil {
				return "", fmt.Errorf("executor: eval parent %q: %w", parent, perr)
			}
			resolved = filepath.Join(filepath.Clean(evalParent), filepath.Base(candidate))
		} else {
			return "", fmt.Errorf("executor: eval path %q: %w", candidate, e)
		}
	}
	resolved = filepath.Clean(resolved)

	if !pathWithinRoot(evalRoot, resolved) {
		return "", fmt.Errorf("%w: %q not within %q", ErrPathEscapesWorkspace, resolved, evalRoot)
	}
	return resolved, nil
}

// resolveWithin is the existence-agnostic variant used for whole directories the
// protocol itself owns (e.g. an executor dir before RemoveAll): it resolves child
// as far as it exists and confirms it is root itself or sits strictly under root.
// Unlike resolveContained it does not require the leaf or its parent to already be
// fully materialised — it walks up to the nearest existing ancestor, eval's that,
// and re-appends the remaining segments.
func resolveWithin(root, child string) (string, error) {
	if strings.TrimSpace(root) == "" {
		return "", errors.New("executor: empty containment root")
	}
	absRoot, err := filepath.Abs(root)
	if err != nil {
		return "", fmt.Errorf("executor: abs root: %w", err)
	}
	absChild, err := filepath.Abs(child)
	if err != nil {
		return "", fmt.Errorf("executor: abs child: %w", err)
	}
	// Resolve the existing prefix of BOTH the same way so symlinked ancestors
	// (e.g. macOS /var → /private/var) don't cause a spurious mismatch when root
	// itself is not created yet.
	evalRoot := evalExistingPrefix(filepath.Clean(absRoot))
	resolved := evalExistingPrefix(filepath.Clean(absChild))

	if !pathWithinRoot(evalRoot, resolved) {
		return "", fmt.Errorf("%w: %q not within %q", ErrPathEscapesWorkspace, resolved, evalRoot)
	}
	return resolved, nil
}

// evalExistingPrefix eval-symlinks the longest existing ancestor of p and
// re-appends the non-existent remainder, so symlink escapes in the existing part
// are dereferenced while a not-yet-created tail is tolerated.
func evalExistingPrefix(p string) string {
	var tail []string
	cur := p
	for {
		if eval, err := filepath.EvalSymlinks(cur); err == nil {
			parts := append([]string{eval}, tail...)
			return filepath.Clean(filepath.Join(parts...))
		}
		parent := filepath.Dir(cur)
		if parent == cur {
			return filepath.Clean(p) // reached root with nothing existing
		}
		tail = append([]string{filepath.Base(cur)}, tail...)
		cur = parent
	}
}

// pathWithinRoot reports whether p is root itself or strictly under root, using a
// separator-aware prefix check (NOT a naive string prefix: /rootEvil must NOT
// count as within /root).
func pathWithinRoot(root, p string) bool {
	if p == root {
		return true
	}
	return strings.HasPrefix(p, root+string(os.PathSeparator))
}
