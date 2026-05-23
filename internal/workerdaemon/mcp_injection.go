package workerdaemon

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"

	"github.com/oopslink/agent-center/internal/secretmgmt"
)

// SecretResolver is the worker-daemon-side abstraction for the
// center's SecretManagement BC SecretResolutionService (ADR-0027 § 7).
// Worker daemon supplies the CallerActor (e.g. "worker:<id>"); the impl
// runs the actual RPC + decryption.
type SecretResolver interface {
	Resolve(ctx context.Context, secretName string) (plaintext []byte, err error)
}

// MCPInjector materialises home_dir/mcp_config.json into a per-execution
// home_dir/mcp_config.runtime.json with all `secret:<name>` references
// replaced by their plaintext (per ADR-0027 § 7).
//
// Lifecycle:
//   - Inject parses the template, calls SecretResolver for every SecretRef,
//     writes the runtime JSON at mode 0600, and returns the path
//   - Cleanup unlinks the runtime file; safe to call on a missing file
//   - CrashRecoveryScan walks all known home_dirs and unlinks
//     mcp_config.runtime.json for executions no longer active
type MCPInjector struct {
	resolver SecretResolver
}

// NewMCPInjector wires the injector. resolver may be nil iff Inject is not
// called.
func NewMCPInjector(resolver SecretResolver) *MCPInjector {
	return &MCPInjector{resolver: resolver}
}

// MCPInjectError carries the per-secret resolve failure detail so caller
// can NACK with reason=secret_unresolvable + message.
type MCPInjectError struct {
	SecretName string
	Cause      error
}

// Error implements error.
func (e *MCPInjectError) Error() string {
	return fmt.Sprintf("mcp injection: resolve secret %q: %v", e.SecretName, e.Cause)
}

// Unwrap exposes the underlying cause for errors.Is.
func (e *MCPInjectError) Unwrap() error { return e.Cause }

// Inject reads <homeDir>/mcp_config.json, replaces every "secret:<name>"
// string value (recursively walking the JSON) with the resolved plaintext,
// and writes <homeDir>/mcp_config.runtime.json at mode 0600.
//
// Returns the absolute path of the runtime.json on success and a Cleanup()
// function that the caller schedules in defer / post-spawn / crash recovery.
//
// If the template file is missing, returns ("", nil, nil) — no injection
// needed.
func (i *MCPInjector) Inject(ctx context.Context, homeDir string) (string, func(), error) {
	if homeDir == "" {
		return "", noopCleanup, errors.New("mcp injector: homeDir required")
	}
	templatePath := filepath.Join(homeDir, "mcp_config.json")
	raw, err := os.ReadFile(templatePath)
	if err != nil {
		if os.IsNotExist(err) {
			return "", noopCleanup, nil // no mcp config — nothing to inject
		}
		return "", noopCleanup, fmt.Errorf("mcp injector: read template: %w", err)
	}
	var doc any
	if err := json.Unmarshal(raw, &doc); err != nil {
		return "", noopCleanup, fmt.Errorf("mcp injector: parse template: %w", err)
	}
	resolved, err := i.resolveRefs(ctx, doc)
	if err != nil {
		return "", noopCleanup, err
	}
	out, err := json.MarshalIndent(resolved, "", "  ")
	if err != nil {
		return "", noopCleanup, fmt.Errorf("mcp injector: marshal runtime: %w", err)
	}
	runtimePath := filepath.Join(homeDir, "mcp_config.runtime.json")
	if err := os.WriteFile(runtimePath, out, 0o600); err != nil {
		return "", noopCleanup, fmt.Errorf("mcp injector: write runtime: %w", err)
	}
	cleanup := func() { _ = os.Remove(runtimePath) }
	return runtimePath, cleanup, nil
}

// resolveRefs walks the JSON document and replaces any string value of the
// form `secret:<name>` with the resolved plaintext.
func (i *MCPInjector) resolveRefs(ctx context.Context, v any) (any, error) {
	switch t := v.(type) {
	case map[string]any:
		for k, val := range t {
			next, err := i.resolveRefs(ctx, val)
			if err != nil {
				return nil, err
			}
			t[k] = next
		}
		return t, nil
	case []any:
		for idx, val := range t {
			next, err := i.resolveRefs(ctx, val)
			if err != nil {
				return nil, err
			}
			t[idx] = next
		}
		return t, nil
	case string:
		if !secretmgmt.IsSecretRefValue(t) {
			return t, nil
		}
		ref, err := secretmgmt.ParseSecretRef(t)
		if err != nil {
			return nil, &MCPInjectError{SecretName: t, Cause: err}
		}
		if i.resolver == nil {
			return nil, &MCPInjectError{SecretName: ref.Name(), Cause: errors.New("no SecretResolver wired")}
		}
		plain, err := i.resolver.Resolve(ctx, ref.Name())
		if err != nil {
			return nil, &MCPInjectError{SecretName: ref.Name(), Cause: err}
		}
		return string(plain), nil
	default:
		return v, nil
	}
}

// CrashRecoveryScan walks every direct subdirectory of agentHomeRoot and
// unlinks any mcp_config.runtime.json whose owning execution is no longer
// in the supplied activeExecutionIDs set. This protects against leftover
// runtime files after a worker daemon crash (per ADR-0027 § 8 + plan
// § 3.8 step 5).
//
// agentHomeRoot is typically ~/.agent-center-worker/agents/.
// activeExecutions is an opaque map; callers populate it by querying the
// task_executions table for status IN ('submitted','working','input_required').
//
// Returns the count of removed files; non-fatal errors are accumulated in
// the returned multi-error.
func CrashRecoveryScan(agentHomeRoot string, activeAgentInstances map[string]bool) (int, error) {
	if agentHomeRoot == "" {
		return 0, errors.New("crash recovery: agentHomeRoot required")
	}
	entries, err := os.ReadDir(agentHomeRoot)
	if err != nil {
		if os.IsNotExist(err) {
			return 0, nil
		}
		return 0, fmt.Errorf("crash recovery: read root: %w", err)
	}
	var (
		removed int
		errs    []error
	)
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		agentID := entry.Name()
		// Skip removal when the agent has an active execution.
		if activeAgentInstances[agentID] {
			continue
		}
		runtimePath := filepath.Join(agentHomeRoot, agentID, "mcp_config.runtime.json")
		err := os.Remove(runtimePath)
		if err == nil {
			removed++
			continue
		}
		if os.IsNotExist(err) {
			continue
		}
		errs = append(errs, fmt.Errorf("remove %s: %w", runtimePath, err))
	}
	if len(errs) > 0 {
		return removed, errors.Join(errs...)
	}
	return removed, nil
}

// noopCleanup is the cleanup returned when Inject was a no-op (no template
// file). Saves callers from nil-checks.
var noopCleanup = func() {}
