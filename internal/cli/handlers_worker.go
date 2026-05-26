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
//
// v2.2 Phase B (per docs/plans/v2.2-audits/v22-B-cli-refactor-audit.md):
// every handler in this file now routes through a.Client (admin
// endpoint) when a Client is configured. A transitional fallback to
// direct Service / Repo access remains for the test path that
// constructs an App via newTestApp() without a Client; that fallback
// dies once the next phase of v2.2-B converts test scaffolding to use
// setupAdminServerForTests (see admin_client_testhelper.go).
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
		// v2.2-B: prefer the admin Client. Fall back to direct Service
		// access for back-compat with in-process tests until they're
		// migrated to setupAdminServerForTests.
		var workerOut, eventOut string
		var version int
		if a.Client != nil {
			res, err := a.Client.WorkerEnroll(ctx, WorkerEnrollRequest{
				WorkerID:     *workerID,
				Capabilities: caps,
			})
			if err != nil {
				return HandleClientError(errw, *format, err)
			}
			workerOut, eventOut, version = res.WorkerID, res.EventID, res.Version
		} else {
			res, err := a.EnrollSvc.Enroll(ctx, wfservice.EnrollCommand{
				WorkerID:      workforce.WorkerID(*workerID),
				Capabilities:  caps,
				ActorIdentity: a.DefaultActor(),
			})
			if err != nil {
				return HandleDomainError(errw, *format, err)
			}
			workerOut, eventOut, version = string(res.WorkerID), string(res.EventID), res.Version
		}
		if *format == "json" {
			b, _ := json.Marshal(map[string]any{
				"worker_id": workerOut,
				"event_id":  eventOut,
				"version":   version,
			})
			writeOut(out, string(b))
		} else {
			fmt.Fprintf(out, "enrolled worker %s (event %s, version %d)\n",
				workerOut, eventOut, version)
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
		var dtos []WorkerDTO
		if a.Client != nil {
			var err error
			if *statusFlag == "" {
				dtos, err = a.Client.WorkerFindAll(ctx)
			} else {
				s := workforce.WorkerStatus(*statusFlag)
				if !s.IsValid() {
					return PrintError(errw, *format, "usage_error",
						fmt.Sprintf("invalid --status %q (must be online|offline)", *statusFlag), ExitUsage)
				}
				dtos, err = a.Client.WorkerFindByStatus(ctx, *statusFlag)
			}
			if err != nil {
				return HandleClientError(errw, *format, err)
			}
		} else {
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
			dtos = workersToDTOs(workers)
		}
		if *format == "json" {
			arr := make([]map[string]any, len(dtos))
			for i, w := range dtos {
				arr[i] = workerDTOToMap(w)
			}
			b, _ := json.Marshal(arr)
			writeOut(out, string(b))
		} else {
			fmt.Fprintf(out, "%-32s %-10s %s\n", "WORKER_ID", "STATUS", "CAPABILITIES")
			for _, w := range dtos {
				fmt.Fprintf(out, "%-32s %-10s %s\n", w.WorkerID, w.Status, strings.Join(w.Capabilities, ","))
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
		var dto WorkerDTO
		if a.Client != nil {
			var err error
			dto, err = a.Client.WorkerFindByID(ctx, args[0])
			if err != nil {
				return HandleClientError(errw, *format, err)
			}
		} else {
			w, err := a.WorkerRepo.FindByID(ctx, workforce.WorkerID(args[0]))
			if err != nil {
				return HandleDomainError(errw, *format, err)
			}
			dto = workerToDTO(w)
		}
		if *format == "json" {
			b, _ := json.Marshal(workerDTOToMap(dto))
			writeOut(out, string(b))
		} else {
			fmt.Fprintf(out, "Worker %s\n  status: %s\n  capabilities: %s\n  version: %d\n",
				dto.WorkerID, dto.Status, strings.Join(dto.Capabilities, ","), dto.Version)
		}
		return ExitOK
	}
}

// workerDTOToMap renders a WorkerDTO into the legacy JSON-output shape
// preserved by the CLI's human/json formatting contract.
func workerDTOToMap(w WorkerDTO) map[string]any {
	m := map[string]any{
		"worker_id":    w.WorkerID,
		"status":       w.Status,
		"capabilities": w.Capabilities,
		"version":      w.Version,
	}
	if w.LastHeartbeatAt != "" {
		m["last_heartbeat_at"] = w.LastHeartbeatAt
	}
	return m
}

// workerToDTO + workersToDTOs adapt domain aggregates back into the DTO
// shape for the transitional fallback path (when a.Client is nil). Once
// every test rebuilds App via setupAdminServerForTests these can go.
func workerToDTO(w *workforce.Worker) WorkerDTO {
	dto := WorkerDTO{
		WorkerID:     string(w.ID()),
		Status:       string(w.Status()),
		Capabilities: w.Capabilities(),
		Version:      w.Version(),
	}
	if hb := w.LastHeartbeatAt(); hb != nil {
		dto.LastHeartbeatAt = hb.Format("2006-01-02T15:04:05.999999999Z")
	}
	return dto
}

func workersToDTOs(ws []*workforce.Worker) []WorkerDTO {
	out := make([]WorkerDTO, len(ws))
	for i, w := range ws {
		out[i] = workerToDTO(w)
	}
	return out
}

// workerToMap is the legacy projection helper preserved here for
// transitional tests (handlers_more_test.go::TestWorkerToMap_WithHeartbeat
// and friends). New callers should go through Client + workerDTOToMap.
func workerToMap(w *workforce.Worker) map[string]any {
	return workerDTOToMap(workerToDTO(w))
}

// proposalToMap is the legacy projection helper preserved for the same
// reason as workerToMap.
func proposalToMap(p *workforce.WorkerProjectProposal) map[string]any {
	return proposalDTOToMap(proposalToDTO(p))
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
		var dtos []ProposalDTO
		if a.Client != nil {
			var err error
			if *workerID != "" {
				if *status != "" {
					s := workforce.ProposalStatus(*status)
					if !s.IsValid() {
						return PrintError(errw, *format, "usage_error",
							"invalid --status (must be pending|accepted|ignored|superseded)", ExitUsage)
					}
				}
				dtos, err = a.Client.ProposalFindByWorkerID(ctx, *workerID, *status)
			} else if *status == string(workforce.ProposalPending) {
				dtos, err = a.Client.ProposalFindPending(ctx)
			} else if *status != "" {
				return PrintError(errw, *format, "usage_error",
					"--status filter without --worker-id is only supported with status=pending", ExitUsage)
			} else {
				dtos, err = a.Client.ProposalFindPending(ctx)
			}
			if err != nil {
				return HandleClientError(errw, *format, err)
			}
		} else {
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
			dtos = proposalsToDTOs(proposals)
		}
		if *format == "json" {
			arr := make([]map[string]any, len(dtos))
			for i, p := range dtos {
				arr[i] = proposalDTOToMap(p)
			}
			b, _ := json.Marshal(arr)
			writeOut(out, string(b))
		} else {
			fmt.Fprintf(out, "%-30s %-12s %-30s %s\n", "PROPOSAL_ID", "STATUS", "WORKER", "PATH")
			for _, p := range dtos {
				fmt.Fprintf(out, "%-30s %-12s %-30s %s\n", p.ProposalID, p.Status, p.WorkerID, p.CandidatePath)
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
		var p ProposalDTO
		if a.Client != nil {
			var err error
			p, err = a.Client.ProposalFindByID(ctx, args[0])
			if err != nil {
				return HandleClientError(errw, *format, err)
			}
		} else {
			pp, err := a.ProposalRepo.FindByID(ctx, workforce.ProposalID(args[0]))
			if err != nil {
				return HandleDomainError(errw, *format, err)
			}
			p = proposalToDTO(pp)
		}
		if *format == "json" {
			b, _ := json.Marshal(proposalDTOToMap(p))
			writeOut(out, string(b))
		} else {
			fmt.Fprintf(out, "Proposal %s\n  worker: %s\n  status: %s\n  candidate_path: %s\n  suggested: %s\n",
				p.ProposalID, p.WorkerID, p.Status, p.CandidatePath,
				p.SuggestedProjectID)
		}
		return ExitOK
	}
}

func (a *App) proposalProposeHandler(fs *flag.FlagSet) Handler {
	workerID := fs.String("worker-id", "", "worker id")
	candidatePath := fs.String("candidate-path", "", "candidate filesystem path")
	suggestedID := fs.String("suggested-project-id", "", "suggested project id (default = path basename)")
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
		var (
			propID        string
			eventID       string
			alreadyExists bool
		)
		if a.Client != nil {
			res, err := a.Client.ProposalPropose(ctx, ProposalProposeRequest{
				WorkerID:           *workerID,
				CandidatePath:      *candidatePath,
				SuggestedProjectID: sugID,
			})
			if err != nil {
				return HandleClientError(errw, *format, err)
			}
			propID, eventID, alreadyExists = res.ProposalID, res.EventID, res.AlreadyExists
		} else {
			res, err := a.AcceptanceSvc.Propose(ctx, wfservice.ProposeCommand{
				WorkerID:           workforce.WorkerID(*workerID),
				CandidatePath:      *candidatePath,
				SuggestedProjectID: workforce.ProjectID(sugID),
				Actor:              observability.Actor("worker:" + *workerID),
			})
			if err != nil {
				return HandleDomainError(errw, *format, err)
			}
			propID, eventID, alreadyExists = string(res.ProposalID), string(res.EventID), res.AlreadyExists
		}
		if *format == "json" {
			b, _ := json.Marshal(map[string]any{
				"proposal_id":    propID,
				"event_id":       eventID,
				"already_exists": alreadyExists,
			})
			writeOut(out, string(b))
		} else {
			suffix := ""
			if alreadyExists {
				suffix = " (already exists)"
			}
			fmt.Fprintf(out, "proposed %s%s\n", propID, suffix)
		}
		return ExitOK
	}
}

func (a *App) proposalAcceptHandler(fs *flag.FlagSet) Handler {
	projectID := fs.String("project-id", "", "override target project id")
	format := fs.String("format", FormatTable, formatFlagHelp())
	return func(ctx context.Context, args []string, out, errw io.Writer) ExitCode {
		if len(args) < 1 {
			return PrintError(errw, *format, "usage_error", "proposal accept <id>", ExitUsage)
		}
		var (
			propID, mappingID, projID string
			projectCreated            bool
		)
		if a.Client != nil {
			res, err := a.Client.ProposalAccept(ctx, ProposalAcceptRequest{
				ProposalID:        args[0],
				OverrideProjectID: *projectID,
			})
			if err != nil {
				return HandleClientError(errw, *format, err)
			}
			propID, mappingID, projID, projectCreated = res.ProposalID, res.MappingID, res.ProjectID, res.ProjectCreated
		} else {
			res, err := a.AcceptanceSvc.Accept(ctx, wfservice.AcceptCommand{
				ProposalID:        workforce.ProposalID(args[0]),
				OverrideProjectID: workforce.ProjectID(*projectID),
				Actor:             a.DefaultActor(),
			})
			if err != nil {
				return HandleDomainError(errw, *format, err)
			}
			propID, mappingID, projID, projectCreated = string(res.ProposalID), string(res.MappingID), string(res.ProjectID), res.ProjectCreated
		}
		if *format == "json" {
			b, _ := json.Marshal(map[string]any{
				"proposal_id":     propID,
				"mapping_id":      mappingID,
				"project_id":      projID,
				"project_created": projectCreated,
			})
			writeOut(out, string(b))
		} else {
			fmt.Fprintf(out, "accepted proposal %s → project %s mapping %s\n",
				propID, projID, mappingID)
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
		if a.Client != nil {
			if _, err := a.Client.ProposalIgnore(ctx, ProposalIgnoreRequest{ProposalID: args[0]}); err != nil {
				return HandleClientError(errw, *format, err)
			}
		} else {
			_, err := a.AcceptanceSvc.Ignore(ctx, wfservice.IgnoreCommand{
				ProposalID: workforce.ProposalID(args[0]),
				Actor:      a.DefaultActor(),
			})
			if err != nil {
				return HandleDomainError(errw, *format, err)
			}
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
		if a.Client != nil {
			if _, err := a.Client.ProposalUnignore(ctx, ProposalIgnoreRequest{ProposalID: args[0]}); err != nil {
				return HandleClientError(errw, *format, err)
			}
		} else {
			_, err := a.AcceptanceSvc.Unignore(ctx, wfservice.IgnoreCommand{
				ProposalID: workforce.ProposalID(args[0]),
				Actor:      a.DefaultActor(),
			})
			if err != nil {
				return HandleDomainError(errw, *format, err)
			}
		}
		writeOut(out, fmt.Sprintf("unignored proposal %s", args[0]))
		return ExitOK
	}
}

// proposalDTOToMap mirrors the legacy proposalToMap for JSON output.
func proposalDTOToMap(p ProposalDTO) map[string]any {
	return map[string]any{
		"proposal_id":          p.ProposalID,
		"worker_id":            p.WorkerID,
		"status":               p.Status,
		"candidate_path":       p.CandidatePath,
		"suggested_project_id": p.SuggestedProjectID,
		"version":              p.Version,
	}
}

// proposalToDTO / proposalsToDTOs adapt domain aggregates for the
// transitional fallback path.
func proposalToDTO(p *workforce.WorkerProjectProposal) ProposalDTO {
	return ProposalDTO{
		ProposalID:         string(p.ID()),
		WorkerID:           string(p.WorkerID()),
		Status:             string(p.Status()),
		CandidatePath:      p.CandidatePath(),
		SuggestedProjectID: string(p.SuggestedProjectID()),
		Version:            p.Version(),
	}
}

func proposalsToDTOs(ps []*workforce.WorkerProjectProposal) []ProposalDTO {
	out := make([]ProposalDTO, len(ps))
	for i, p := range ps {
		out[i] = proposalToDTO(p)
	}
	return out
}

func pathBasename(p string) string {
	for i := len(p) - 1; i >= 0; i-- {
		if p[i] == '/' || p[i] == '\\' {
			return p[i+1:]
		}
	}
	return p
}
