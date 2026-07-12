package centergit

import (
	"errors"
	"fmt"
	"strings"
)

// RepoKind enumerates the three center-hosted git repo scopes (§4.2).
type RepoKind string

const (
	// RepoKindAgent is a single agent's private memory repo.
	RepoKindAgent RepoKind = "agent"
	// RepoKindTeam is a team's shared memory repo (all members rw).
	RepoKindTeam RepoKind = "team"
	// RepoKindGlobal is the single platform-level repo (all agents read).
	RepoKindGlobal RepoKind = "global"
)

// ErrInvalidRepoRef is returned for a malformed or unsafe RepoRef.
var ErrInvalidRepoRef = errors.New("centergit: invalid repo ref")

// RepoRef identifies one bare repo on the center host. For RepoKindGlobal the
// ID is empty (there is exactly one global repo).
type RepoRef struct {
	Kind RepoKind
	ID   string
}

// AgentRepo returns the ref for agent id's private repo.
func AgentRepo(id string) RepoRef { return RepoRef{Kind: RepoKindAgent, ID: id} }

// TeamRepo returns the ref for team id's shared repo.
func TeamRepo(id string) RepoRef { return RepoRef{Kind: RepoKindTeam, ID: id} }

// GlobalRepo returns the ref for the single global repo.
func GlobalRepo() RepoRef { return RepoRef{Kind: RepoKindGlobal} }

// Validate checks the ref is well-formed and its ID is a safe single path
// segment (no separators, no traversal, no leading dot). Global refs must carry
// no ID.
func (r RepoRef) Validate() error {
	switch r.Kind {
	case RepoKindGlobal:
		if r.ID != "" {
			return fmt.Errorf("%w: global repo takes no id", ErrInvalidRepoRef)
		}
		return nil
	case RepoKindAgent, RepoKindTeam:
		return validateSegment(r.ID)
	default:
		return fmt.Errorf("%w: unknown kind %q", ErrInvalidRepoRef, r.Kind)
	}
}

// dirName is the on-disk bare directory path (slash-separated, relative to the
// host root): "agent/<id>.git", "team/<id>.git" or "global.git".
func (r RepoRef) dirName() string {
	switch r.Kind {
	case RepoKindGlobal:
		return "global.git"
	default:
		return string(r.Kind) + "/" + r.ID + ".git"
	}
}

// String is a stable, human-readable identifier for logs/errors.
func (r RepoRef) String() string {
	if r.Kind == RepoKindGlobal {
		return "global"
	}
	return string(r.Kind) + ":" + r.ID
}

// validateSegment rejects empty, over-long, traversal or separator-bearing ids
// so a RepoRef can never escape the host root or the URL namespace.
func validateSegment(s string) error {
	if s == "" {
		return fmt.Errorf("%w: empty id", ErrInvalidRepoRef)
	}
	if len(s) > 128 {
		return fmt.Errorf("%w: id too long (%d > 128)", ErrInvalidRepoRef, len(s))
	}
	if s == "." || s == ".." || strings.HasPrefix(s, ".") {
		return fmt.Errorf("%w: id may not start with a dot: %q", ErrInvalidRepoRef, s)
	}
	if strings.ContainsAny(s, "/\\") {
		return fmt.Errorf("%w: id may not contain path separators: %q", ErrInvalidRepoRef, s)
	}
	if strings.ContainsRune(s, 0) {
		return fmt.Errorf("%w: id may not contain NUL: %q", ErrInvalidRepoRef, s)
	}
	return nil
}

// parseRepoPath splits a smart-HTTP request path (already stripped of any mount
// prefix, with or without a leading slash) into the RepoRef and the git
// sub-path ("info/refs", "git-upload-pack", "git-receive-pack", …).
//
// Accepted forms:
//
//	agent/<id>.git/<sub>
//	team/<id>.git/<sub>
//	global.git/<sub>
func parseRepoPath(p string) (RepoRef, string, error) {
	p = strings.TrimPrefix(p, "/")
	segs := strings.Split(p, "/")

	// global.git/<sub...>
	if segs[0] == "global.git" {
		ref := GlobalRepo()
		return ref, strings.Join(segs[1:], "/"), nil
	}

	// <kind>/<id>.git/<sub...>
	if len(segs) < 2 {
		return RepoRef{}, "", fmt.Errorf("%w: unrecognised path %q", ErrInvalidRepoRef, p)
	}
	kind := RepoKind(segs[0])
	if kind != RepoKindAgent && kind != RepoKindTeam {
		return RepoRef{}, "", fmt.Errorf("%w: unknown repo kind %q", ErrInvalidRepoRef, segs[0])
	}
	if !strings.HasSuffix(segs[1], ".git") {
		return RepoRef{}, "", fmt.Errorf("%w: repo segment must end in .git: %q", ErrInvalidRepoRef, segs[1])
	}
	id := strings.TrimSuffix(segs[1], ".git")
	ref := RepoRef{Kind: kind, ID: id}
	if err := ref.Validate(); err != nil {
		return RepoRef{}, "", err
	}
	return ref, strings.Join(segs[2:], "/"), nil
}
