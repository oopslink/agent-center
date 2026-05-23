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

// WorkerCommands returns the `worker` command tree (excluding the worker
// daemon `worker run` placeholder which lives in handlers_system.go).
func (a *App) WorkerCommands() []*Command {
	return []*Command{
		{
			Name:    "enroll",
			Summary: "Enroll a worker into the center",
			Flags:   a.workerEnrollHandler,
		},
		{
			Name:    "list",
			Summary: "List enrolled workers",
			Flags:   a.workerListHandler,
		},
		{
			Name:    "status",
			Summary: "Show one worker",
			Flags:   a.workerStatusHandler,
		},
		{
			Name:    "proposal",
			Summary: "Manage worker-project proposals",
			Subcommands: []*Command{
				{Name: "list", Summary: "List proposals", Flags: a.proposalListHandler},
				{Name: "show", Summary: "Show one proposal", Flags: a.proposalShowHandler},
				{Name: "propose", Summary: "Submit a new proposal (admin)", Flags: a.proposalProposeHandler},
				{Name: "accept", Summary: "Accept a proposal", Flags: a.proposalAcceptHandler},
				{Name: "ignore", Summary: "Ignore a proposal", Flags: a.proposalIgnoreHandler},
				{Name: "unignore", Summary: "Unignore a proposal", Flags: a.proposalUnignoreHandler},
			},
		},
	}
}

// =============================================================================
// worker enroll
// =============================================================================

func (a *App) workerEnrollHandler(fs *flag.FlagSet) Handler {
	workerID := fs.String("worker-id", "", "worker id")
	capsStr := fs.String("capabilities", "claude-code", "comma-separated capability list")
	format := fs.String("format", FormatTable, formatFlagHelp())
	return func(ctx context.Context, args []string, out, errw io.Writer) ExitCode {
		if *workerID == "" {
			return PrintError(errw, *format, "usage_error", "--worker-id required", ExitUsage)
		}
		caps := splitNonEmpty(*capsStr, ",")
		res, err := a.EnrollSvc.Enroll(ctx, wfservice.EnrollCommand{
			WorkerID:      workforce.WorkerID(*workerID),
			Capabilities:  caps,
			ActorIdentity: a.DefaultActor(),
		})
		if err != nil {
			return HandleDomainError(errw, *format, err)
		}
		if *format == "json" {
			b, _ := json.Marshal(map[string]any{
				"worker_id": string(res.WorkerID),
				"event_id":  string(res.EventID),
				"version":   res.Version,
			})
			writeOut(out, string(b))
		} else {
			fmt.Fprintf(out, "enrolled worker %s (event %s, version %d)\n",
				res.WorkerID, res.EventID, res.Version)
		}
		return ExitOK
	}
}

// =============================================================================
// worker list / status
// =============================================================================

func (a *App) workerListHandler(fs *flag.FlagSet) Handler {
	statusFlag := fs.String("status", "", "filter by status (online|offline)")
	format := fs.String("format", FormatTable, formatFlagHelp())
	return func(ctx context.Context, args []string, out, errw io.Writer) ExitCode {
		var workers []*workforce.Worker
		var err error
		if *statusFlag == "" {
			workers, err = a.WorkerRepo.FindAll(ctx)
		} else {
			s := workforce.WorkerStatus(*statusFlag)
			if !s.IsValid() {
				return PrintError(errw, *format, "usage_error",
					fmt.Sprintf("invalid --status %q (must be online|offline)", *statusFlag), ExitUsage)
			}
			workers, err = a.WorkerRepo.FindByStatus(ctx, s)
		}
		if err != nil {
			return HandleDomainError(errw, *format, err)
		}
		if *format == "json" {
			arr := make([]map[string]any, len(workers))
			for i, w := range workers {
				arr[i] = workerToMap(w)
			}
			b, _ := json.Marshal(arr)
			writeOut(out, string(b))
		} else {
			fmt.Fprintf(out, "%-32s %-10s %s\n", "WORKER_ID", "STATUS", "CAPABILITIES")
			for _, w := range workers {
				fmt.Fprintf(out, "%-32s %-10s %s\n", w.ID(), w.Status(), strings.Join(w.Capabilities(), ","))
			}
		}
		return ExitOK
	}
}

func (a *App) workerStatusHandler(fs *flag.FlagSet) Handler {
	format := fs.String("format", FormatTable, formatFlagHelp())
	return func(ctx context.Context, args []string, out, errw io.Writer) ExitCode {
		if len(args) < 1 {
			return PrintError(errw, *format, "usage_error", "worker status <worker_id>", ExitUsage)
		}
		w, err := a.WorkerRepo.FindByID(ctx, workforce.WorkerID(args[0]))
		if err != nil {
			return HandleDomainError(errw, *format, err)
		}
		if *format == "json" {
			b, _ := json.Marshal(workerToMap(w))
			writeOut(out, string(b))
		} else {
			fmt.Fprintf(out, "Worker %s\n  status: %s\n  capabilities: %s\n  version: %d\n",
				w.ID(), w.Status(), strings.Join(w.Capabilities(), ","), w.Version())
		}
		return ExitOK
	}
}

func workerToMap(w *workforce.Worker) map[string]any {
	m := map[string]any{
		"worker_id":    string(w.ID()),
		"status":       string(w.Status()),
		"capabilities": w.Capabilities(),
		"version":      w.Version(),
	}
	if hb := w.LastHeartbeatAt(); hb != nil {
		m["last_heartbeat_at"] = hb.Format("2006-01-02T15:04:05.999999999Z")
	}
	return m
}

func splitNonEmpty(s, sep string) []string {
	if s == "" {
		return nil
	}
	parts := strings.Split(s, sep)
	out := parts[:0]
	for _, p := range parts {
		p = strings.TrimSpace(p)
		if p != "" {
			out = append(out, p)
		}
	}
	return out
}

// =============================================================================
// proposal list / show / propose / accept / ignore / unignore
// =============================================================================

func (a *App) proposalListHandler(fs *flag.FlagSet) Handler {
	workerID := fs.String("worker-id", "", "filter by worker id")
	status := fs.String("status", "", "filter by status (pending|accepted|ignored|superseded)")
	format := fs.String("format", FormatTable, formatFlagHelp())
	return func(ctx context.Context, args []string, out, errw io.Writer) ExitCode {
		var proposals []*workforce.WorkerProjectProposal
		var err error
		if *workerID != "" {
			var statuses []workforce.ProposalStatus
			if *status != "" {
				s := workforce.ProposalStatus(*status)
				if !s.IsValid() {
					return PrintError(errw, *format, "usage_error",
						"invalid --status (must be pending|accepted|ignored|superseded)", ExitUsage)
				}
				statuses = []workforce.ProposalStatus{s}
			}
			proposals, err = a.ProposalRepo.FindByWorkerID(ctx, workforce.WorkerID(*workerID), statuses...)
		} else if *status == string(workforce.ProposalPending) {
			proposals, err = a.ProposalRepo.FindPending(ctx)
		} else if *status != "" {
			return PrintError(errw, *format, "usage_error",
				"--status filter without --worker-id is only supported with status=pending", ExitUsage)
		} else {
			proposals, err = a.ProposalRepo.FindPending(ctx)
		}
		if err != nil {
			return HandleDomainError(errw, *format, err)
		}
		if *format == "json" {
			arr := make([]map[string]any, len(proposals))
			for i, p := range proposals {
				arr[i] = proposalToMap(p)
			}
			b, _ := json.Marshal(arr)
			writeOut(out, string(b))
		} else {
			fmt.Fprintf(out, "%-30s %-12s %-30s %s\n", "PROPOSAL_ID", "STATUS", "WORKER", "PATH")
			for _, p := range proposals {
				fmt.Fprintf(out, "%-30s %-12s %-30s %s\n", p.ID(), p.Status(), p.WorkerID(), p.CandidatePath())
			}
		}
		return ExitOK
	}
}

func (a *App) proposalShowHandler(fs *flag.FlagSet) Handler {
	format := fs.String("format", FormatTable, formatFlagHelp())
	return func(ctx context.Context, args []string, out, errw io.Writer) ExitCode {
		if len(args) < 1 {
			return PrintError(errw, *format, "usage_error", "proposal show <id>", ExitUsage)
		}
		p, err := a.ProposalRepo.FindByID(ctx, workforce.ProposalID(args[0]))
		if err != nil {
			return HandleDomainError(errw, *format, err)
		}
		if *format == "json" {
			b, _ := json.Marshal(proposalToMap(p))
			writeOut(out, string(b))
		} else {
			fmt.Fprintf(out, "Proposal %s\n  worker: %s\n  status: %s\n  candidate_path: %s\n  suggested: %s/%s\n",
				p.ID(), p.WorkerID(), p.Status(), p.CandidatePath(),
				p.SuggestedProjectID(), p.SuggestedKind())
		}
		return ExitOK
	}
}

func (a *App) proposalProposeHandler(fs *flag.FlagSet) Handler {
	workerID := fs.String("worker-id", "", "worker id")
	candidatePath := fs.String("candidate-path", "", "candidate filesystem path")
	suggestedID := fs.String("suggested-project-id", "", "suggested project slug (default = path basename)")
	suggestedKind := fs.String("suggested-kind", "", "suggested kind")
	format := fs.String("format", FormatTable, formatFlagHelp())
	return func(ctx context.Context, args []string, out, errw io.Writer) ExitCode {
		if *workerID == "" || *candidatePath == "" {
			return PrintError(errw, *format, "usage_error",
				"--worker-id and --candidate-path required", ExitUsage)
		}
		sugID := *suggestedID
		if sugID == "" {
			sugID = pathBasename(*candidatePath)
		}
		kind := workforce.ProjectKind(*suggestedKind)
		if !kind.IsValid() {
			return PrintError(errw, *format, "usage_error",
				"invalid --suggested-kind", ExitUsage)
		}
		res, err := a.AcceptanceSvc.Propose(ctx, wfservice.ProposeCommand{
			WorkerID:           workforce.WorkerID(*workerID),
			CandidatePath:      *candidatePath,
			SuggestedProjectID: workforce.ProjectID(sugID),
			SuggestedKind:      kind,
			Actor:              observability.Actor("worker:" + *workerID),
		})
		if err != nil {
			return HandleDomainError(errw, *format, err)
		}
		if *format == "json" {
			b, _ := json.Marshal(map[string]any{
				"proposal_id":    string(res.ProposalID),
				"event_id":       string(res.EventID),
				"already_exists": res.AlreadyExists,
			})
			writeOut(out, string(b))
		} else {
			suffix := ""
			if res.AlreadyExists {
				suffix = " (already exists)"
			}
			fmt.Fprintf(out, "proposed %s%s\n", res.ProposalID, suffix)
		}
		return ExitOK
	}
}

func (a *App) proposalAcceptHandler(fs *flag.FlagSet) Handler {
	projectID := fs.String("project-id", "", "override target project id")
	kind := fs.String("kind", "", "override project kind")
	format := fs.String("format", FormatTable, formatFlagHelp())
	return func(ctx context.Context, args []string, out, errw io.Writer) ExitCode {
		if len(args) < 1 {
			return PrintError(errw, *format, "usage_error", "proposal accept <id>", ExitUsage)
		}
		k := workforce.ProjectKind(*kind)
		if !k.IsValid() {
			return PrintError(errw, *format, "usage_error", "invalid --kind", ExitUsage)
		}
		res, err := a.AcceptanceSvc.Accept(ctx, wfservice.AcceptCommand{
			ProposalID:        workforce.ProposalID(args[0]),
			OverrideProjectID: workforce.ProjectID(*projectID),
			OverrideKind:      k,
			Actor:             a.DefaultActor(),
		})
		if err != nil {
			return HandleDomainError(errw, *format, err)
		}
		if *format == "json" {
			b, _ := json.Marshal(map[string]any{
				"proposal_id":     string(res.ProposalID),
				"mapping_id":      string(res.MappingID),
				"project_id":      string(res.ProjectID),
				"project_created": res.ProjectCreated,
			})
			writeOut(out, string(b))
		} else {
			fmt.Fprintf(out, "accepted proposal %s → project %s mapping %s\n",
				res.ProposalID, res.ProjectID, res.MappingID)
		}
		return ExitOK
	}
}

func (a *App) proposalIgnoreHandler(fs *flag.FlagSet) Handler {
	format := fs.String("format", FormatTable, formatFlagHelp())
	return func(ctx context.Context, args []string, out, errw io.Writer) ExitCode {
		if len(args) < 1 {
			return PrintError(errw, *format, "usage_error", "proposal ignore <id>", ExitUsage)
		}
		_, err := a.AcceptanceSvc.Ignore(ctx, wfservice.IgnoreCommand{
			ProposalID: workforce.ProposalID(args[0]),
			Actor:      a.DefaultActor(),
		})
		if err != nil {
			return HandleDomainError(errw, *format, err)
		}
		writeOut(out, fmt.Sprintf("ignored proposal %s", args[0]))
		return ExitOK
	}
}

func (a *App) proposalUnignoreHandler(fs *flag.FlagSet) Handler {
	format := fs.String("format", FormatTable, formatFlagHelp())
	return func(ctx context.Context, args []string, out, errw io.Writer) ExitCode {
		if len(args) < 1 {
			return PrintError(errw, *format, "usage_error", "proposal unignore <id>", ExitUsage)
		}
		_, err := a.AcceptanceSvc.Unignore(ctx, wfservice.IgnoreCommand{
			ProposalID: workforce.ProposalID(args[0]),
			Actor:      a.DefaultActor(),
		})
		if err != nil {
			return HandleDomainError(errw, *format, err)
		}
		writeOut(out, fmt.Sprintf("unignored proposal %s", args[0]))
		return ExitOK
	}
}

func proposalToMap(p *workforce.WorkerProjectProposal) map[string]any {
	return map[string]any{
		"proposal_id":          string(p.ID()),
		"worker_id":            string(p.WorkerID()),
		"status":               string(p.Status()),
		"candidate_path":       p.CandidatePath(),
		"suggested_project_id": string(p.SuggestedProjectID()),
		"suggested_kind":       string(p.SuggestedKind()),
		"version":              p.Version(),
	}
}

func pathBasename(p string) string {
	for i := len(p) - 1; i >= 0; i-- {
		if p[i] == '/' || p[i] == '\\' {
			return p[i+1:]
		}
	}
	return p
}
