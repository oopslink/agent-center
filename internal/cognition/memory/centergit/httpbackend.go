package centergit

import (
	"bytes"
	"errors"
	"fmt"
	"net/http"
	"net/http/cgi"
	"os/exec"
	"path/filepath"
	"strings"
)

// AgentResolver extracts the authenticated agent id from a request. In the
// admin API this is backed by the bearer-token auth middleware: resolve the
// worker token → the operating agent (requireAgentOnWorker) → agent.ID(). It
// returns ok=false when no agent identity is present (→ HTTP 401).
type AgentResolver func(*http.Request) (agentID string, ok bool)

// Handler is the center-hosted git smart-HTTP endpoint (§4.2/§4.3). Per request
// it: (1) resolves the caller's agent id, (2) parses the target RepoRef and
// whether the operation reads (upload-pack) or writes (receive-pack),
// (3) authorizes via Authorizer, then (4) bridges to git-http-backend over CGI.
//
// Mount it behind the admin auth+deps middleware, e.g. at "/admin/git/"; set
// MountPrefix to that value so the handler can recover the bare repo path.
type Handler struct {
	host        *Host
	authz       *Authorizer
	resolve     AgentResolver
	httpBackend string   // absolute path to the git-http-backend CGI
	execPath    string   // git core exec dir (for GIT_EXEC_PATH)
	mountPrefix string   // e.g. "/admin/git"; trimmed before path parsing
	extraEnv    []string // additional env passed to the CGI (tests/overrides)
}

// HandlerOption configures a Handler.
type HandlerOption func(*Handler)

// WithMountPrefix sets the URL prefix the handler is mounted at; it is trimmed
// from the request path before the repo path is parsed.
func WithMountPrefix(p string) HandlerOption {
	return func(h *Handler) { h.mountPrefix = "/" + strings.Trim(p, "/") }
}

// WithHTTPBackend overrides the git-http-backend binary path (tests / non-default
// git installs).
func WithHTTPBackend(path string) HandlerOption {
	return func(h *Handler) { h.httpBackend = path }
}

// WithExtraEnv appends env vars to every CGI invocation.
func WithExtraEnv(env ...string) HandlerOption {
	return func(h *Handler) { h.extraEnv = append(h.extraEnv, env...) }
}

// NewHandler wires a Handler. When the git-http-backend path is not overridden
// it is discovered via `git --exec-path`.
func NewHandler(host *Host, authz *Authorizer, resolve AgentResolver, opts ...HandlerOption) (*Handler, error) {
	if host == nil || authz == nil || resolve == nil {
		return nil, errors.New("centergit: NewHandler requires host, authz and resolve")
	}
	h := &Handler{host: host, authz: authz, resolve: resolve}
	for _, o := range opts {
		o(h)
	}
	if h.httpBackend == "" || h.execPath == "" {
		execDir, err := gitExecPath()
		if err != nil {
			return nil, err
		}
		if h.execPath == "" {
			h.execPath = execDir
		}
		if h.httpBackend == "" {
			h.httpBackend = filepath.Join(execDir, "git-http-backend")
		}
	}
	return h, nil
}

// gitExecPath returns git's core exec dir (`git --exec-path`).
func gitExecPath() (string, error) {
	cmd := exec.Command("git", "--exec-path")
	cmd.Env = baseGitEnv("", "", "")
	out, err := cmd.Output()
	if err != nil {
		return "", fmt.Errorf("%w: git --exec-path: %v", ErrGitOpFailed, err)
	}
	return strings.TrimSpace(string(out)), nil
}

// operationFor classifies the smart-HTTP request. git-receive-pack (push) is a
// write; everything else (info/refs?service=git-upload-pack, git-upload-pack,
// dumb object fetches) is a read.
func operationFor(gitSub, service string) Operation {
	switch {
	case gitSub == "git-receive-pack":
		return OpWrite
	case gitSub == "info/refs" && service == "git-receive-pack":
		return OpWrite
	default:
		return OpRead
	}
}

func (h *Handler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	reqPath := r.URL.Path
	if h.mountPrefix != "" {
		reqPath = strings.TrimPrefix(reqPath, h.mountPrefix)
	}

	ref, gitSub, err := parseRepoPath(reqPath)
	if err != nil {
		http.Error(w, "not found", http.StatusNotFound)
		return
	}

	agentID, ok := h.resolve(r)
	if !ok || agentID == "" {
		w.Header().Set("WWW-Authenticate", `Bearer realm="agent-center git"`)
		http.Error(w, "unauthenticated", http.StatusUnauthorized)
		return
	}

	op := operationFor(gitSub, r.URL.Query().Get("service"))
	if authErr := h.authz.Authorize(r.Context(), agentID, ref, op); authErr != nil {
		writeAuthError(w, authErr)
		return
	}

	// Repo must be provisioned before it can be served (provisioning is an
	// explicit lifecycle step, not implicit on first touch).
	exists, existErr := h.host.RepoExists(ref)
	if existErr != nil {
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}
	if !exists {
		http.Error(w, "repo not found", http.StatusNotFound)
		return
	}

	h.serveCGI(w, r, ref, gitSub, agentID)
}

// writeAuthError maps authz sentinels to HTTP status codes.
func writeAuthError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, ErrUnauthenticated):
		w.Header().Set("WWW-Authenticate", `Bearer realm="agent-center git"`)
		http.Error(w, "unauthenticated", http.StatusUnauthorized)
	case errors.Is(err, ErrForbidden):
		http.Error(w, "forbidden", http.StatusForbidden)
	default:
		http.Error(w, "internal error", http.StatusInternalServerError)
	}
}

// serveCGI invokes git-http-backend, letting net/http/cgi stream the request
// body to stdin and parse the CGI response. GIT_PROJECT_ROOT points at the bare
// tree; GIT_HTTP_EXPORT_ALL is set because access control has already been
// enforced above.
func (h *Handler) serveCGI(w http.ResponseWriter, r *http.Request, ref RepoRef, gitSub, agentID string) {
	env := append([]string{}, baseGitEnv("", "", "")...)
	env = append(env,
		"GIT_PROJECT_ROOT="+h.host.Root(),
		"GIT_HTTP_EXPORT_ALL=1",
		"GIT_EXEC_PATH="+h.execPath,
		"REMOTE_USER="+agentID,
	)
	env = append(env, h.extraEnv...)

	var stderr bytes.Buffer
	backend := &cgi.Handler{
		Path:   h.httpBackend,
		Root:   "", // PATH_INFO == full canonical path below
		Env:    env,
		Stderr: &stderr,
	}

	// Serve against the canonical repo path so PATH_INFO is exactly
	// "/<kind>/<id>.git/<sub>" regardless of how the caller mounted us.
	r2 := r.Clone(r.Context())
	r2.URL.Path = "/" + ref.dirName() + "/" + gitSub
	backend.ServeHTTP(w, r2)
}
