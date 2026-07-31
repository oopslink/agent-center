package memory

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strconv"
	"strings"
)

const (
	defaultHarnessMemoryBudgetBytes = 24 * 1024
	defaultHarnessPerFileBytes      = 8 * 1024
	defaultHarnessOmittedBytes      = 4 * 1024
	defaultHarnessOmittedEntries    = 80

	envHarnessMemoryBudgetBytes = "AGENT_CENTER_MEMORY_BUDGET_BYTES"
	envHarnessPerFileBytes      = "AGENT_CENTER_MEMORY_PER_FILE_BYTES"
	envHarnessOmittedBytes      = "AGENT_CENTER_MEMORY_OMITTED_BYTES"
	envHarnessOmittedEntries    = "AGENT_CENTER_MEMORY_OMITTED_ENTRIES"
)

// Engine is the runtime façade the agent CLI uses to manage its scoped memory:
// init the repo at startup, assemble scoped context for prompt injection
// (ancestor walk), write a scope's memory (commit), and sync dirty working-tree
// edits into commit history. It wires a GitOps + SkeletonFactory over a single
// memoryDir (the per-agent <home>/memory).
//
// W2 boundary: local commit only. Pushing the repo to the Center remote (W1) is
// deliberately out of scope here — Engine never calls GitOps.Push.
type Engine struct {
	memoryDir string
	gitops    *GitOps
	factory   *SkeletonFactory
}

// NewEngine wires an Engine over memoryDir. homeOverride pins git's HOME so the
// operator's ~/.gitconfig (gpgsign/hooks) cannot pollute memory commits; when
// empty, GitOps falls back to HOME=memoryDir (still isolated from the dev
// machine). Pass memoryDir == <agent home>/memory.
func NewEngine(memoryDir, homeOverride string) *Engine {
	g := NewGitOps(memoryDir, nil, homeOverride)
	return &Engine{
		memoryDir: memoryDir,
		gitops:    g,
		factory:   NewSkeletonFactory(memoryDir, g),
	}
}

// MemoryDir returns the absolute memory directory the engine manages.
func (e *Engine) MemoryDir() string { return e.memoryDir }

// GitLog returns the repo's `git log --oneline` (all refs) — used by tests and
// inspection to verify commit history. Empty repo (no commits) yields "".
func (e *Engine) GitLog(ctx context.Context) (string, error) {
	return e.gitops.LogOneline(ctx)
}

// EnsureRootInit makes memoryDir a git repo seeded with the global MEMORY.md +
// supervisor.md skeletons. Idempotent — safe to call at every agent CLI startup.
func (e *Engine) EnsureRootInit(ctx context.Context) error {
	return e.factory.EnsureRootInit(ctx)
}

// AncestorScopes returns the scopes to consult for `scope`, ordered BROADEST →
// NARROWEST (global first, the scope itself last). Precedence runs the other
// way: the narrower a scope, the higher its priority, so when rendered into a
// prompt the most specific guidance is read last and overrides the general.
// supervisor.md is NOT part of any walk (cognition/02 §1: it is loaded
// explicitly) — use AssembleScoped's includeSupervisor flag for it.
//
// Team memory (design §3/§5): when the scope carries a TeamID, the agent's
// team-shared memory is inserted as a layer BROADER than project but narrower
// than global — rendered as global → team → project → (task/issue). Because the
// chain is broadest→narrowest and the narrowest wins, this makes project facts
// override team conventions (design §3: "项目具体事实盖团队通用规约"), while team
// conventions still override the platform-wide global scope. The agent's own
// task/issue notes stay most specific.
func AncestorScopes(scope MemoryScope) []MemoryScope {
	global := MemoryScope{Kind: MemScopeGlobal}
	teamLayer := func() []MemoryScope {
		if scope.TeamID == "" {
			return nil
		}
		return []MemoryScope{{Kind: MemScopeTeam, TeamID: scope.TeamID}}
	}
	switch scope.Kind {
	case MemScopeTeam:
		return []MemoryScope{global, scope}
	case MemScopeProject:
		chain := []MemoryScope{global}
		chain = append(chain, teamLayer()...)
		return append(chain, scope)
	case MemScopeTask, MemScopeIssue:
		chain := []MemoryScope{global}
		chain = append(chain, teamLayer()...)
		chain = append(chain, MemoryScope{Kind: MemScopeProject, ProjectID: scope.ProjectID})
		return append(chain, scope)
	case MemScopeConversation, MemScopeWorker:
		chain := []MemoryScope{global}
		chain = append(chain, teamLayer()...)
		return append(chain, scope)
	default:
		// global / supervisor / unknown → just the global root.
		return []MemoryScope{global}
	}
}

// AssembleScoped reads every existing memory file along scope's ancestor chain
// (missing files are skipped) and renders them into one injectable block,
// ordered broadest → narrowest. When includeSupervisor is set, supervisor.md
// (if present) is appended last as the supervisor's self-memory. Returns "" when
// no memory file exists anywhere on the chain, so callers can cheaply skip
// injection.
func (e *Engine) AssembleScoped(ctx context.Context, scope MemoryScope, includeSupervisor bool) (string, error) {
	chain := AncestorScopes(scope)
	if includeSupervisor {
		chain = append(chain, MemoryScope{Kind: MemScopeSupervisor})
	}
	var b strings.Builder
	n := 0
	for _, s := range chain {
		body, err := e.readScope(s)
		if err != nil {
			return "", err
		}
		if strings.TrimSpace(body) == "" {
			continue
		}
		if n == 0 {
			b.WriteString("<agent-memory>\n")
		}
		fmt.Fprintf(&b, "## scope: %s\n%s\n\n", scopeLabel(s), strings.TrimRight(body, "\n"))
		n++
	}
	if n == 0 {
		return "", nil
	}
	b.WriteString("</agent-memory>\n")
	return b.String(), nil
}

// HarnessContext renders the memory block injected into the agent CLI's
// append-system-prompt harness at launch: a short guide to the on-disk scoped
// layout (so the agent reads/writes the right MEMORY.md via its own file tools)
// plus the always-relevant global + supervisor memory bodies. The runtime
// commits the agent's edits automatically (see CommitDirty), so the guide tells
// the agent to just edit the matching file. Never returns an error for a missing
// file (an empty repo yields the guide with no bodies).
func (e *Engine) HarnessContext(ctx context.Context) (string, error) {
	body, omitted, err := e.AssembleHarnessContext(ctx, HarnessDisclosureOptions{})
	if err != nil {
		return "", err
	}
	return e.renderHarnessContext(body, omitted), nil
}

// HarnessContextWithOptions is the observable variant used by runtimes. It keeps
// HarnessContext's rendered prompt stable while returning budget/manifest stats
// for diagnostics.
func (e *Engine) HarnessContextWithOptions(ctx context.Context, opt HarnessDisclosureOptions) (string, HarnessContextStats, error) {
	res, err := e.AssembleHarnessContextDetailed(ctx, opt)
	if err != nil {
		return "", HarnessContextStats{}, err
	}
	return e.renderHarnessContext(res.Body, res.OmittedManifest), res.Stats, nil
}

func (e *Engine) renderHarnessContext(body, omitted string) string {
	var b strings.Builder
	b.WriteString("== Your memory ==\n")
	fmt.Fprintf(&b, "Your persistent memory is a git repo at %s — markdown, organised by scope:\n", e.memoryDir)
	b.WriteString("  MEMORY.md                                          global (all your work)\n")
	b.WriteString("  supervisor.md                                      your self-memory\n")
	b.WriteString("  projects/<project_id>/MEMORY.md                     project scope\n")
	b.WriteString("  projects/<project_id>/tasks/<task_id>/MEMORY.md     task scope\n")
	b.WriteString("  projects/<project_id>/issues/<issue_id>/MEMORY.md   issue scope\n")
	b.WriteString("  conversations/<conversation_id>/MEMORY.md           conversation scope\n")
	b.WriteString("Startup memory below is intentionally budgeted. Treat MEMORY.md as the index, read omitted paths only when they are relevant, and consult the ancestor chain narrow→broad when a task needs scoped detail. Record durable lessons in the most specific matching MEMORY.md. Never write outside this directory.\n")
	if body != "" {
		b.WriteString("\nProgressive startup memory:\n")
		b.WriteString(body)
	}
	if omitted != "" {
		b.WriteString("\nMemory available on demand:\n")
		b.WriteString(omitted)
	}
	return b.String()
}

// HarnessDisclosureOptions controls how much memory is disclosed at supervisor
// startup. Zero values use conservative defaults; tests may lower them.
type HarnessDisclosureOptions struct {
	MemoryBudgetBytes int
	PerFileBytes      int
	OmittedBytes      int
	OmittedEntries    int
}

// HarnessDisclosureOptionsFromEnv reads startup-memory budget knobs. Invalid or
// non-positive values are ignored so a bad environment falls back to safe defaults.
func HarnessDisclosureOptionsFromEnv() HarnessDisclosureOptions {
	return HarnessDisclosureOptions{
		MemoryBudgetBytes: positiveEnvInt(envHarnessMemoryBudgetBytes),
		PerFileBytes:      positiveEnvInt(envHarnessPerFileBytes),
		OmittedBytes:      positiveEnvInt(envHarnessOmittedBytes),
		OmittedEntries:    positiveEnvInt(envHarnessOmittedEntries),
	}
}

// HarnessContextResult is the lower-level assembly output before the surrounding
// "Your memory" guide is rendered.
type HarnessContextResult struct {
	Body            string
	OmittedManifest string
	Stats           HarnessContextStats
}

// HarnessContextStats is intentionally content-free: it is safe for runtime
// diagnostics and lets operators see whether startup memory was clipped without
// leaking memory text into activity payloads.
type HarnessContextStats struct {
	MemoryBudgetBytes      int
	PerFileBytes           int
	OmittedBytes           int
	OmittedEntries         int
	IncludedFiles          int
	TruncatedFiles         int
	OmittedFiles           int
	BodyBytes              int
	OmittedManifestBytes   int
	OmittedManifestClipped bool
}

// AssembleHarnessContext returns the bounded startup memory excerpt plus an
// omitted-path manifest. It keeps HarnessContext's historical global+supervisor
// compatibility, but turns those bodies into budgeted excerpts and lists other
// markdown files for explicit on-demand reads.
func (e *Engine) AssembleHarnessContext(ctx context.Context, opt HarnessDisclosureOptions) (string, string, error) {
	res, err := e.AssembleHarnessContextDetailed(ctx, opt)
	if err != nil {
		return "", "", err
	}
	return res.Body, res.OmittedManifest, nil
}

// AssembleHarnessContextDetailed returns the bounded startup memory excerpt,
// omitted-path manifest, and diagnostic stats.
func (e *Engine) AssembleHarnessContextDetailed(ctx context.Context, opt HarnessDisclosureOptions) (HarnessContextResult, error) {
	if err := ctx.Err(); err != nil {
		return HarnessContextResult{}, err
	}
	budget := opt.MemoryBudgetBytes
	if budget <= 0 {
		budget = defaultHarnessMemoryBudgetBytes
	}
	perFile := opt.PerFileBytes
	if perFile <= 0 {
		perFile = defaultHarnessPerFileBytes
	}
	omittedLimit := opt.OmittedBytes
	if omittedLimit <= 0 {
		omittedLimit = defaultHarnessOmittedBytes
	}
	omittedEntries := opt.OmittedEntries
	if omittedEntries <= 0 {
		omittedEntries = defaultHarnessOmittedEntries
	}

	candidates := []MemoryScope{{Kind: MemScopeGlobal}, {Kind: MemScopeSupervisor}}
	var body strings.Builder
	var omitted []string
	used := 0
	included := 0
	truncatedFiles := 0
	for _, scope := range candidates {
		rel, err := ScopeToFSPath(scope)
		if err != nil {
			return HarnessContextResult{}, err
		}
		raw, err := e.readScope(scope)
		if err != nil {
			return HarnessContextResult{}, err
		}
		text := strings.TrimSpace(sanitizeHarnessMemory(raw))
		if text == "" {
			continue
		}
		excerpt, truncated := boundedText(text, perFile)
		block := fmt.Sprintf("## %s (%s)\n%s\n\n", scopeLabel(scope), rel, strings.TrimRight(excerpt, "\n"))
		if used+len(block) > budget {
			omitted = append(omitted, rel+" (startup budget exhausted)")
			continue
		}
		if included == 0 {
			body.WriteString("<agent-memory progressive=\"true\">\n")
		}
		body.WriteString(block)
		used += len(block)
		included++
		if truncated {
			omitted = append(omitted, rel+" (excerpt truncated)")
			truncatedFiles++
		}
	}
	if included > 0 {
		body.WriteString("</agent-memory>\n")
	}

	discovered, err := e.discoverMarkdownMemoryFiles(ctx)
	if err != nil {
		return HarnessContextResult{}, err
	}
	seen := map[string]struct{}{
		"MEMORY.md":     {},
		"supervisor.md": {},
	}
	for _, rel := range discovered {
		if _, ok := seen[rel]; ok {
			continue
		}
		omitted = append(omitted, rel)
	}
	manifest, clipped := formatOmittedMemory(omitted, omittedLimit, omittedEntries)
	stats := HarnessContextStats{
		MemoryBudgetBytes:      budget,
		PerFileBytes:           perFile,
		OmittedBytes:           omittedLimit,
		OmittedEntries:         omittedEntries,
		IncludedFiles:          included,
		TruncatedFiles:         truncatedFiles,
		OmittedFiles:           len(omitted),
		BodyBytes:              body.Len(),
		OmittedManifestBytes:   len(manifest),
		OmittedManifestClipped: clipped,
	}
	return HarnessContextResult{Body: body.String(), OmittedManifest: manifest, Stats: stats}, nil
}

func positiveEnvInt(name string) int {
	raw := strings.TrimSpace(os.Getenv(name))
	if raw == "" {
		return 0
	}
	n, err := strconv.Atoi(raw)
	if err != nil || n <= 0 {
		return 0
	}
	return n
}

func sanitizeHarnessMemory(in string) string {
	var out []string
	for _, line := range strings.Split(in, "\n") {
		if isDangerousCenterBypassMemoryLine(line) {
			continue
		}
		out = append(out, line)
	}
	return strings.Join(out, "\n")
}

func isDangerousCenterBypassMemoryLine(line string) bool {
	l := strings.ToLower(line)
	needles := []string{
		"admin-socket",
		"admin socket",
		"admin http",
		"admin-tools",
		"sqlite",
		"agent-center.db",
		"worker token",
		"process args",
		"mcp_config.runtime.json",
		"bypass mcp",
		"mcp fallback",
		"mcp 兜底",
		"绕过 mcp",
	}
	for _, needle := range needles {
		if strings.Contains(l, needle) {
			return true
		}
	}
	return false
}

func boundedText(s string, limit int) (string, bool) {
	if limit <= 0 || len(s) <= limit {
		return s, false
	}
	cut := strings.LastIndexByte(s[:limit], '\n')
	if cut < limit/2 {
		cut = limit
	}
	return strings.TrimRight(s[:cut], "\n") + "\n[truncated; read file for full memory]", true
}

func (e *Engine) discoverMarkdownMemoryFiles(ctx context.Context) ([]string, error) {
	var out []string
	err := filepath.WalkDir(e.memoryDir, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if err := ctx.Err(); err != nil {
			return err
		}
		name := d.Name()
		if d.IsDir() {
			if name == ".git" {
				return filepath.SkipDir
			}
			return nil
		}
		if !strings.HasSuffix(strings.ToLower(name), ".md") {
			return nil
		}
		rel, err := filepath.Rel(e.memoryDir, path)
		if err != nil {
			return err
		}
		out = append(out, filepath.ToSlash(rel))
		return nil
	})
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	return out, err
}

func formatOmittedMemory(paths []string, byteLimit, entryLimit int) (string, bool) {
	if len(paths) == 0 {
		return "", false
	}
	var b strings.Builder
	clipped := false
	written := 0
	for _, p := range paths {
		if entryLimit > 0 && written >= entryLimit {
			clipped = true
			break
		}
		line := "- " + p + "\n"
		if byteLimit > 0 && b.Len()+len(line) > byteLimit {
			clipped = true
			break
		}
		b.WriteString(line)
		written++
	}
	if clipped {
		remaining := len(paths) - written
		suffix := "- ...\n"
		if remaining > 0 {
			suffix = fmt.Sprintf("- ... (%d more)\n", remaining)
		}
		if byteLimit <= 0 || b.Len()+len(suffix) <= byteLimit {
			b.WriteString(suffix)
		}
	}
	return b.String(), clipped
}

// WriteScoped writes content as the memory file for scope (mkdir -p) and commits
// it via GitOps under the given author. The path is containment-guarded
// (AbsPath blocks lexical "../" traversal; a runtime symlink-escape check blocks
// a directory/file under memory/ that links outside it). An empty author or
// message falls back to a system identity / default message.
func (e *Engine) WriteScoped(ctx context.Context, scope MemoryScope, content, authorName, authorEmail, message string) error {
	abs, err := e.containedAbs(scope)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(abs), 0o755); err != nil {
		return fmt.Errorf("memory: mkdir: %w", err)
	}
	if err := os.WriteFile(abs, []byte(content), 0o644); err != nil {
		return fmt.Errorf("memory: write: %w", err)
	}
	rel, err := ScopeToFSPath(scope)
	if err != nil {
		return err
	}
	if authorName == "" || authorEmail == "" {
		authorName, authorEmail = "system:bootstrap", "system:bootstrap@agent-center.local"
	}
	if message == "" {
		message = "update: memory for " + scopeLabel(scope)
	}
	return e.gitops.CommitFile(ctx, rel, authorName, authorEmail, message)
}

// CommitDirty stages and commits any dirty working-tree changes under memoryDir.
// The agent edits MEMORY.md files directly via its file tools; this is the
// "memory sync" that turns those edits into commit history. No-op on a clean
// tree. W2 scope: LOCAL commit only — remote push (W1) is out.
func (e *Engine) CommitDirty(ctx context.Context, authorName, authorEmail, message string) error {
	if authorName == "" || authorEmail == "" {
		authorName, authorEmail = "system:memory-sync", "system:memory-sync@agent-center.local"
	}
	if message == "" {
		message = "memory: sync working tree"
	}
	return e.gitops.AutoCommitDirty(ctx, authorName, authorEmail, message)
}

// readScope returns the body of scope's memory file, or "" if it does not exist.
// It goes through the same containment + symlink-escape guard as writes, so a
// memory tree poisoned with a symlink pointing outside memoryDir is refused
// rather than silently read.
func (e *Engine) readScope(scope MemoryScope) (string, error) {
	abs, err := e.containedAbs(scope)
	if err != nil {
		return "", err
	}
	data, err := os.ReadFile(abs)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return "", nil
		}
		return "", fmt.Errorf("memory: read %s: %w", scopeLabel(scope), err)
	}
	return string(data), nil
}

// containedAbs resolves scope's absolute path and guards against escape:
// AbsPath blocks lexical "../" traversal, and guardSymlinkEscape blocks a path
// whose deepest existing ancestor resolves (via symlinks) outside memoryDir.
func (e *Engine) containedAbs(scope MemoryScope) (string, error) {
	abs, err := AbsPath(e.memoryDir, scope)
	if err != nil {
		return "", err
	}
	if err := guardSymlinkEscape(e.memoryDir, abs); err != nil {
		return "", err
	}
	return abs, nil
}

// ErrMemoryPathEscapes is returned when a memory path resolves outside memoryDir
// through a symlink (the lexical guard lives in AbsPath; this is the runtime fs
// guard). Use errors.Is to test.
var ErrMemoryPathEscapes = errors.New("memory: path escapes memoryDir")

// guardSymlinkEscape walks from abs up to its deepest EXISTING ancestor,
// resolves that ancestor through symlinks, and refuses if the resolved real path
// is no longer inside memoryDir. This catches the case AbsPath cannot: a real
// directory (or the target file itself) under memory/ that is a symlink to
// somewhere outside the containment root. A not-yet-existing path is fine — its
// nearest existing parent is what gets checked.
func guardSymlinkEscape(memoryDir, abs string) error {
	realRoot, err := filepath.EvalSymlinks(memoryDir)
	if err != nil {
		// memoryDir may not exist at the very first init; fall back to its clean
		// lexical form for the prefix comparison.
		realRoot = filepath.Clean(memoryDir)
	}
	probe := filepath.Clean(abs)
	for {
		if _, statErr := os.Lstat(probe); statErr == nil {
			real, evalErr := filepath.EvalSymlinks(probe)
			if evalErr != nil {
				return fmt.Errorf("memory: resolve %q: %w", probe, evalErr)
			}
			real = filepath.Clean(real)
			if real != realRoot && !strings.HasPrefix(real, realRoot+string(filepath.Separator)) {
				return fmt.Errorf("%w: %q -> %q", ErrMemoryPathEscapes, abs, real)
			}
			return nil
		}
		parent := filepath.Dir(probe)
		if parent == probe {
			return nil // reached filesystem root without finding an existing ancestor
		}
		probe = parent
	}
}
