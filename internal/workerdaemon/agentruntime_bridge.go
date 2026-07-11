package workerdaemon

// agentruntime_bridge.go — the Phase 0b seam between the daemon and the
// agentruntime package. It (1) ALIASES the shared types that moved DOWN into
// agentruntime back into workerdaemon so existing daemon code + tests keep
// compiling unchanged, (2) adapts the daemon's concrete session starters to the
// runtime's starter contracts, and (3) builds a per-agent LocalRuntime.

import (
	"context"

	"github.com/oopslink/agent-center/internal/agentruntime"
)

// --- Type aliases (moved DOWN; daemon aliases back). ---

// agentSession is the narrow session control surface (Inject/Stop/Detach).
type agentSession = agentruntime.Session

// sessionStarter / codexSessionStarter are the per-CLI session factories (test seams).
type sessionStarter = agentruntime.SessionStarter
type codexSessionStarter = agentruntime.CodexSessionStarter

// SupervisorSessionConfig configures a claude supervisor session start.
type SupervisorSessionConfig = agentruntime.SupervisorSessionConfig

// feedbackReporter is the RESULT-feedback seam.
type feedbackReporter = agentruntime.Reporter

// executorEngine is the per-agent concurrent-execution wiring (moved DOWN in Phase
// 0c). The daemon aliases it back so existing code/tests referencing the name keep
// compiling; the ENGINE itself now lives on the agent's LocalRuntime (r.exec).
type executorEngine = agentruntime.ExecutorEngine

// agentToolCaller is the narrow center agent-tool transport seam (moved DOWN). The
// daemon's *AdminClient satisfies it (CallAgentTool); tests inject a fake.
type agentToolCaller = agentruntime.ToolCaller

// UsageReport is one per-turn usage sample.
type UsageReport = agentruntime.UsageReport

// --- Const aliases. ---

const cliCodex = agentruntime.CLICodex
const wakeDedupCap = agentruntime.WakeDedupCap

// DefaultResumeNudge is injected into a RELAUNCHED agent's session for an ACTIVE
// WorkItem on boot/self-heal so the interrupted task continues.
const DefaultResumeNudge = agentruntime.DefaultResumeNudge

// --- Test-facing re-exports (pure helpers moved down; workerdaemon tests call these). ---

var streamActivityPayload = agentruntime.StreamActivityPayload
var activityEventType = agentruntime.ActivityEventType
var toolResultIsError = agentruntime.ToolResultIsError

// buildConverseBrief renders the converse stdin brief (tests call it with a
// conversePayload; convert at the boundary to the runtime's neutral request).
func buildConverseBrief(pl conversePayload) string {
	return agentruntime.BuildConverseBrief(converseRequestOf(pl))
}

// --- Payload → runtime request converters (byte-identical field mapping). ---

func workRequestOf(pl workPayload) agentruntime.WorkRequest {
	return agentruntime.WorkRequest{AgentID: pl.AgentID, TaskID: pl.TaskID, TaskRef: pl.TaskRef, Brief: pl.Brief}
}

func wakeRequestOf(pl wakePayload) agentruntime.WakeRequest {
	return agentruntime.WakeRequest{
		AgentID: pl.AgentID, TaskID: pl.TaskID, TaskRef: pl.TaskRef,
		ConversationID: pl.ConversationID, MessageID: pl.MessageID,
		MessageText: pl.MessageText, RootMessageID: pl.RootMessageID,
	}
}

func converseRequestOf(pl conversePayload) agentruntime.ConverseRequest {
	return agentruntime.ConverseRequest{
		AgentID: pl.AgentID, ConversationID: pl.ConversationID, ConvKind: pl.ConvKind,
		ConvName: pl.ConvName, SenderRef: pl.SenderRef, SenderDisplay: pl.SenderDisplay,
		MessageID: pl.MessageID, MessageText: pl.MessageText, RootMessageID: pl.RootMessageID,
		AttachmentCount: pl.AttachmentCount, OwnerRef: pl.OwnerRef,
	}
}

// --- Session starters (production adapters). ---

// startSupervisorSessionAdapter is the PRODUCTION claude session starter.
func startSupervisorSessionAdapter(ctx context.Context, cfg SupervisorSessionConfig) (agentSession, error) {
	s, err := StartSupervisorSession(ctx, cfg)
	if err != nil {
		return nil, err
	}
	return s, nil
}

// startCodexSessionAdapter is the PRODUCTION codex session starter. It converts the
// neutral CodexSpec → agentruntime.CodexSessionConfig (computing the merged runtime env) and
// starts the real CodexSession.
func startCodexSessionAdapter(ctx context.Context, spec agentruntime.CodexSpec) (agentSession, error) {
	env := runtimeAgentEnv(spec.AgentID, spec.DisplayName, spec.EnvVars)
	if spec.CodexHome != "" {
		// Export the per-agent CODEX_HOME so codex loads the generated config.toml
		// ([mcp_servers.agent-center] → center tools) instead of the shared ~/.codex.
		if env == nil {
			env = map[string]string{}
		}
		env["CODEX_HOME"] = spec.CodexHome
	}
	s, err := agentruntime.StartCodexSession(ctx, agentruntime.CodexSessionConfig{
		AgentID:  spec.AgentID,
		TasksDir: spec.TasksDir,
		Binary:   spec.Binary,
		Model:    spec.Model,
		Env:      env,
		Logger:   spec.Logger,
		OnEvent:  spec.OnEvent,
		OnExit:   spec.OnExit,
	})
	if err != nil {
		return nil, err
	}
	return s, nil
}

// startSpecOf builds a StartSpec from a reconcile payload (intent-driven start —
// forkResume=false).
func startSpecOf(pl reconcilePayload) agentruntime.StartSpec {
	return agentruntime.StartSpec{
		AgentID:            pl.AgentID,
		Version:            pl.Version,
		ForkResume:         false,
		Resume:             false,
		Model:              pl.Model,
		DisplayName:        pl.DisplayName,
		CLI:                pl.CLI,
		PromptDescription:  pl.PromptDescription,
		EnvVars:            pl.EnvVars,
		ConcurrencyEnabled: execConfigOf(pl).ConcurrencyEnabled(),
	}
}

// execConfigOf converts a reconcile payload into the neutral ExecutorConfig the
// runtime's BuildExecutorEngine consumes (the daemon→runtime boundary — agentruntime
// never sees the daemon's wire payload type). The concurrency predicate itself now
// lives on ExecutorConfig in the runtime domain (ExecutorConfig.ConcurrencyEnabled).
func execConfigOf(pl reconcilePayload) agentruntime.ExecutorConfig {
	return agentruntime.ExecutorConfig{
		AgentID:              pl.AgentID,
		DisplayName:          pl.DisplayName,
		EnvVars:              pl.EnvVars,
		MaxConcurrentTasks:   pl.MaxConcurrentTasks,
		AllowedExecutors:     pl.AllowedExecutors,
		OrchestratorModel:    pl.OrchestratorModel,
		DefaultExecutorModel: pl.DefaultExecutorModel,
		JudgeEnabled:         pl.JudgeEnabled, // T950 ②: per-agent judge opt-in (default OFF)
		CLI:                  pl.CLI,
	}
}
