package workerdaemon

// Agent-CLI adapter registration (v2.7 #177 / FINDING-D).
//
// Each adapter package self-registers into agentadapter.DefaultRegistry via
// its init() (agentadapter.Register(New(""))). Registration therefore only
// happens for packages that are actually imported into the worker daemon
// binary. claude-code arrives transitively (claude_session.go), but codex and
// opencode were imported nowhere — so their init() never ran, DefaultRegistry
// held only claude-code, and ProbeAllAdapters reported a single CLI even when
// codex/opencode were installed and on PATH (#175 fixed PATH, but PATH is
// necessary-not-sufficient without registration).
//
// These blank imports trigger all three adapters' init() registration so the
// daemon's online-time ProbeAllAdapters discovers every supported CLI. Keep
// this list in sync with internal/agentadapter/* packages.
import (
	_ "github.com/oopslink/agent-center/internal/agentadapter/claudecode"
	_ "github.com/oopslink/agent-center/internal/agentadapter/codex"
	_ "github.com/oopslink/agent-center/internal/agentadapter/opencode"
)
