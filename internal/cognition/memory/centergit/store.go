package centergit

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"gopkg.in/yaml.v3"

	"github.com/oopslink/agent-center/internal/cognition/memory"
	"github.com/oopslink/agent-center/internal/idgen"
)

// entriesDir is the sub-directory (relative to a working copy root) that holds
// one file per experience. indexFile is the derived, never-hand-edited index.
const (
	entriesDir = "entries"
	indexFile  = "MEMORY.md"
)

// Store errors.
var (
	// ErrPushRetriesExhausted means pull-rebase-retry ran out of attempts while
	// racing concurrent writers.
	ErrPushRetriesExhausted = errors.New("centergit: push retries exhausted")
	// ErrInvalidEntry means an Entry failed validation.
	ErrInvalidEntry = errors.New("centergit: invalid entry")
)

// Author is the git identity a Store commits under.
type Author struct {
	Name  string
	Email string
}

// Entry is one memory experience. Design §9 mandates 每条经验一文件 so concurrent
// writers touch different files (git auto-merges), and the shared MEMORY.md
// index is DERIVED from entries — never hand-edited.
type Entry struct {
	// Slug is the human, path-safe stem of the file name (e.g.
	// "prefer-table-driven-tests"). Combined with a uuid it forms the file name.
	Slug string
	// Title is an optional heading for the entry body.
	Title string
	// Description is the one-line hook that lands in the index.
	Description string
	// Body is the markdown content (without frontmatter).
	Body string
	// Type is an optional classification (user/feedback/project/reference…).
	Type string
}

// entryFrontmatter is the YAML header persisted at the top of every entry file
// and re-read to regenerate the index. A struct (not a map) keeps key order
// deterministic across writes.
type entryFrontmatter struct {
	Name        string `yaml:"name"`
	Title       string `yaml:"title,omitempty"`
	Description string `yaml:"description"`
	UUID        string `yaml:"uuid"`
	Type        string `yaml:"type,omitempty"`
}

// Store is the client-side (runtime) view of a checked-out center repo working
// copy. It writes per-entry files, regenerates the index deterministically, and
// pushes with pull-rebase-retry to absorb concurrent team writes (§5, §9).
type Store struct {
	dir          string
	runner       memory.GitRunner
	newID        func() string
	homeOverride string
}

// StoreOption configures a Store.
type StoreOption func(*Store)

// WithIDGen injects the entry uuid generator (tests use a deterministic one).
func WithIDGen(fn func() string) StoreOption {
	return func(s *Store) { s.newID = fn }
}

// WithHomeOverride sets HOME/XDG_CONFIG_HOME for git invocations (test hygiene).
func WithHomeOverride(home string) StoreOption {
	return func(s *Store) { s.homeOverride = home }
}

// NewStore wires a Store over the working-copy dir. A nil runner defaults to the
// real git binary; the default uuid source is a ULID.
func NewStore(dir string, runner memory.GitRunner, opts ...StoreOption) *Store {
	if runner == nil {
		runner = memory.NewExecGitRunner()
	}
	s := &Store{dir: dir, runner: runner, newID: idgen.MustNewULID}
	for _, o := range opts {
		o(s)
	}
	return s
}

// WriteEntry persists e as entries/<slug>-<uuid>.md and returns the repo-relative
// path. It does NOT commit or regenerate the index — callers batch those (see
// RegenerateIndex + SyncPush) so a burst of writes is one commit.
func (s *Store) WriteEntry(e Entry) (string, error) {
	if err := validateSegment(e.Slug); err != nil {
		return "", fmt.Errorf("%w: slug: %v", ErrInvalidEntry, err)
	}
	if strings.TrimSpace(e.Description) == "" {
		return "", fmt.Errorf("%w: description is required (it seeds the index)", ErrInvalidEntry)
	}
	id := s.newID()
	fm := entryFrontmatter{
		Name:        e.Slug,
		Title:       e.Title,
		Description: strings.TrimSpace(e.Description),
		UUID:        id,
		Type:        e.Type,
	}
	rel := filepath.ToSlash(filepath.Join(entriesDir, e.Slug+"-"+id+".md"))
	abs := filepath.Join(s.dir, filepath.FromSlash(rel))
	if err := os.MkdirAll(filepath.Dir(abs), 0o700); err != nil {
		return "", err
	}
	content, err := renderEntry(fm, e.Body)
	if err != nil {
		return "", err
	}
	if err := os.WriteFile(abs, []byte(content), 0o600); err != nil {
		return "", err
	}
	return rel, nil
}

// renderEntry serialises frontmatter + body into the on-disk format.
func renderEntry(fm entryFrontmatter, body string) (string, error) {
	y, err := yaml.Marshal(fm)
	if err != nil {
		return "", err
	}
	var b strings.Builder
	b.WriteString("---\n")
	b.Write(y)
	b.WriteString("---\n\n")
	b.WriteString(strings.TrimRight(body, "\n"))
	b.WriteString("\n")
	return b.String(), nil
}

// indexRow is one parsed entry used to build the index.
type indexRow struct {
	file        string // repo-relative path
	name        string
	description string
}

// ListEntries parses every entries/*.md file's frontmatter. Entries are sorted
// by (name, file) for a stable, deterministic order.
func (s *Store) ListEntries() ([]indexRow, error) {
	dir := filepath.Join(s.dir, entriesDir)
	ents, err := os.ReadDir(dir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	var rows []indexRow
	for _, de := range ents {
		if de.IsDir() || !strings.HasSuffix(de.Name(), ".md") {
			continue
		}
		fm, perr := parseFrontmatter(filepath.Join(dir, de.Name()))
		if perr != nil {
			return nil, fmt.Errorf("parse %s: %w", de.Name(), perr)
		}
		rows = append(rows, indexRow{
			file:        filepath.ToSlash(filepath.Join(entriesDir, de.Name())),
			name:        fm.Name,
			description: fm.Description,
		})
	}
	sort.Slice(rows, func(i, j int) bool {
		if rows[i].name != rows[j].name {
			return rows[i].name < rows[j].name
		}
		return rows[i].file < rows[j].file
	})
	return rows, nil
}

// parseFrontmatter extracts the leading YAML frontmatter block of a file.
func parseFrontmatter(path string) (entryFrontmatter, error) {
	var fm entryFrontmatter
	raw, err := os.ReadFile(path)
	if err != nil {
		return fm, err
	}
	text := string(raw)
	if !strings.HasPrefix(text, "---\n") {
		return fm, fmt.Errorf("missing frontmatter")
	}
	rest := text[len("---\n"):]
	end := strings.Index(rest, "\n---")
	if end < 0 {
		return fm, fmt.Errorf("unterminated frontmatter")
	}
	if err := yaml.Unmarshal([]byte(rest[:end]), &fm); err != nil {
		return fm, err
	}
	return fm, nil
}

// RegenerateIndex rebuilds MEMORY.md purely from the entry files (§9: 索引从条目
// 派生、不手编). The output is deterministic so identical entry sets on two
// runtimes produce byte-identical indexes → no spurious merge conflicts.
func (s *Store) RegenerateIndex() error {
	rows, err := s.ListEntries()
	if err != nil {
		return err
	}
	var b strings.Builder
	b.WriteString("# Memory Index\n\n")
	b.WriteString("<!-- GENERATED from entries/ — do not edit by hand. See centergit.Store.RegenerateIndex. -->\n\n")
	if len(rows) == 0 {
		b.WriteString("_No entries yet._\n")
	}
	for _, r := range rows {
		desc := strings.TrimSpace(r.description)
		if desc == "" {
			desc = "(no description)"
		}
		fmt.Fprintf(&b, "- [%s](%s) — %s\n", r.name, r.file, desc)
	}
	return os.WriteFile(filepath.Join(s.dir, indexFile), []byte(b.String()), 0o600)
}

// Commit stages the whole working tree and commits under author. It is a no-op
// (returns nil) when the tree is clean.
func (s *Store) Commit(ctx context.Context, author Author, message string) error {
	if err := s.requireAuthor(author); err != nil {
		return err
	}
	env := baseGitEnv(s.homeOverride, author.Name, author.Email)
	out, err := s.runner.Run(ctx, s.dir, env, "status", "--porcelain")
	if err != nil {
		return fmt.Errorf("%w: status: %v: %s", ErrGitOpFailed, err, out)
	}
	if strings.TrimSpace(out) == "" {
		return nil
	}
	if out, err := s.runner.Run(ctx, s.dir, env, "add", "-A"); err != nil {
		return fmt.Errorf("%w: add -A: %v: %s", ErrGitOpFailed, err, out)
	}
	if out, err := s.runner.Run(ctx, s.dir, env, "-c", "commit.gpgsign=false", "commit", "-m", message); err != nil {
		return fmt.Errorf("%w: commit: %v: %s", ErrGitOpFailed, err, out)
	}
	return nil
}

// SyncPush regenerates the index, commits, then pushes to remote/branch. On a
// non-fast-forward rejection (a concurrent team writer landed first) it runs
// pull --rebase and retries, up to maxRetries times — the §9 "push 前
// pull-rebase-retry 兜并发写" contract. Because entries are per-file, the rebase
// almost never conflicts; only the derived index could, and it is regenerated
// deterministically after each rebase.
func (s *Store) SyncPush(ctx context.Context, remote, branch string, author Author, message string, maxRetries int) error {
	if err := s.requireAuthor(author); err != nil {
		return err
	}
	if maxRetries < 0 {
		maxRetries = 0
	}
	env := baseGitEnv(s.homeOverride, author.Name, author.Email)

	if err := s.RegenerateIndex(); err != nil {
		return err
	}
	if err := s.Commit(ctx, author, message); err != nil {
		return err
	}

	for attempt := 0; attempt <= maxRetries; attempt++ {
		out, err := s.runner.Run(ctx, s.dir, env, "push", remote, "HEAD:"+branch)
		if err == nil {
			return nil
		}
		if !isNonFastForward(out) {
			return fmt.Errorf("%w: push: %v: %s", ErrGitOpFailed, err, out)
		}
		if attempt == maxRetries {
			break
		}
		// Reconcile with the concurrent writer. Entry files are uniquely named
		// (slug+uuid) so they never collide on rebase; the only file both sides
		// touch is the derived MEMORY.md. --strategy-option=theirs auto-resolves
		// that add/add without stopping the rebase (any pick is fine — the index
		// is regenerated deterministically from the merged entry set right
		// after). This is the §9 "last-write 收" policy for a genuinely edited
		// same entry, and lossless for the common append case.
		if out, rErr := s.runner.Run(ctx, s.dir, env,
			"-c", "rebase.autoStash=true", "pull", "--rebase",
			"--strategy-option=theirs", remote, branch); rErr != nil {
			return fmt.Errorf("%w: pull --rebase: %v: %s", ErrGitOpFailed, rErr, out)
		}
		if err := s.RegenerateIndex(); err != nil {
			return err
		}
		if err := s.Commit(ctx, author, message+" (reindex after rebase)"); err != nil {
			return err
		}
	}
	return ErrPushRetriesExhausted
}

func (s *Store) requireAuthor(a Author) error {
	if a.Name == "" || a.Email == "" {
		return errors.New("centergit: author name + email required")
	}
	return nil
}

// isNonFastForward detects a rejected push that a pull --rebase can resolve.
func isNonFastForward(out string) bool {
	lo := strings.ToLower(out)
	return strings.Contains(lo, "non-fast-forward") ||
		strings.Contains(lo, "fetch first") ||
		strings.Contains(lo, "updates were rejected") ||
		(strings.Contains(lo, "rejected") && strings.Contains(lo, "push"))
}
