package cli

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"strings"
	"time"

	"github.com/oopslink/agent-center/internal/observability"
	"github.com/oopslink/agent-center/internal/taskruntime"
	"github.com/oopslink/agent-center/internal/taskruntime/inputrequest"
	trservice "github.com/oopslink/agent-center/internal/taskruntime/service"
)

// InputRequestCommands returns the user-facing `input-request` subcommand
// tree per P11 § 3.7. Agent-facing `request-input` lives separately under
// agent runtime CLI; this group is for the user to **answer** pending IRs.
//
// v2.2 Phase B (per docs/plans/v2.2-audits/v22-B-cli-refactor-audit.md):
// every handler in this file routes through a.Client (admin endpoint)
// when a Client is configured. The transitional Service / Repo fallback
// remains for the test path that constructs an App via newTestApp()
// without a Client.
func (a *App) InputRequestCommands() []*Command {
	return []*Command{
		{
			Name: "list", Summary: "List input requests (optional --pending)", Flags: a.irListHandler,
			Examples: []string{
				`agent-center input-request list`,
				`agent-center input-request list --execution=E-01HXXX --format=json`,
			},
		},
		{Name: "show", Summary: "Show one input request", Flags: a.irShowHandler},
		{
			Name: "respond", Summary: "Respond to a pending input request", Flags: a.irRespondHandler,
			Examples: []string{
				`agent-center input-request respond IR-01HXXX --answer=yes`,
				`agent-center input-request respond IR-01HXXX --answer-file=./response.txt --format=json`,
				`echo "approved" | agent-center input-request respond IR-01HXXX --answer-file=-`,
			},
		},
		{
			Name: "cancel", Summary: "Cancel a pending input request (frees the execution)", Flags: a.irCancelHandler,
			Examples: []string{
				`agent-center input-request cancel IR-01HXXX --message="user reconsidered"`,
			},
		},
	}
}

func (a *App) irListHandler(fs *flag.FlagSet) Handler {
	pending := fs.Bool("pending", false, "show only status=pending (default behavior)")
	execID := fs.String("execution", "", "filter by execution id (exact)")
	format := fs.String("format", FormatTable, formatFlagHelp())
	return func(ctx context.Context, args []string, out, errw io.Writer) ExitCode {
		_ = pending
		var (
			dtos []InputRequestDTO
			err  error
		)
		if a.Client != nil {
			if *execID != "" {
				dto, ferr := a.Client.IRFindByExecutionID(ctx, *execID)
				if ferr == nil {
					dtos = []InputRequestDTO{dto}
				} else if ce, ok := ferr.(*ClientError); ok && ce.IsNotFound() {
					// Empty list — same legacy semantics as
					// inputrequest.ErrInputRequestNotFound below.
				} else {
					err = ferr
				}
			} else {
				dtos, err = a.Client.IRFindPending(ctx)
			}
			if err != nil {
				return HandleClientError(errw, *format, err)
			}
		} else {
			var irs []*inputrequest.InputRequest
			if *execID != "" {
				ir, ferr := a.IRRepo.FindByTaskExecutionID(ctx, taskruntime.TaskExecutionID(*execID))
				if ferr == nil {
					irs = []*inputrequest.InputRequest{ir}
				} else if !errors.Is(ferr, inputrequest.ErrInputRequestNotFound) {
					err = ferr
				}
			} else {
				// FindPending(olderThan=now+1y) returns every pending IR.
				irs, err = a.IRRepo.FindPending(ctx, time.Now().UTC().Add(24*365*time.Hour))
			}
			if err != nil {
				return HandleDomainError(errw, *format, err)
			}
			dtos = irsToDTOs(irs)
		}
		switch *format {
		case FormatJSON:
			arr := make([]map[string]any, len(dtos))
			for i, ir := range dtos {
				arr[i] = irDTOToMap(ir)
			}
			b, _ := json.Marshal(arr)
			writeOut(out, string(b))
		case FormatText:
			ids := make([]string, len(dtos))
			for i, ir := range dtos {
				ids[i] = ir.ID
			}
			writeTextLines(out, ids)
		default:
			fmt.Fprintf(out, "%-30s %-12s %-30s %s\n", "ID", "STATUS", "EXECUTION", "QUESTION")
			for _, ir := range dtos {
				q := ir.Question
				if len(q) > 50 {
					q = q[:50] + "…"
				}
				fmt.Fprintf(out, "%-30s %-12s %-30s %s\n", ir.ID, ir.Status, ir.ExecutionID, q)
			}
		}
		return ExitOK
	}
}

func (a *App) irShowHandler(fs *flag.FlagSet) Handler {
	format := fs.String("format", FormatTable, formatFlagHelp())
	return func(ctx context.Context, args []string, out, errw io.Writer) ExitCode {
		if len(args) < 1 {
			return PrintError(errw, *format, "usage_error", "input-request show <id>", ExitUsage)
		}
		var dto InputRequestDTO
		if a.Client != nil {
			d, err := a.Client.IRFindByID(ctx, args[0])
			if err != nil {
				return HandleClientError(errw, *format, err)
			}
			dto = d
		} else {
			ir, err := a.IRRepo.FindByID(ctx, taskruntime.InputRequestID(args[0]))
			if err != nil {
				return HandleDomainError(errw, *format, err)
			}
			dto = irToDTO(ir)
		}
		if *format == "json" {
			b, _ := json.Marshal(irDTOToMap(dto))
			writeOut(out, string(b))
		} else {
			fmt.Fprintf(out, "input request %s\n  status: %s\n  execution: %s\n  question: %s\n",
				dto.ID, dto.Status, dto.ExecutionID, dto.Question)
			if len(dto.Options) > 0 {
				fmt.Fprintf(out, "  options:\n")
				for _, o := range dto.Options {
					fmt.Fprintf(out, "    - %s\n", o)
				}
			}
			if dto.DecidedAt != "" {
				// Re-format the timestamp to RFC3339 to preserve the legacy
				// human-readable output (the DTO carries RFC3339Nano).
				ts := dto.DecidedAt
				if t, perr := time.Parse(time.RFC3339Nano, dto.DecidedAt); perr == nil {
					ts = t.Format(time.RFC3339)
				}
				fmt.Fprintf(out, "  responded_at: %s\n  decided_by: %s\n  answer: %s\n",
					ts, dto.DecidedBy, dto.Answer)
			}
		}
		return ExitOK
	}
}

func (a *App) irRespondHandler(fs *flag.FlagSet) Handler {
	answer := fs.String("answer", "", "answer text (omit to enter interactive mode)")
	answerFile := fs.String("answer-file", "", "read answer from file ('-' = stdin)")
	decidedBy := fs.String("decided-by", "", "decided-by identity (defaults to configured user)")
	format := fs.String("format", FormatTable, formatFlagHelp())
	return func(ctx context.Context, args []string, out, errw io.Writer) ExitCode {
		if len(args) < 1 {
			return PrintError(errw, *format, "usage_error", "input-request respond <id> [--answer=... | --answer-file=... | <stdin>]", ExitUsage)
		}
		irID := taskruntime.InputRequestID(args[0])
		body, err := resolveAnswerInput(*answer, *answerFile)
		if err != nil {
			return PrintError(errw, *format, "usage_error", err.Error(), ExitUsage)
		}
		actor := a.DefaultActor()
		who := *decidedBy
		if who == "" {
			who = string(actor)
		}
		if a.Client != nil {
			if _, err := a.Client.IRRespond(ctx, IRRespondRequest{
				InputRequestID: string(irID),
				Answer:         body,
				DecidedBy:      who,
			}); err != nil {
				return HandleClientError(errw, *format, err)
			}
		} else {
			if a.IRSvc == nil {
				return PrintError(errw, *format, "internal_error",
					"input request service not wired", ExitNotImplemented)
			}
			if err := a.IRSvc.Respond(ctx, trservice.RespondInput{
				InputRequestID: irID,
				Answer:         body,
				DecidedBy:      who,
				Actor:          actor,
			}); err != nil {
				return HandleDomainError(errw, *format, err)
			}
		}
		if *format == "json" {
			b, _ := json.Marshal(map[string]any{
				"input_request_id": string(irID),
				"decided_by":       who,
				"answered":         true,
			})
			writeOut(out, string(b))
		} else {
			fmt.Fprintf(out, "responded to %s\n", irID)
		}
		return ExitOK
	}
}

func (a *App) irCancelHandler(fs *flag.FlagSet) Handler {
	reason := fs.String("reason", "user_cancel", "cancel reason")
	message := fs.String("message", "", "cancel message (required)")
	format := fs.String("format", FormatTable, formatFlagHelp())
	return func(ctx context.Context, args []string, out, errw io.Writer) ExitCode {
		if len(args) < 1 {
			return PrintError(errw, *format, "usage_error", "input-request cancel <id>", ExitUsage)
		}
		if *message == "" {
			return PrintError(errw, *format, "usage_error", "--message required", ExitUsage)
		}
		if a.Client != nil {
			if _, err := a.Client.IRCancel(ctx, IRCancelRequest{
				InputRequestID: args[0],
				Reason:         *reason,
				Message:        *message,
			}); err != nil {
				return HandleClientError(errw, *format, err)
			}
		} else {
			if a.IRSvc == nil {
				return PrintError(errw, *format, "internal_error",
					"input request service not wired", ExitNotImplemented)
			}
			if err := a.IRSvc.Cancel(ctx, trservice.CancelInput{
				InputRequestID: taskruntime.InputRequestID(args[0]),
				Reason:         *reason,
				Message:        *message,
				Actor:          a.DefaultActor(),
			}); err != nil {
				return HandleDomainError(errw, *format, err)
			}
		}
		writeOut(out, fmt.Sprintf("canceled input request %s", args[0]))
		return ExitOK
	}
}

// irDTOToMap mirrors the legacy irToMap for JSON output.
func irDTOToMap(ir InputRequestDTO) map[string]any {
	m := map[string]any{
		"id":           ir.ID,
		"status":       ir.Status,
		"execution_id": ir.ExecutionID,
		"question":     ir.Question,
		"options":      ir.Options,
		"urgency":      ir.Urgency,
		"created_at":   ir.CreatedAt,
	}
	if ir.DecidedAt != "" {
		m["answer"] = ir.Answer
		m["decided_by"] = ir.DecidedBy
		m["decided_at"] = ir.DecidedAt
	}
	return m
}

// irToDTO adapts a domain InputRequest to the DTO shape for the
// transitional Service / Repo fallback path (when a.Client is nil).
func irToDTO(ir *inputrequest.InputRequest) InputRequestDTO {
	dto := InputRequestDTO{
		ID:          string(ir.ID()),
		Status:      string(ir.Status()),
		ExecutionID: string(ir.TaskExecutionID()),
		Question:    ir.Question(),
		Options:     ir.Options(),
		Urgency:     string(ir.Urgency()),
		CreatedAt:   ir.CreatedAt().Format(time.RFC3339Nano),
	}
	if ra := ir.RespondedAt(); ra != nil {
		dto.Answer = ir.ResponseText()
		dto.DecidedBy = ir.RespondedBy()
		dto.DecidedAt = ra.Format(time.RFC3339Nano)
	}
	return dto
}

func irsToDTOs(irs []*inputrequest.InputRequest) []InputRequestDTO {
	out := make([]InputRequestDTO, len(irs))
	for i, ir := range irs {
		out[i] = irToDTO(ir)
	}
	return out
}

// irToMap is the legacy projection helper preserved here for any tests
// that still call it directly. New callers should go through Client +
// irDTOToMap.
func irToMap(ir *inputrequest.InputRequest) map[string]any {
	return irDTOToMap(irToDTO(ir))
}

// resolveAnswerInput resolves the answer body from one of the supported
// sources, in priority order:
//
//  1. --answer flag (literal text)
//  2. --answer-file=<path> (file contents; "-" reads stdin)
//  3. interactive prompt on stdin (when no flag given and stdin is a TTY)
//  4. piped stdin (when no flag and stdin is not a TTY)
//
// Returns an error if all sources are empty.
func resolveAnswerInput(answerFlag, fileFlag string) (string, error) {
	if answerFlag != "" {
		return answerFlag, nil
	}
	if fileFlag != "" {
		if fileFlag == "-" {
			data, err := io.ReadAll(os.Stdin)
			if err != nil {
				return "", fmt.Errorf("read stdin: %w", err)
			}
			body := strings.TrimRight(string(data), "\n")
			if body == "" {
				return "", errors.New("empty answer from stdin")
			}
			return body, nil
		}
		data, err := os.ReadFile(fileFlag)
		if err != nil {
			return "", fmt.Errorf("read --answer-file: %w", err)
		}
		body := strings.TrimRight(string(data), "\n")
		if body == "" {
			return "", fmt.Errorf("empty answer in %s", fileFlag)
		}
		return body, nil
	}
	// No explicit input source. If stdin has data piped in, read it;
	// otherwise enter interactive prompt mode.
	info, err := os.Stdin.Stat()
	if err == nil && (info.Mode()&os.ModeCharDevice) == 0 {
		data, _ := io.ReadAll(os.Stdin)
		body := strings.TrimRight(string(data), "\n")
		if body != "" {
			return body, nil
		}
	}
	fmt.Fprint(os.Stderr, "answer> ")
	reader := bufio.NewReader(os.Stdin)
	line, err := reader.ReadString('\n')
	if err != nil && !errors.Is(err, io.EOF) {
		return "", fmt.Errorf("read interactive prompt: %w", err)
	}
	body := strings.TrimRight(line, "\n")
	if body == "" {
		return "", errors.New("answer required")
	}
	return body, nil
}

// _ keeps observability import alive for actor types.
var _ = observability.Actor("")
