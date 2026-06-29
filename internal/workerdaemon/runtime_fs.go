package workerdaemon

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"os"
	"os/exec"
	"path"
	"path/filepath"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/oopslink/agent-center/internal/runtimefs"
)

// runtime_fs.go (issue-921db054 / I5) — the WORKER side of the agent runtime file
// browser. The Center sends an `agent.runtime_fs` command down the control-loop; this
// runs the read-only op (list / read / gitlog) INSIDE the agent's home, builds the
// FE-facing DTO (or an error code), and POSTs the correlated response back up the
// feedback channel. Every op is read-only and hard-bounded; the security red lines
// are enforced HERE (the Center is pure transport):
//
//   - PATH SAFETY: every user path goes through resolveContainedPath (EvalSymlinks +
//     pathWithinRoot, file_transfer.go) so `..` / absolute / symlink escape is
//     rejected before any open.
//   - `.git/` is NEVER listed and never read (hidden — it is the memory repo plumbing).
//   - mcp_config.runtime.json (plaintext credentials, 0600) is listed but its CONTENT
//     is WITHHELD (redacted) — plaintext creds never leave the worker.
//   - *.sock / *.lock / non-regular / binary files return metadata only (content null).
//   - limits: ≤1000 dir entries, ≤1MB file preview, ≤200 git-log commits (truncated).

const cmdTypeRuntimeFs = runtimefs.CommandType

const (
	runtimeFsMaxEntries  = 1000
	runtimeFsMaxFileSize = 1 << 20 // 1 MiB preview cap
	runtimeFsGitLogDef   = 50
	runtimeFsGitLogMax   = 200
)

// runtimeFsError carries a structured op failure (a contract error code + message).
type runtimeFsError struct {
	code string
	msg  string
}

// runtimeFs handles one agent.runtime_fs command: resolve the agent home, run the op,
// and POST the correlated Response. It NEVER returns a control-loop error (the read is
// fire-and-forget — a failed POST is logged, and the Center degrades to {unavailable}
// on its own timeout), so a transient feedback failure never wedges the command
// cursor.
func (c *AgentController) runtimeFs(ctx context.Context, pl runtimefs.Command) error {
	resp := runtimefs.Response{AgentID: pl.AgentID, ReqID: pl.ReqID}

	home, _, _, err := c.agentPaths(pl.AgentID)
	if err != nil {
		// agent not hosted here / bad config — report an error result (the Center
		// maps it; it will not leak another agent's data because home never resolved).
		resp.Code, resp.Message = runtimefs.ErrCodeInternal, err.Error()
		c.postRuntimeFs(ctx, resp)
		return nil
	}

	var (
		result any
		opErr  *runtimeFsError
	)
	switch pl.Op {
	case runtimefs.OpList:
		result, opErr = runtimeFsList(home, pl.Path)
	case runtimefs.OpRead:
		result, opErr = runtimeFsRead(home, pl.Path)
	case runtimefs.OpGitLog:
		result, opErr = runtimeFsGitLog(ctx, home, pl.Path, pl.Limit)
	default:
		opErr = &runtimeFsError{runtimefs.ErrCodeInternal, "unknown op " + pl.Op}
	}

	switch {
	case opErr != nil:
		resp.Code, resp.Message = opErr.code, opErr.msg
	default:
		raw, merr := json.Marshal(result)
		if merr != nil {
			resp.Code, resp.Message = runtimefs.ErrCodeInternal, merr.Error()
		} else {
			resp.Result = raw
		}
	}
	c.postRuntimeFs(ctx, resp)
	return nil
}

// postRuntimeFs POSTs the response best-effort (logs failures).
func (c *AgentController) postRuntimeFs(ctx context.Context, resp runtimefs.Response) {
	if err := c.cfg.Reporter.ReportRuntimeFsResponse(ctx, resp); err != nil {
		c.log("runtime_fs response post (req=%s op-agent=%s): %v", resp.ReqID, resp.AgentID, err)
	}
}

// ── ops ────────────────────────────────────────────────────────────────────────────

// runtimeFsList lists a directory under the agent home. The agent home root is the
// empty / "." path. `.git` is skipped; entries are capped at runtimeFsMaxEntries.
func runtimeFsList(home, userPath string) (*runtimefs.ListResult, *runtimeFsError) {
	rel := runtimeFsCleanRel(userPath)
	dir, err := resolveContainedPath(home, runtimeFsResolveArg(rel), true)
	if err != nil {
		return nil, classifyPathErr(err)
	}
	// Red line, checked on the RESOLVED path (so a symlink that lands inside .git is
	// caught too): never expose the git plumbing dir, even when it is the target.
	if runtimeFsResolvedInGit(home, dir) {
		return nil, &runtimeFsError{runtimefs.ErrCodeNotFound, "not found"}
	}
	info, err := os.Stat(dir)
	if err != nil {
		return nil, classifyStatErr(err)
	}
	if !info.IsDir() {
		return nil, &runtimeFsError{runtimefs.ErrCodeNotDir, "not a directory"}
	}
	ents, err := os.ReadDir(dir)
	if err != nil {
		return nil, &runtimeFsError{runtimefs.ErrCodeInternal, err.Error()}
	}
	res := &runtimefs.ListResult{Path: rel, Type: "directory", Entries: []runtimefs.Entry{}}
	for _, e := range ents {
		name := e.Name()
		if name == ".git" {
			continue // red line: the memory git dir is never listed
		}
		if len(res.Entries) >= runtimeFsMaxEntries {
			res.Truncated = true
			break
		}
		fi, ierr := e.Info()
		if ierr != nil {
			continue // raced unlink — skip rather than fail the whole listing
		}
		etype := "file"
		if e.IsDir() {
			etype = "directory"
		}
		res.Entries = append(res.Entries, runtimefs.Entry{
			Name:      name,
			Path:      path.Join(rel, name),
			Type:      etype,
			Size:      fi.Size(),
			Mtime:     fi.ModTime().UTC().Format(time.RFC3339),
			Sensitive: runtimeFsSensitiveName(name),
		})
	}
	return res, nil
}

// runtimeFsRead returns a file's metadata + (for previewable text) content. Credential
// files are redacted (content withheld); special / non-regular / binary files return
// metadata only; text is capped at runtimeFsMaxFileSize.
func runtimeFsRead(home, userPath string) (*runtimefs.ReadResult, *runtimeFsError) {
	rel := runtimeFsCleanRel(userPath)
	if rel == "" {
		return nil, &runtimeFsError{runtimefs.ErrCodeNotFile, "no file path"}
	}
	full, err := resolveContainedPath(home, rel, true)
	if err != nil {
		return nil, classifyPathErr(err)
	}
	// Red line, checked on the RESOLVED path so a symlink pointing INTO .git (which
	// stays inside the home, so it passes containment) is still hidden: never expose
	// the git plumbing dir's contents.
	if runtimeFsResolvedInGit(home, full) {
		return nil, &runtimeFsError{runtimefs.ErrCodeNotFound, "not found"}
	}
	info, err := os.Stat(full)
	if err != nil {
		return nil, classifyStatErr(err)
	}
	if info.IsDir() {
		return nil, &runtimeFsError{runtimefs.ErrCodeNotFile, "is a directory"}
	}
	res := &runtimefs.ReadResult{
		Path:  rel,
		Type:  "file",
		Size:  info.Size(),
		Mtime: info.ModTime().UTC().Format(time.RFC3339),
	}
	base := filepath.Base(full)

	// Credential file → redacted: list it, but its plaintext content NEVER leaves the
	// worker (the red line). Matched by NAME *and* by inode identity, so a hardlink
	// alias (which EvalSymlinks does NOT resolve — a hardlink shares the inode, not a
	// symlink) under a different name can't smuggle the plaintext out.
	if runtimeFsIsCredential(home, base, info) {
		res.Redacted = true
		res.ContentType = "application/json"
		res.Content = nil
		return res, nil
	}
	// Special / non-regular (socket, lock, fifo, …) → metadata only.
	if !info.Mode().IsRegular() || runtimeFsSpecialName(base) {
		res.Binary = true
		res.ContentType = "application/octet-stream"
		res.Content = nil
		return res, nil
	}

	res.ContentType = sniffContentType(full)

	f, err := os.Open(full)
	if err != nil {
		return nil, classifyStatErr(err)
	}
	defer f.Close()
	// Read one byte past the cap to detect truncation without loading the whole file.
	data, err := io.ReadAll(io.LimitReader(f, runtimeFsMaxFileSize+1))
	if err != nil {
		return nil, &runtimeFsError{runtimefs.ErrCodeInternal, err.Error()}
	}
	if len(data) > runtimeFsMaxFileSize {
		res.Truncated = true
		data = data[:runtimeFsMaxFileSize]
	}
	if runtimeFsLooksBinary(data) {
		res.Binary = true
		res.Content = nil
	} else {
		s := string(data)
		res.Content = &s
	}
	return res, nil
}

// runtimeFsGitLog returns the read-only commit history of the agent's memory git repo
// (or any git repo under the home that userPath resolves to). A non-repo / empty repo
// yields an empty commit list (not an error). limit defaults to 50, capped at 200.
func runtimeFsGitLog(ctx context.Context, home, userPath string, limit int) (*runtimefs.GitLogResult, *runtimeFsError) {
	rel := runtimeFsCleanRel(userPath)
	if rel == "" {
		rel = "memory" // default target is the memory repo
	}
	dir, err := resolveContainedPath(home, rel, true)
	if err != nil {
		return nil, classifyPathErr(err)
	}
	if runtimeFsResolvedInGit(home, dir) {
		return nil, &runtimeFsError{runtimefs.ErrCodeNotFound, "not found"}
	}
	info, err := os.Stat(dir)
	if err != nil {
		return nil, classifyStatErr(err)
	}
	if !info.IsDir() {
		return nil, &runtimeFsError{runtimefs.ErrCodeNotDir, "not a directory"}
	}
	if limit <= 0 {
		limit = runtimeFsGitLogDef
	}
	if limit > runtimeFsGitLogMax {
		limit = runtimeFsGitLogMax
	}
	// Fetch one extra to detect truncation. Field-separate with US (0x1f), record-
	// separate with RS (0x1e) so a multi-line subject never breaks parsing.
	const fieldSep = "\x1f"
	const recSep = "\x1e"
	format := "--pretty=format:%H" + fieldSep + "%an" + fieldSep + "%aI" + fieldSep + "%s" + recSep
	out, runErr := runtimeFsGit(ctx, dir, "log", "--max-count", strconv.Itoa(limit+1), "--no-color", format)
	if runErr != nil {
		// Not a git repo / no commits yet → empty history (graceful, not an error).
		if strings.Contains(out, "not a git repository") ||
			strings.Contains(out, "does not have any commits yet") {
			return &runtimefs.GitLogResult{Commits: []runtimefs.Commit{}}, nil
		}
		return nil, &runtimeFsError{runtimefs.ErrCodeInternal, strings.TrimSpace(out)}
	}
	res := &runtimefs.GitLogResult{Commits: []runtimefs.Commit{}}
	for _, rec := range strings.Split(out, recSep) {
		rec = strings.Trim(rec, "\n")
		if strings.TrimSpace(rec) == "" {
			continue
		}
		if len(res.Commits) >= limit {
			res.Truncated = true
			break
		}
		parts := strings.SplitN(rec, fieldSep, 4)
		if len(parts) != 4 {
			continue
		}
		res.Commits = append(res.Commits, runtimefs.Commit{
			SHA: parts[0], Author: parts[1], Date: parts[2], Message: parts[3],
		})
	}
	return res, nil
}

// runtimeFsGit runs a read-only git command in dir with a hardened, config-free env
// (mirrors internal/cognition/memory's gitops env: no global/system config, no locks,
// no prompts, English output for stable error-string matching). Returns combined
// output + the exec error.
func runtimeFsGit(ctx context.Context, dir string, args ...string) (string, error) {
	cmd := exec.CommandContext(ctx, "git", args...)
	cmd.Dir = dir
	cmd.Env = []string{
		"GIT_TERMINAL_PROMPT=0",
		"GIT_OPTIONAL_LOCKS=0",
		"GIT_CONFIG_GLOBAL=/dev/null",
		"GIT_CONFIG_SYSTEM=/dev/null",
		"LANGUAGE=en",
		"LC_ALL=en_US.UTF-8",
		"HOME=" + dir,
		"PATH=/usr/local/bin:/usr/bin:/bin:/opt/homebrew/bin",
	}
	out, err := cmd.CombinedOutput()
	return string(out), err
}

// ── helpers ──────────────────────────────────────────────────────────────────────

// runtimeFsCleanRel normalises a user path to a clean home-relative path ("" = root).
// It strips a leading slash and Cleans; resolveContainedPath is the actual escape
// guard (this is just display/forms normalisation).
func runtimeFsCleanRel(p string) string {
	p = strings.TrimSpace(p)
	p = strings.TrimPrefix(p, "/")
	if p == "" {
		return ""
	}
	cleaned := path.Clean(p)
	if cleaned == "." {
		return ""
	}
	return cleaned
}

// runtimeFsResolveArg turns a clean rel path into the arg resolveContainedPath wants
// (it rejects an empty path, so the home root is passed as ".").
func runtimeFsResolveArg(rel string) string {
	if rel == "" {
		return "."
	}
	return rel
}

// runtimeFsResolvedInGit reports whether the RESOLVED path `full` (already contained
// in home by resolveContainedPath) has a `.git` segment anywhere under the home root —
// i.e. it is, or sits inside, a git plumbing dir. It is evaluated on the resolved path
// (not the user-supplied rel) so a symlink that dereferences INTO .git while staying
// within the home is still caught (the same "post-resolution" caliber as the
// filepath.Base(full) credential check). Fail-closed: if the path can't be related to
// home, treat it as hidden.
func runtimeFsResolvedInGit(home, full string) bool {
	evalHome, err := filepath.EvalSymlinks(home)
	if err != nil {
		evalHome = home
	}
	rel, err := filepath.Rel(evalHome, full)
	if err != nil {
		return true
	}
	for _, seg := range strings.Split(rel, string(os.PathSeparator)) {
		if seg == ".git" {
			return true
		}
	}
	return false
}

// runtimeFsCredentialName flags the plaintext-credential file whose content is withheld.
func runtimeFsCredentialName(base string) bool {
	return base == "mcp_config.runtime.json"
}

// runtimeFsIsCredential reports whether the resolved target is the plaintext-credential
// file — by NAME, or by INODE IDENTITY with the canonical <home>/mcp_config.runtime.json
// (os.SameFile). The inode check closes the hardlink-rename bypass: `ln
// mcp_config.runtime.json notes.txt` gives the secret a benign name that the name check
// alone would let through (a hardlink is the SAME inode, and EvalSymlinks — which only
// dereferences symlinks — does not normalise it back). The canonical file is JIT-created
// and may be absent; when it is, there is no alias to protect (return on name only).
func runtimeFsIsCredential(home, base string, info os.FileInfo) bool {
	if runtimeFsCredentialName(base) {
		return true
	}
	if info == nil {
		return false
	}
	credInfo, err := os.Stat(filepath.Join(home, "mcp_config.runtime.json"))
	if err != nil {
		return false // no canonical credential file → nothing to alias
	}
	return os.SameFile(info, credInfo)
}

// runtimeFsSpecialName flags special files served as metadata-only.
func runtimeFsSpecialName(base string) bool {
	return strings.HasSuffix(base, ".sock") || strings.HasSuffix(base, ".lock")
}

// runtimeFsSensitiveName flags entries the FE marks (redacted creds / lock / sock).
func runtimeFsSensitiveName(name string) bool {
	return runtimeFsCredentialName(name) || runtimeFsSpecialName(name)
}

// runtimeFsLooksBinary heuristically detects non-text content: a NUL byte or invalid
// UTF-8 means "binary" (content withheld, metadata only).
func runtimeFsLooksBinary(data []byte) bool {
	if len(data) == 0 {
		return false
	}
	if bytes.IndexByte(data, 0) >= 0 {
		return true
	}
	return !utf8.Valid(data)
}

// classifyPathErr maps a resolveContainedPath error to a contract code.
func classifyPathErr(err error) *runtimeFsError {
	if errors.Is(err, ErrPathEscapesWorkspace) {
		return &runtimeFsError{runtimefs.ErrCodePathEscape, "path escapes agent home"}
	}
	if errors.Is(err, os.ErrNotExist) {
		return &runtimeFsError{runtimefs.ErrCodeNotFound, "not found"}
	}
	return &runtimeFsError{runtimefs.ErrCodeInternal, err.Error()}
}

// classifyStatErr maps an os.Stat/Open error to a contract code.
func classifyStatErr(err error) *runtimeFsError {
	if errors.Is(err, os.ErrNotExist) {
		return &runtimeFsError{runtimefs.ErrCodeNotFound, "not found"}
	}
	return &runtimeFsError{runtimefs.ErrCodeInternal, err.Error()}
}
