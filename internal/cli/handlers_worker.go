package cli

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"strings"

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
	}
}

// =============================================================================
// worker enroll
// =============================================================================

func (a *App) workerEnrollHandler(fs *flag.FlagSet) Handler {
	workerID := fs.String("worker-id", "", "worker id")
	format := fs.String("format", FormatTable, formatFlagHelp())
	return func(ctx context.Context, args []string, out, errw io.Writer) ExitCode {
		if *workerID == "" {
			return PrintError(errw, *format, "usage_error", "--worker-id required", ExitUsage)
		}
		// v2.7 #147: capabilities are auto-discovered by the worker daemon
		// (ProbeAllAdapters → report on every online), not hand-set. A manual
		// `worker enroll` seeds an empty set; the daemon fills it on online.
		var caps []string
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
				ActorIdentity: a.operatorActor(),
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
