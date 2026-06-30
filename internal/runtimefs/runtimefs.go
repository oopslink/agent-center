// Package runtimefs is the shared protocol + in-process correlator for the agent
// runtime file browser (issue-921db054 / I5). The Web Console reads an agent's
// runtime data (memory/, workspace, events, …) over the EXISTING Center↔Worker
// control-channel: there is NO reverse RPC into a worker, so a read is a REQUEST
// command (down the control-loop) plus a correlated feedback RESPONSE (up the
// feedback POST). This package defines:
//
//   - the wire types both ends agree on (Command down, Response up, + the FE-facing
//     result DTOs the worker builds and the Center passes through verbatim), and
//   - Dispatcher, the Center-process-global req_id→response correlator that lets a
//     Web Console handler block on the worker's reply (with a timeout).
//
// It is deliberately dependency-free (only stdlib) so BOTH the worker daemon and the
// two Center HTTP servers (webconsole + admin) can import it without cycles.
package runtimefs

import (
	"encoding/json"
	"sync"
)

// CommandType is the control-loop command type for a runtime-fs read request — the
// single string both the Center (enqueue) and the worker (Handle dispatch) key on.
const CommandType = "agent.runtime_fs"

// Operation codes carried in a Command.Op.
const (
	OpList    = "list"
	OpRead    = "read"
	OpGitLog  = "gitlog"
	OpGitDiff = "gitdiff"
)

// Error codes a worker may return when an op cannot be served. The Web Console
// handler maps them to HTTP status; offline/timeout are NOT errors here (they are
// the Center's own {unavailable} result when no worker reply arrives).
const (
	ErrCodePathEscape = "path_escape" // userPath resolved outside the agent home → 403
	ErrCodeNotFound   = "not_found"   // path does not exist → 404
	ErrCodeNotDir     = "not_a_dir"   // list target is not a directory → 400
	ErrCodeNotFile    = "not_a_file"  // read target is not a regular file → 400
	ErrCodeInternal   = "internal"    // unexpected worker-side failure → 500
)

// Command is the center→worker request payload, JSON-encoded into the control
// command's Payload. ReqID correlates the eventual Response. AgentID is the
// execution-entity id whose home the op runs in (the worker re-verifies ownership).
type Command struct {
	ReqID   string `json:"req_id"`
	AgentID string `json:"agent_id"`
	Op      string `json:"op"`
	Path    string `json:"path"`
	Limit   int    `json:"limit,omitempty"`
	// Ref is the commit SHA the gitdiff op renders (ignored by the other ops).
	Ref string `json:"ref,omitempty"`
}

// Response is the worker→center reply, POSTed to the admin feedback endpoint and
// matched to a pending request by ReqID. On success Result holds the op's FE-facing
// DTO (ListResult / ReadResult / GitLogResult) as raw JSON the Center passes through;
// on failure Code (+ Message) is set and Result is empty.
type Response struct {
	AgentID string          `json:"agent_id"`
	ReqID   string          `json:"req_id"`
	Result  json.RawMessage `json:"result,omitempty"`
	Code    string          `json:"code,omitempty"`
	Message string          `json:"message,omitempty"`
}

// ── FE-facing result DTOs (worker builds, Center passes through verbatim) ──────────
// Shapes are the BE↔FE contract (agreed with the FE owner): the Center adds nothing,
// so the worker is the single source of the rendered shape.

// Entry is one directory child in a ListResult.
type Entry struct {
	Name string `json:"name"`
	Path string `json:"path"` // relative to the agent home, forward-slashed
	Type string `json:"type"` // "file" | "directory"
	Size int64  `json:"size"`
	// Mtime is RFC3339; a directory's size is its own (often 0).
	Mtime string `json:"mtime"`
	// Sensitive flags a credential file (mcp_config.runtime.json) or a special file
	// (*.sock / *.lock) whose CONTENT the read op will withhold. Omitted when false.
	Sensitive bool `json:"sensitive,omitempty"`
}

// ListResult is the `list` op DTO.
type ListResult struct {
	Path      string  `json:"path"`
	Type      string  `json:"type"` // always "directory"
	Entries   []Entry `json:"entries"`
	Truncated bool    `json:"truncated"`
}

// ReadResult is the `read` op DTO. Content is nil (JSON null) for a binary or a
// redacted (credential / special) file — only metadata is returned in those cases.
//
// Image files are an exception to the binary rule: a previewable image (png/jpeg/
// gif/webp) under the image cap is returned with Image=true and Content holding the
// base64-encoded bytes (Encoding="base64") so the FE can render it inline. An image
// over the cap falls back to Binary=true / Content=nil (metadata only).
type ReadResult struct {
	Path        string `json:"path"`
	Type        string `json:"type"` // always "file"
	Size        int64  `json:"size"`
	Mtime       string `json:"mtime"`
	ContentType string `json:"content_type"`
	Binary      bool   `json:"binary"`
	Redacted    bool   `json:"redacted,omitempty"`
	// Image marks a base64-previewable image; Encoding is "base64" when Content is
	// not plain UTF-8 text (i.e. an image). Both are omitted for ordinary text files.
	Image     bool    `json:"image,omitempty"`
	Encoding  string  `json:"encoding,omitempty"`
	Truncated bool    `json:"truncated"`
	Content   *string `json:"content"`
}

// Commit is one entry in a GitLogResult.
type Commit struct {
	SHA     string `json:"sha"`
	Message string `json:"message"`
	Author  string `json:"author"`
	Date    string `json:"date"` // RFC3339 (author date)
}

// GitLogResult is the `gitlog` op DTO (read-only memory/ history).
type GitLogResult struct {
	Commits   []Commit `json:"commits"`
	Truncated bool     `json:"truncated"`
}

// GitDiffResult is the `gitdiff` op DTO — the unified diff of a single commit
// (`git show <sha>`) in the memory repo. Diff is capped; Truncated marks a cut.
type GitDiffResult struct {
	SHA       string `json:"sha"`
	Diff      string `json:"diff"`
	Truncated bool   `json:"truncated"`
}

// Dispatcher is the Center-process-global correlator between a Web Console read
// request and the worker's feedback reply. A webconsole handler Registers a req_id
// (getting a one-shot channel), enqueues the control command, then blocks on the
// channel until either the admin feedback endpoint Resolves it or the handler times
// out. It is safe for concurrent use.
type Dispatcher struct {
	mu      sync.Mutex
	pending map[string]chan Response
}

// NewDispatcher builds an empty correlator.
func NewDispatcher() *Dispatcher {
	return &Dispatcher{pending: make(map[string]chan Response)}
}

// Register creates a pending slot for reqID and returns a receive-only channel that
// will get exactly one Response (when the worker replies) plus a release func the
// caller MUST defer to free the slot (on timeout / context-cancel / after receive).
// The channel is buffered (cap 1) so Resolve never blocks even if the waiter already
// left.
func (d *Dispatcher) Register(reqID string) (<-chan Response, func()) {
	ch := make(chan Response, 1)
	d.mu.Lock()
	d.pending[reqID] = ch
	d.mu.Unlock()
	return ch, func() {
		d.mu.Lock()
		delete(d.pending, reqID)
		d.mu.Unlock()
	}
}

// Resolve delivers resp to the waiter registered for resp.ReqID. It returns true
// when a waiter was found (and removes the slot), false when none is pending — an
// unknown / already-timed-out / duplicate reply, which the caller can safely ignore.
// Never blocks (the per-request channel is buffered).
func (d *Dispatcher) Resolve(resp Response) bool {
	d.mu.Lock()
	ch, ok := d.pending[resp.ReqID]
	if ok {
		delete(d.pending, resp.ReqID)
	}
	d.mu.Unlock()
	if !ok {
		return false
	}
	ch <- resp
	return true
}
