package cli

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"strings"

	"github.com/oopslink/agent-center/internal/observability"
	"github.com/oopslink/agent-center/internal/workforce"
	wfservice "github.com/oopslink/agent-center/internal/workforce/service"
)

// AgentCommands returns the `agent` subcommand tree per ADR-0024 § 1 +
// ADR-0029 + P10 § 3.8 (Identity[kind=agent] auto-register on create).
func (a *App) AgentCommands() []*Command {
	return []*Command{
		{
			Name: "create", Summary: "Create a non-builtin AgentInstance (+ auto-register Identity)", Flags: a.agentCreateHandler,
			Examples: []string{
				`agent-center agent create --name=worker-bee --agent-cli=claudecode --worker=w-1`,
				`agent-center agent create --name=cody --agent-cli=codex --worker=w-2 --format=json`,
			},
		},
		{
			Name: "list", Summary: "List AgentInstances", Flags: a.agentListHandler,
			Examples: []string{
				`agent-center agent list`,
				`agent-center agent list --state=idle --worker=w-1`,
				`agent-center agent list --format=json`,
			},
		},
		{Name: "show", Summary: "Show an AgentInstance by id or name", Flags: a.agentShowHandler},
		{Name: "archive", Summary: "Archive an AgentInstance", Flags: a.agentArchiveHandler},
	}
}

func (a *App) agentCreateHandler(fs *flag.FlagSet) Handler {
	name := fs.String("name", "", "agent instance name (required)")
	agentCLI := fs.String("agent-cli", "", "agent CLI (claudecode | codex | opencode | etc.)")
	workerID := fs.String("worker", "", "worker id this instance lives on (required for non-builtin)")
	config := fs.String("config", "", "JSON config blob (defaults to {})")
	maxConcurrent := fs.Int("max-concurrent", -1, "max concurrent executions (negative = unset)")
	format := fs.String("format", FormatTable, formatFlagHelp())
	return func(ctx context.Context, args []string, out, errw io.Writer) ExitCode {
		if *name == "" {
			return PrintError(errw, *format, "usage_error", "--name required", ExitUsage)
		}
		if *agentCLI == "" {
			return PrintError(errw, *format, "usage_error", "--agent-cli required", ExitUsage)
		}
		if *workerID == "" {
			return PrintError(errw, *format, "usage_error", "--worker required", ExitUsage)
		}
		if a.AgentMgmtSvc == nil {
			return PrintError(errw, *format, "internal_error",
				"agent management service not wired", ExitNotImplemented)
		}
		cmd := wfservice.CreateAgentInstanceCommand{
			Name:          *name,
			AgentCLI:      *agentCLI,
			WorkerID:      workforce.WorkerID(*workerID),
			Config:        *config,
			ActorIdentity: a.DefaultActor(),
		}
		if *maxConcurrent >= 0 {
			mc := *maxConcurrent
			cmd.MaxConcurrent = &mc
		}
		res, err := a.AgentMgmtSvc.Create(ctx, cmd)
		if err != nil {
			return HandleDomainError(errw, *format, err)
		}
		if *format == "json" {
			b, _ := json.Marshal(map[string]any{
				"id":          string(res.ID),
				"identity_id": "agent:" + string(res.ID),
				"event_id":    string(res.EventID),
			})
			writeOut(out, string(b))
		} else {
			fmt.Fprintf(out, "created agent instance %q id=%s (Identity=agent:%s)\n", *name, res.ID, res.ID)
		}
		return ExitOK
	}
}

func (a *App) agentListHandler(fs *flag.FlagSet) Handler {
	stateFlag := fs.String("state", "", "filter by state (idle|active|sleeping|archived)")
	workerFlag := fs.String("worker", "", "filter by worker id")
	format := fs.String("format", FormatTable, formatFlagHelp())
	return func(ctx context.Context, args []string, out, errw io.Writer) ExitCode {
		filter := workforce.AgentInstanceFilter{}
		if *stateFlag != "" {
			st := workforce.AgentInstanceState(*stateFlag)
			filter.State = &st
		}
		if *workerFlag != "" {
			wid := workforce.WorkerID(*workerFlag)
			filter.WorkerID = &wid
		}
		list, err := a.AgentInstanceRepo.FindAll(ctx, filter)
		if err != nil {
			return HandleDomainError(errw, *format, err)
		}
		switch *format {
		case FormatJSON:
			arr := make([]map[string]any, len(list))
			for i, ai := range list {
				arr[i] = agentToMap(ai)
			}
			b, _ := json.Marshal(arr)
			writeOut(out, string(b))
		case FormatText:
			ids := make([]string, len(list))
			for i, ai := range list {
				ids[i] = string(ai.ID())
			}
			writeTextLines(out, ids)
		default:
			fmt.Fprintf(out, "%-30s %-12s %-30s %-15s %s\n", "ID", "STATE", "NAME", "AGENT_CLI", "WORKER")
			for _, ai := range list {
				w := ""
				if ai.WorkerID() != nil {
					w = string(*ai.WorkerID())
				}
				fmt.Fprintf(out, "%-30s %-12s %-30s %-15s %s\n", ai.ID(), ai.State(), ai.Name(), ai.AgentCLI(), w)
			}
		}
		return ExitOK
	}
}

func (a *App) agentShowHandler(fs *flag.FlagSet) Handler {
	format := fs.String("format", FormatTable, formatFlagHelp())
	return func(ctx context.Context, args []string, out, errw io.Writer) ExitCode {
		if len(args) < 1 {
			return PrintError(errw, *format, "usage_error", "agent show <id-or-name>", ExitUsage)
		}
		// Try id first; fall back to name lookup.
		ai, err := a.AgentInstanceRepo.FindByID(ctx, workforce.AgentInstanceID(args[0]))
		if err != nil {
			ai, err = a.AgentInstanceRepo.FindByName(ctx, args[0])
			if err != nil {
				return HandleDomainError(errw, *format, err)
			}
		}
		m := agentToMap(ai)
		if *format == "json" {
			b, _ := json.Marshal(m)
			writeOut(out, string(b))
		} else {
			w := ""
			if ai.WorkerID() != nil {
				w = string(*ai.WorkerID())
			}
			fmt.Fprintf(out, "agent %s\n  name: %s\n  state: %s\n  agent_cli: %s\n  worker: %s\n  is_builtin: %v\n  max_concurrent: %v\n  identity_id: agent:%s\n",
				ai.ID(), ai.Name(), ai.State(), ai.AgentCLI(), w, ai.IsBuiltin(), ai.MaxConcurrent(), ai.ID())
		}
		return ExitOK
	}
}

func (a *App) agentArchiveHandler(fs *flag.FlagSet) Handler {
	reasonFlag := fs.String("reason", "user_request", "archive reason (user_request|worker_offline|...)")
	message := fs.String("message", "", "archive message (required)")
	versionFlag := fs.Int("version", 0, "expected version (CAS, required)")
	format := fs.String("format", FormatTable, formatFlagHelp())
	return func(ctx context.Context, args []string, out, errw io.Writer) ExitCode {
		if len(args) < 1 {
			return PrintError(errw, *format, "usage_error", "agent archive <id>", ExitUsage)
		}
		if *message == "" {
			return PrintError(errw, *format, "usage_error", "--message required", ExitUsage)
		}
		if *versionFlag <= 0 {
			return PrintError(errw, *format, "usage_error", "--version required for CAS", ExitUsage)
		}
		if a.AgentMgmtSvc == nil {
			return PrintError(errw, *format, "internal_error",
				"agent management service not wired", ExitNotImplemented)
		}
		_, err := a.AgentMgmtSvc.Archive(ctx, wfservice.ArchiveAgentInstanceCommand{
			ID:            workforce.AgentInstanceID(args[0]),
			Reason:        workforce.AgentInstanceArchivedReason(strings.TrimSpace(*reasonFlag)),
			Message:       *message,
			Version:       *versionFlag,
			ActorIdentity: a.DefaultActor(),
		})
		if err != nil {
			return HandleDomainError(errw, *format, err)
		}
		writeOut(out, fmt.Sprintf("archived agent instance %s", args[0]))
		return ExitOK
	}
}

func agentToMap(ai *workforce.AgentInstance) map[string]any {
	w := ""
	if ai.WorkerID() != nil {
		w = string(*ai.WorkerID())
	}
	return map[string]any{
		"id":             string(ai.ID()),
		"name":           ai.Name(),
		"state":          string(ai.State()),
		"agent_cli":      ai.AgentCLI(),
		"worker_id":      w,
		"is_builtin":     ai.IsBuiltin(),
		"max_concurrent": ai.MaxConcurrent(),
		"config":         ai.Config(),
		"version":        ai.Version(),
		"identity_id":    "agent:" + string(ai.ID()),
	}
}

// _ keeps observability import alive (used via DefaultActor → Actor).
var _ = observability.Actor("")
