package supervisor

import (
	"fmt"
	"os"
	"sort"
	"strings"
	"time"

	"github.com/oopslink/agent-center/internal/cognition"
	"github.com/oopslink/agent-center/internal/cognition/memory"
	"github.com/oopslink/agent-center/internal/observability"
)

// PromptThreshold is the byte cap at which we switch to blob_ref instead
// of inline prompt (cognition/01 § 3 + conventions § 8). When the assembled
// prompt exceeds this, the caller writes it to BlobStore and passes the
// ref to the subprocess.
const PromptThreshold = 10 * 1024

// AssembleInput captures the parameters for Assemble.
type AssembleInput struct {
	Scope         cognition.InvocationScope
	TriggerEvents []*observability.Event
	MemoryDir     string
}

// AssembleResult bundles the final supervisor prompt + the working
// directory claude should be launched in.
type AssembleResult struct {
	Prompt   string
	WorkDir  string
	SkillRef string // absolute path of the supervisor.md skill file
}

// Assemble builds the supervisor prompt. CWD is derived from the
// InvocationScope so claude's ancestor walk picks up the right
// CLAUDE.md chain. supervisor.md self-memory is loaded via the Read
// instruction inside the skill itself.
//
// Pure function modulo MemoryDir → MemoryScope mapping; the skill file
// reference is the actual disk path the caller has materialised.
func Assemble(in AssembleInput) (AssembleResult, error) {
	if in.Scope.IsZero() {
		return AssembleResult{}, fmt.Errorf("prompt: scope required")
	}
	memScope, err := invocationScopeToMemoryScope(in.Scope, in.TriggerEvents)
	if err != nil {
		return AssembleResult{}, err
	}
	memPath, err := memory.AbsPath(in.MemoryDir, memScope)
	if err != nil {
		return AssembleResult{}, err
	}
	workDir := memDirOfPath(memPath)
	if _, err := os.Stat(workDir); err != nil {
		// Working dir must exist before claude can chdir into it.
		// Memory subscriber should have created it; if not, fall back to
		// memoryDir.
		workDir = in.MemoryDir
	}
	var b strings.Builder
	b.WriteString("# Supervisor Invocation\n\n")
	b.WriteString(fmt.Sprintf("Scope: `%s`\n", in.Scope.String()))
	b.WriteString(fmt.Sprintf("CWD: `%s`\n\n", workDir))
	b.WriteString("## Trigger events (sorted by occurred_at DESC)\n\n")
	events := append([]*observability.Event(nil), in.TriggerEvents...)
	sort.Slice(events, func(i, j int) bool {
		return events[i].OccurredAt().After(events[j].OccurredAt())
	})
	for _, e := range events {
		b.WriteString(formatEvent(e))
	}
	b.WriteString("\n## Your task\n\n")
	b.WriteString("Read `$AGENT_CENTER_MEMORY_DIR/supervisor.md` first, then survey the events above and decide what (if anything) to do. Follow the skill's decision protocol. Every action MUST include `--rationale`.\n")
	return AssembleResult{
		Prompt:  b.String(),
		WorkDir: workDir,
	}, nil
}

func formatEvent(e *observability.Event) string {
	if e == nil {
		return ""
	}
	occ := e.OccurredAt().Format(time.RFC3339)
	refs := refsOneLine(e.Refs())
	payload := payloadOneLine(e.Payload())
	return fmt.Sprintf("- `%s` [%s] refs=%s payload=%s\n",
		e.ID(), e.Type(), refs, payload) + fmt.Sprintf("  occurred_at=%s actor=%s\n", occ, e.Actor())
}

func refsOneLine(refs observability.EventRefs) string {
	parts := []string{}
	if refs.TaskID != "" {
		parts = append(parts, "task_id="+refs.TaskID)
	}
	if refs.IssueID != "" {
		parts = append(parts, "issue_id="+refs.IssueID)
	}
	if refs.ExecutionID != "" {
		parts = append(parts, "execution_id="+refs.ExecutionID)
	}
	if refs.WorkerID != "" {
		parts = append(parts, "worker_id="+refs.WorkerID)
	}
	if refs.ConversationID != "" {
		parts = append(parts, "conversation_id="+refs.ConversationID)
	}
	if refs.InputRequestID != "" {
		parts = append(parts, "input_request_id="+refs.InputRequestID)
	}
	if refs.ProjectID != "" {
		parts = append(parts, "project_id="+refs.ProjectID)
	}
	if len(parts) == 0 {
		return "{}"
	}
	return "{" + strings.Join(parts, ",") + "}"
}

func payloadOneLine(p map[string]any) string {
	if len(p) == 0 {
		return "{}"
	}
	keys := make([]string, 0, len(p))
	for k := range p {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	out := make([]string, 0, len(keys))
	for _, k := range keys {
		// Truncate very long values to keep prompt readable.
		s := fmt.Sprintf("%v", p[k])
		if len(s) > 80 {
			s = s[:80] + "...(truncated)"
		}
		out = append(out, fmt.Sprintf("%s=%s", k, s))
	}
	return "{" + strings.Join(out, ",") + "}"
}

func memDirOfPath(p string) string {
	if p == "" {
		return ""
	}
	// directory containing the CLAUDE.md / supervisor.md file
	for i := len(p) - 1; i >= 0; i-- {
		if p[i] == '/' || p[i] == '\\' {
			return p[:i]
		}
	}
	return p
}

// invocationScopeToMemoryScope maps Invocation scope → Memory scope. Both
// task / issue need a project_id, which is sourced from the latest trigger
// event with refs.project_id set.
func invocationScopeToMemoryScope(scope cognition.InvocationScope, events []*observability.Event) (memory.MemoryScope, error) {
	switch scope.Kind() {
	case cognition.ScopeGlobal:
		return memory.MemoryScope{Kind: memory.MemScopeGlobal}, nil
	case cognition.ScopeConversation:
		return memory.MemoryScope{Kind: memory.MemScopeConversation, Key: scope.Key()}, nil
	case cognition.ScopeWorker:
		return memory.MemoryScope{Kind: memory.MemScopeWorker, Key: scope.Key()}, nil
	case cognition.ScopeTask:
		project := findProject(events)
		if project == "" {
			// Fallback: use scope key as project too — assembler still
			// returns a usable path (task without project context).
			return memory.MemoryScope{Kind: memory.MemScopeTask, Key: scope.Key(), ProjectID: "_unbound_"}, nil
		}
		return memory.MemoryScope{Kind: memory.MemScopeTask, Key: scope.Key(), ProjectID: project}, nil
	case cognition.ScopeIssue:
		project := findProject(events)
		if project == "" {
			return memory.MemoryScope{Kind: memory.MemScopeIssue, Key: scope.Key(), ProjectID: "_unbound_"}, nil
		}
		return memory.MemoryScope{Kind: memory.MemScopeIssue, Key: scope.Key(), ProjectID: project}, nil
	}
	return memory.MemoryScope{}, fmt.Errorf("prompt: unknown scope kind %q", scope.Kind())
}

func findProject(events []*observability.Event) string {
	for _, e := range events {
		if e.Refs().ProjectID != "" {
			return e.Refs().ProjectID
		}
	}
	return ""
}

// writeFile is a small helper that creates parent dirs + writes the file.
func writeFile(path string, content []byte) error {
	if err := os.MkdirAll(memDirOfPath(path), 0o755); err != nil {
		return err
	}
	return os.WriteFile(path, content, 0o644)
}
