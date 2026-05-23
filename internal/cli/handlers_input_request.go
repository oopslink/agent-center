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
			irs []*inputrequest.InputRequest
			err error
		)
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
		switch *format {
		case FormatJSON:
			arr := make([]map[string]any, len(irs))
			for i, ir := range irs {
				arr[i] = irToMap(ir)
			}
			b, _ := json.Marshal(arr)
			writeOut(out, string(b))
		case FormatText:
			ids := make([]string, len(irs))
			for i, ir := range irs {
				ids[i] = string(ir.ID())
			}
			writeTextLines(out, ids)
		default:
			fmt.Fprintf(out, "%-30s %-12s %-30s %s\n", "ID", "STATUS", "EXECUTION", "QUESTION")
			for _, ir := range irs {
				q := ir.Question()
				if len(q) > 50 {
					q = q[:50] + "…"
				}
				fmt.Fprintf(out, "%-30s %-12s %-30s %s\n", ir.ID(), ir.Status(), ir.TaskExecutionID(), q)
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
		ir, err := a.IRRepo.FindByID(ctx, taskruntime.InputRequestID(args[0]))
		if err != nil {
			return HandleDomainError(errw, *format, err)
		}
		m := irToMap(ir)
		if *format == "json" {
			b, _ := json.Marshal(m)
			writeOut(out, string(b))
		} else {
			fmt.Fprintf(out, "input request %s\n  status: %s\n  execution: %s\n  question: %s\n",
				ir.ID(), ir.Status(), ir.TaskExecutionID(), ir.Question())
			opts := ir.Options()
			if len(opts) > 0 {
				fmt.Fprintf(out, "  options:\n")
				for _, o := range opts {
					fmt.Fprintf(out, "    - %s\n", o)
				}
			}
			if ra := ir.RespondedAt(); ra != nil {
				fmt.Fprintf(out, "  responded_at: %s\n  decided_by: %s\n  answer: %s\n",
					ra.Format(time.RFC3339), ir.RespondedBy(), ir.ResponseText())
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
		writeOut(out, fmt.Sprintf("canceled input request %s", args[0]))
		return ExitOK
	}
}

func irToMap(ir *inputrequest.InputRequest) map[string]any {
	m := map[string]any{
		"id":           string(ir.ID()),
		"status":       string(ir.Status()),
		"execution_id": string(ir.TaskExecutionID()),
		"question":     ir.Question(),
		"options":      ir.Options(),
		"urgency":      string(ir.Urgency()),
		"created_at":   ir.CreatedAt().Format(time.RFC3339Nano),
	}
	if ra := ir.RespondedAt(); ra != nil {
		m["answer"] = ir.ResponseText()
		m["decided_by"] = ir.RespondedBy()
		m["decided_at"] = ra.Format(time.RFC3339Nano)
	}
	return m
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
